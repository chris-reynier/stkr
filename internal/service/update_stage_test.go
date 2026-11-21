package service

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/testutil"
	upd "github.com/chris-reynier/stkr/internal/update"
)

// releaseServer serves one Windows release (zip with both programs).
func releaseServer(t *testing.T, tag string) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range map[string]string{"stkr.exe": "NEW CLI", "stkrw.exe": "NEW DESK"} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	_ = zw.Close()
	arc := buf.Bytes()
	sum := sha256.Sum256(arc)
	ver := strings.TrimPrefix(tag, "v")
	zname := "stkr_" + ver + "_windows_amd64.zip"
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{"tag_name": tag, "html_url": "https://example.test/" + tag, "assets": []map[string]string{
				{"name": zname, "browser_download_url": srv.URL + "/a/" + zname},
				{"name": "checksums.txt", "browser_download_url": srv.URL + "/a/checksums.txt"},
			}}})
		case r.URL.Path == "/a/"+zname:
			_, _ = w.Write(arc)
		case r.URL.Path == "/a/checksums.txt":
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  " + zname + "\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestUpdateStage(t *testing.T) {
	srv := releaseServer(t, "v0.2.0")
	ctx := context.Background()
	dir := t.TempDir()
	exe := filepath.Join(dir, "stkrw.exe")
	for _, n := range []string{"stkr.exe", "stkrw.exe"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("OLD"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk := func(version string) *Service {
		svc, _, _ := updateFixture(t, true, version, releases())
		svc.updater = &upd.Updater{Repo: "o/r", APIBase: srv.URL, HTTP: srv.Client(), GOOS: "windows", GOARCH: "amd64"}
		return svc
	}

	st, err := mk("v0.1.0").UpdateStage(ctx, exe, upd.FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Method != upd.MethodReplace || st.Version != "0.2.0" {
		t.Fatalf("staged = %+v", st)
	}
	if b, _ := os.ReadFile(exe); string(b) != "OLD" {
		t.Error("program changed before Commit")
	}
	if err := st.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "NEW DESK" {
		t.Errorf("program = %q", b)
	}

	if _, err := mk("v0.2.0").UpdateStage(ctx, exe, upd.FetchOptions{}); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("up to date: %v", err)
	}
	if _, err := mk("dev").UpdateStage(ctx, exe, upd.FetchOptions{}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("dev build: %v", err)
	}
}

func TestUpdateConfigAppliesAutostartFirst(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	fake := &testutil.FakeAutostart{}
	f.svc.autostart = fake
	path := filepath.Join(t.TempDir(), "config.yaml")
	f.svc.configPath = path
	if err := config.Save(path, config.Default()); err != nil {
		t.Fatal(err)
	}

	if err := f.svc.UpdateConfig(ctx, map[string]string{AutostartKey: "true", "day_end": "18:00"}); err != nil {
		t.Fatal(err)
	}
	if on, _ := f.svc.AutostartEnabled(); !on {
		t.Error("sign-in entry not written")
	}
	if c, _ := f.svc.ConfigFile(); !c.Desktop.Autostart || c.DayEnd != "18:00" {
		t.Errorf("config = %+v", c.Desktop)
	}
	if err := f.svc.UpdateConfig(ctx, map[string]string{AutostartKey: "false"}); err != nil {
		t.Fatal(err)
	}
	if on, _ := f.svc.AutostartEnabled(); on {
		t.Error("sign-in entry not removed")
	}

	// A refused registry write leaves the file alone.
	fake.Err = errors.New("denied")
	if err := f.svc.UpdateConfig(ctx, map[string]string{AutostartKey: "true", "day_end": "19:00"}); err == nil {
		t.Fatal("expected the registry error")
	}
	if c, _ := f.svc.ConfigFile(); c.DayEnd != "18:00" || c.Desktop.Autostart {
		t.Errorf("config changed despite the error: %+v", c)
	}
	// An invalid value is refused without touching the entry.
	fake.Err = nil
	calls := fake.Calls
	if err := f.svc.UpdateConfig(ctx, map[string]string{AutostartKey: "maybe"}); !errors.Is(err, domain.ErrInvalid) || fake.Calls != calls {
		t.Errorf("invalid value: %v, calls %d -> %d", err, calls, fake.Calls)
	}
	// Other keys do not touch the sign-in entry.
	calls = fake.Calls
	if err := f.svc.UpdateConfig(ctx, map[string]string{"day_end": "17:30"}); err != nil || fake.Calls != calls {
		t.Errorf("other key: %v", err)
	}
}

func TestDesktopExe(t *testing.T) {
	// The test binary is neither stkrw.exe nor has one beside it.
	if p := DesktopExe(); p != "" {
		t.Errorf("DesktopExe = %q", p)
	}
}
