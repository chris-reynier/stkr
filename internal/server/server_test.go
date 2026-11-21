package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/clock"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/httpapi"
	"github.com/chris-reynier/stkr/internal/service"
	"github.com/chris-reynier/stkr/internal/testutil"
	"github.com/chris-reynier/stkr/internal/update"
)

func newService(t *testing.T, mutate func(*config.Config)) *service.Service {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Timezone = "UTC"
	cfg.Update.Check = false
	if mutate != nil {
		mutate(cfg)
	}
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFake(time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC))
	svc, err := service.Open(context.Background(), filepath.Join(dir, "stkr.db"), clk, cfg,
		service.Deps{BackupDir: filepath.Join(dir, "backups"), ConfigPath: cfgPath, Version: "0.2.0", Updater: offlineUpdater(), Autostart: &testutil.FakeAutostart{}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

func get(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(url) //nolint:noctx // test against a loopback server
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var v map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&v)
	return resp.StatusCode, v
}

func TestStartServesAndShutsDown(t *testing.T) {
	svc := newService(t, nil)
	h, err := Start(context.Background(), svc, Options{Port: 0, HostedBy: HostedByDesktop})
	if err != nil {
		t.Fatal(err)
	}
	if h.Port() == 0 || h.Addr() != "127.0.0.1:"+itoa(h.Port()) || h.URL() != "http://"+h.Addr() {
		t.Fatalf("addr %q url %q", h.Addr(), h.URL())
	}
	code, body := get(t, h.URL()+"/healthz")
	if code != 200 || body["ok"] != true {
		t.Fatalf("healthz %d %v", code, body)
	}
	code, body = get(t, h.URL()+"/api/status")
	if code != 200 || body["hosted_by"] != "desktop" {
		t.Fatalf("status %d %v", code, body)
	}
	// The web UI is mounted too.
	resp, err := http.Get(h.URL() + "/ui/today") //nolint:noctx // test
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("ui %d", resp.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.Done():
	default:
		t.Fatal("Done not closed after Shutdown")
	}
	if _, err := http.Get(h.URL() + "/healthz"); err == nil { //nolint:noctx,bodyclose // expect failure
		t.Fatal("server still answers after shutdown")
	}
}

func TestStartStopsWhenContextEnds(t *testing.T) {
	svc := newService(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	h, err := Start(ctx, svc, Options{})
	if err != nil {
		t.Fatal(err)
	}
	_, body := get(t, h.URL()+"/api/status")
	if body["hosted_by"] != "serve" {
		t.Fatalf("default hosted_by = %v", body["hosted_by"])
	}
	cancel()
	select {
	case <-h.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("server did not stop")
	}
	if h.Err() != nil {
		t.Fatal(h.Err())
	}
}

func TestStartPortBusy(t *testing.T) {
	ln, err := httpapi.Listen(0)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	svc := newService(t, nil)
	busy := ln.Addr().(*net.TCPAddr).Port
	_, err = Start(context.Background(), svc, Options{Port: busy})
	var le *ListenError
	if !errors.As(err, &le) || le.Port != busy || le.Unwrap() == nil || le.Error() == "" {
		t.Fatalf("err = %v", err)
	}
}

func TestShutdownHonoursContext(t *testing.T) {
	h := &Handle{cancel: func() {}, done: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := h.Shutdown(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestJobsPerConfig(t *testing.T) {
	base := []string{"daily-backup", "agent-sweeper", "close-reminder", "phone-inbox"}
	tests := []struct {
		name   string
		mutate func(*config.Config)
		want   []string
	}{
		{"defaults without update check", nil, base},
		{"update check on", func(c *config.Config) { c.Update.Check = true }, append(slices.Clone(base), "update-check")},
		{"ollama active", func(c *config.Config) {
			c.AI.Provider = "ollama"
			c.Ollama.Enabled = true
			c.Ollama.URL = "http://127.0.0.1:1" // closed port: no real calls
		}, append(slices.Clone(base), "ai-batch")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newService(t, tt.mutate)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h, err := Start(ctx, svc, Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = h.Shutdown(context.Background()) }()
			if got := h.Jobs(); !slices.Equal(got, tt.want) {
				t.Fatalf("jobs = %v, want %v", got, tt.want)
			}
		})
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// offlineUpdater points the update check at a closed loopback port, so the
// update-check job never reaches the network.
func offlineUpdater() *update.Updater {
	u := update.New()
	u.APIBase = "http://127.0.0.1:1"
	return u
}

func postForm(t *testing.T, base, path string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader("next=/ui/settings"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", base)
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// The desktop callbacks reach the web UI only when the desktop app hosts the
// server; `stkr serve` never runs them even if they are set.
func TestDesktopHooksOnlyWhenHostedByDesktop(t *testing.T) {
	for _, hostedBy := range []string{HostedByDesktop, HostedByServe} {
		t.Run(hostedBy, func(t *testing.T) {
			var folder, saved atomic.Int32
			svc := newService(t, nil)
			h, err := Start(context.Background(), svc, Options{
				Port: 0, HostedBy: hostedBy,
				OnOpenDataFolder: func() error { folder.Add(1); return nil },
				OnSettingsSaved:  func() { saved.Add(1) },
			})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = h.Shutdown(context.Background()) }()
			if code := postForm(t, h.URL(), "/ui/settings/open-data"); code != http.StatusSeeOther {
				t.Fatalf("open-data: %d", code)
			}
			if code := postForm(t, h.URL(), "/ui/settings/config"); code != http.StatusSeeOther {
				t.Fatalf("config: %d", code)
			}
			want := int32(0)
			if hostedBy == HostedByDesktop {
				want = 1
			}
			if folder.Load() != want || saved.Load() != want {
				t.Errorf("hooks ran folder=%d saved=%d, want %d", folder.Load(), saved.Load(), want)
			}
		})
	}
}
