package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chris-reynier/stkr/internal/service"
	"github.com/chris-reynier/stkr/internal/update"
)

func TestUpdateBannerOnPages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"tag_name": "v0.2.0", "html_url": "https://example.test/v0.2.0"}})
	}))
	defer srv.Close()
	f := newFixture(t, func(d *service.Deps) {
		d.Version = "v0.1.0"
		d.Updater = &update.Updater{Repo: "o/r", APIBase: srv.URL, HTTP: srv.Client()}
	})
	if body := f.get("/ui/today").Body.String(); strings.Contains(body, "is available") {
		t.Fatal("banner before any check")
	}
	if err := f.svc.UpdateTick(context.Background()); err != nil {
		t.Fatal(err)
	}
	body := f.get("/ui/today").Body.String()
	if !strings.Contains(body, "STKR v0.2.0 is available") || strings.Contains(body, `id="update-banner" class="banner" hidden`) {
		t.Fatalf("banner missing: %s", body)
	}
	if body := f.get("/ui/week").Body.String(); !strings.Contains(body, "STKR v0.2.0 is available") {
		t.Error("banner missing on week page")
	}
}
