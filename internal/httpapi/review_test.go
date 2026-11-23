package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
)

const httpSentinel = "HTTP-PRIVATE-4b2e"

// friday records Friday 2026-11-13: acme 09:00, a break with a private note
// at 12:00, acme again at 13:00 and off at 17:00. The clock ends on Monday.
func friday(t *testing.T, f *dailyFixture) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.svc.AddContext(ctx, domain.Context{ID: "acme", Label: "Acme", Kind: domain.KindProject, Workdir: "/src/" + httpSentinel}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []struct {
		h        int
		ctx, why string
	}{{9, "acme", "build"}, {12, "break", httpSentinel}, {13, "acme", ""}, {17, domain.ContextOff, ""}} {
		f.clk.Set(time.Date(2026, 11, 13, s.h, 0, 0, 0, time.UTC))
		if _, err := f.svc.Switch(ctx, s.ctx, domain.SourceDeck, s.why); err != nil {
			t.Fatal(err)
		}
	}
	f.clk.Set(time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC))
}

func TestReviewContextRoute(t *testing.T) {
	f := newDailyFixture(t)
	friday(t, f)

	w := f.do("GET", "/api/review/context")
	rc := decodeJSON[map[string]any](t, w)
	if w.Code != 200 || rc["date"] != "2026-11-13" || rc["basis"] != "previous_working_day" || rc["timezone"] != "UTC" || rc["working_day"] != true {
		t.Fatalf("%d %v", w.Code, rc)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("cache header")
	}
	if strings.Contains(w.Body.String(), httpSentinel) {
		t.Fatalf("private data: %s", w.Body)
	}
	for _, k := range []string{"tracking", "blocks", "gaps", "totals", "contexts", "open_items", "new_ideas", "close", "missing", "activity"} {
		if _, ok := rc[k]; !ok {
			t.Fatalf("no %q in %v", k, rc)
		}
	}
	if !strings.Contains(w.Body.String(), `"saved_at":null`) {
		t.Fatalf("close.saved_at must be null: %s", w.Body)
	}

	w = f.do("GET", "/api/review/context?date=2026-11-12")
	if rc := decodeJSON[map[string]any](t, w); w.Code != 200 || rc["basis"] != "requested" || rc["working_day"] != false {
		t.Fatalf("explicit: %d %v", w.Code, rc)
	}
	for _, bad := range []string{"2026-13-01", "yesterday", "17/11/2026", "2026-11-17", "2026-11-1"} {
		w = f.do("GET", "/api/review/context?date="+bad)
		if body := decodeJSON[errBody](t, w); w.Code != 400 || body.Error != "invalid" {
			t.Fatalf("%s: %d %v", bad, w.Code, body)
		}
	}
	for _, m := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		if w := f.do(m, "/api/review/context"); w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s: %d", m, w.Code)
		}
	}
	if w := f.do("GET", "/api/review/context", "Host", "evil.example:7878"); w.Code != http.StatusMisdirectedRequest {
		t.Fatalf("host guard: %d", w.Code)
	}
}

func TestReviewPromptRoute(t *testing.T) {
	f := newDailyFixture(t)
	friday(t, f)
	w := f.do("GET", "/api/review/prompt?date=2026-11-13")
	res := decodeJSON[map[string]any](t, w)
	if w.Code != 200 || res["clipboard"] != false || !strings.Contains(res["prompt"].(string), "stkr-review v1") {
		t.Fatalf("%d %v", w.Code, res)
	}
	if f.clip.Text() != "" {
		t.Fatal("GET must not touch the clipboard")
	}
	w = f.do("POST", "/api/review/prompt")
	if res := decodeJSON[map[string]any](t, w); w.Code != 200 || res["clipboard"] != true || f.clip.Text() == "" {
		t.Fatalf("POST: %d %v", w.Code, res)
	}
	if strings.Contains(f.clip.Text(), httpSentinel) {
		t.Fatal("private data in the prompt")
	}
}

func activityJSON(n int) string {
	var b strings.Builder
	b.WriteString(`{"events":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"ts":"2026-11-13T09:%02d:%02d+01:00","app":"Code","category":"development","workspace":"stkr","duration_seconds":60,"lock_state":"active"}`, (i/60)%60, i%60)
	}
	b.WriteString("]}")
	return b.String()
}

func TestActivityRoutes(t *testing.T) {
	f := newDailyFixture(t)
	ok := `{"events":[{"ts":"2026-11-13T09:10:00+01:00","app":"Code","category":"development","workspace":"stream-deck","context_id":"other","duration_seconds":320,"lock_state":"active"},` +
		`{"ts":"2026-11-13T10:00:00+01:00","app":"LockApp","category":"lock","duration_seconds":600,"lock_state":"locked"}]}`
	w := f.post("/api/activity/events", ok)
	res := decodeJSON[map[string]any](t, w)
	if w.Code != 200 || res["inserted"] != 2.0 {
		t.Fatalf("%d %v", w.Code, res)
	}
	w = f.post("/api/activity/events", ok)
	if res := decodeJSON[map[string]any](t, w); res["unchanged"] != 2.0 || res["inserted"] != 0.0 {
		t.Fatalf("replay: %v", res)
	}
	for name, body := range map[string]string{
		"window title field": `{"events":[{"ts":"2026-11-13T09:10:00+01:00","app":"Code","category":"development","duration_seconds":1,"lock_state":"active","window_title":"secret.docx - Word"}]}`,
		"clipboard field":    `{"events":[],"clipboard":"x"}`,
		"url field":          `{"events":[{"ts":"2026-11-13T09:10:00+01:00","app":"Edge","category":"browser","duration_seconds":1,"lock_state":"active","url":"https://x"}]}`,
		"not json":           `events=1`,
		"two values":         `{"events":[]} {"events":[]}`,
		"no events":          `{}`,
		"float duration":     `{"events":[{"ts":"2026-11-13T09:10:00+01:00","app":"Code","category":"development","duration_seconds":1.5,"lock_state":"active"}]}`,
		"negative duration":  `{"events":[{"ts":"2026-11-13T09:10:00+01:00","app":"Code","category":"development","duration_seconds":-5,"lock_state":"active"}]}`,
		"path workspace":     `{"events":[{"ts":"2026-11-13T09:10:00+01:00","app":"Code","category":"development","workspace":"C:\\src\\x","duration_seconds":5,"lock_state":"active"}]}`,
		"unknown lock state": `{"events":[{"ts":"2026-11-13T09:10:00+01:00","app":"Code","category":"development","duration_seconds":5,"lock_state":"asleep"}]}`,
	} {
		w := f.post("/api/activity/events", body)
		if e := decodeJSON[errBody](t, w); w.Code != 400 || e.Error != "invalid" {
			t.Fatalf("%s: %d %v", name, w.Code, e)
		}
	}
	// 1,000 events (a full day) fit; more than 5,000 or more than 1 MiB do not.
	if w := f.post("/api/activity/events", activityJSON(1000)); w.Code != 200 {
		t.Fatalf("1000 events: %d %s", w.Code, w.Body)
	}
	if w := f.post("/api/activity/events", `{"events":[`+strings.Repeat(`{},`, 5000)+`{}]}`); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("5001 events: %d", w.Code)
	}
	if w := f.post("/api/activity/events", activityJSON(6000)); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over 1 MiB: %d", w.Code)
	}
	if w := f.post("/api/activity/events", ok, "Origin", "https://evil.example"); w.Code != http.StatusForbidden {
		t.Fatalf("origin guard: %d", w.Code)
	}
	if w := f.do("GET", "/api/activity/events"); w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET events: %d", w.Code)
	}

	w = f.do("GET", "/api/activity/summary?date=2026-11-13")
	sum := decodeJSON[map[string]any](t, w)
	if w.Code != 200 || sum["events"] != 1002.0 || sum["locked_seconds"] != 600.0 {
		t.Fatalf("summary: %d %v", w.Code, sum)
	}
	if w := f.do("GET", "/api/activity/summary?date=nope"); w.Code != 400 {
		t.Fatalf("bad date: %d", w.Code)
	}
	if strings.Contains(f.logs.String(), "stream-deck") || strings.Contains(f.logs.String(), "LockApp") {
		t.Fatal("activity content logged")
	}
}

func TestImportReviewAndPreviewRoutes(t *testing.T) {
	f := newDailyFixture(t)
	text := "```stkr-review v1\ndate|type|context|text\n2026-11-13|achievement|other|Done A\n2026-11-13|action|other|Do B\n2026-11-13|action|other|Do B\n```"
	w := f.body("/api/import/preview", text)
	p := decodeJSON[map[string]any](t, w)
	if w.Code != 200 || p["valid"] != true || len(p["records"].([]any)) != 3 {
		t.Fatalf("preview: %d %v", w.Code, p)
	}
	w = f.body("/api/import", text)
	res := decodeJSON[map[string]any](t, w)
	if w.Code != 200 || res["kind"] != "review" || res["items_created"] != 2.0 || res["duplicates"] != 1.0 {
		t.Fatalf("import: %d %v", w.Code, res)
	}
	for _, k := range []string{"items_created", "ignored", "warnings", "exported", "blocks_rejected"} {
		if _, ok := res[k]; !ok {
			t.Fatalf("no %q in %v", k, res)
		}
	}
	w = f.body("/api/import", "```stkr-review v1\n2026-11-13|note|other\n```")
	if res := decodeJSON[map[string]any](t, w); w.Code != 422 || res["error"] != "no_valid_block" || res["redirect"] == nil || res["blocks_rejected"] != 1.0 {
		t.Fatalf("invalid block: %d %v", w.Code, res)
	}
}
