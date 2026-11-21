package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/clock"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/store"
	"github.com/chris-reynier/stkr/internal/testutil"
	upd "github.com/chris-reynier/stkr/internal/update"
)

func updateFixture(t *testing.T, check bool, version string, h http.Handler) (*Service, string, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "stkr.db")
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.Migrate(ctx, t.TempDir(), at("2026-11-16 08:00")); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Update.Check = check
	svc, err := New(st, clock.NewFake(at("2026-11-16 09:00")), cfg, Deps{
		Version: version, Autostart: &testutil.FakeAutostart{},
		Updater: &upd.Updater{Repo: "o/r", APIBase: srv.URL, HTTP: srv.Client()},
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, dbPath, &hits
}

func releases(tags ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		var out []map[string]any
		for _, tag := range tags {
			out = append(out, map[string]any{"tag_name": tag, "html_url": "https://example.test/" + tag})
		}
		_ = json.NewEncoder(w).Encode(out)
	})
}

func TestUpdateTickDisabledMakesNoRequest(t *testing.T) {
	svc, _, hits := updateFixture(t, false, "v0.1.0", releases("v0.2.0"))
	if err := svc.UpdateTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 0 {
		t.Fatalf("update.check=false made %d request(s)", hits.Load())
	}
	if svc.UpdateBanner(context.Background()) != "" {
		t.Error("banner shown with check off")
	}
}

func TestUpdateTickStoresAndBanner(t *testing.T) {
	svc, dbPath, hits := updateFixture(t, true, "v0.1.0", releases("v0.2.0", "v0.1.0"))
	ctx := context.Background()
	if _, ok := svc.UpdateInfo(ctx); ok {
		t.Fatal("info before any check")
	}
	if err := svc.UpdateTick(ctx); err != nil || hits.Load() != 1 {
		t.Fatalf("tick: %v hits=%d", err, hits.Load())
	}
	info, ok := svc.UpdateInfo(ctx)
	if !ok || !info.Available || info.Latest != "v0.2.0" || info.Checked.IsZero() {
		t.Fatalf("info = %+v ok=%v", info, ok)
	}
	if b := svc.UpdateBanner(ctx); b != "STKR v0.2.0 is available" {
		t.Errorf("banner = %q", b)
	}
	if ci, ok := CachedUpdateInfo(ctx, dbPath, "v0.1.0"); !ok || !ci.Available {
		t.Errorf("cached = %+v %v", ci, ok)
	}
	if ci, ok := CachedUpdateInfo(ctx, dbPath, "v0.2.0"); !ok || ci.Available {
		t.Errorf("cached current = %+v %v", ci, ok)
	}
	if _, ok := CachedUpdateInfo(ctx, filepath.Join(t.TempDir(), "none.db"), "v0.1.0"); ok {
		t.Error("missing db reported info")
	}
	// After updating (running version == latest) the banner goes away.
	svc.version = "v0.2.0"
	if svc.UpdateBanner(ctx) != "" {
		t.Error("banner after update")
	}
}

func TestUpdateBannerHiddenForDevBuild(t *testing.T) {
	svc, _, _ := updateFixture(t, true, "dev", releases("v0.2.0"))
	_ = svc.UpdateTick(context.Background())
	if svc.UpdateBanner(context.Background()) != "" {
		t.Error("dev build shows banner")
	}
	if info, ok := svc.UpdateInfo(context.Background()); !ok || !info.Available {
		t.Errorf("dev is older than any release: %+v", info)
	}
}

func TestUpdateTickSilentAndBoundedOnFailure(t *testing.T) {
	// Server that hangs until the request context is cancelled.
	svc, _, _ := updateFixture(t, true, "v0.1.0", http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := svc.UpdateTick(ctx); err != nil {
		t.Fatalf("tick must be silent, got %v", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("tick blocked for %v", d)
	}
	if svc.UpdateBanner(context.Background()) != "" {
		t.Error("banner after failed check")
	}

	// A non-200 answer is also silent.
	svc2, _, _ := updateFixture(t, true, "v0.1.0", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	if err := svc2.UpdateTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc2.UpdateCheck(context.Background()); err == nil || strings.Contains(err.Error(), "rate limited") {
		t.Errorf("explicit check must report failure without server text: %v", err)
	}
}
