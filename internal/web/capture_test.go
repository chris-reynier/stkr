package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
	"github.com/chris-reynier/stkr/internal/store"
)

func (f *fixture) audioDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(f.dir, "audio")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestIdeasPage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	if _, err := f.svc.AddContext(ctx, domain.Context{ID: "proja", Label: "Project A", Kind: domain.KindProject, Active: true}); err != nil {
		t.Fatal(err)
	}
	empty := body(t, f.get("/ui/ideas"), 200)
	mustContain(t, empty, "No ideas match", "0 new", ">Ideas<", `action="/ui/ideas/add"`)

	// Text capture through the form.
	w := f.post("/ui/ideas/add", url.Values{"text": {"<b>try htmx</b>"}, "next": {"/ui/ideas"}})
	if w.Code != 303 {
		t.Fatalf("add: %d", w.Code)
	}
	page := f.follow(w, "/ui/ideas")
	mustContain(t, page, `class="flash ok"`, "Idea saved", "&lt;b&gt;try htmx&lt;/b&gt;", "1 new", "Project A")
	if strings.Contains(page, "<b>try htmx") {
		t.Error("idea text not escaped")
	}
	f.post("/ui/ideas/add", url.Values{"text": {"dentist"}, "personal": {"1"}, "next": {"/ui/ideas"}})
	f.post("/ui/ideas/add", url.Values{"text": {"send budget"}, "type": {"action"}, "next": {"/ui/ideas"}})
	mustContain(t, body(t, f.get("/ui/ideas?tag=personal"), 200), "dentist")
	if strings.Contains(body(t, f.get("/ui/ideas?tag=personal"), 200), "try htmx") {
		t.Error("tag filter ignored")
	}
	if got := body(t, f.get("/ui/ideas?status=done"), 200); !strings.Contains(got, "No ideas match") {
		t.Error("status filter ignored")
	}
	items, _ := f.svc.ItemsOn(ctx, "2026-11-16")
	if len(items) != 1 || items[0].Origin != "meeting" {
		t.Errorf("items = %+v", items)
	}

	// Triage.
	list, _ := f.svc.ListIdeas(ctx, service.IdeaFilter{Tag: "pro"})
	id := itoa(list[0].ID)
	w = f.post("/ui/ideas/triage", url.Values{"id": {id}, "status": {"triaged"}, "project": {"proja"}, "tag": {"pro"}, "title": {"HTMX"}, "next": {"/ui/ideas?tag=pro"}})
	if w.Header().Get("Location") != "/ui/ideas?tag=pro" {
		t.Errorf("location = %q", w.Header().Get("Location"))
	}
	mustContain(t, f.follow(w, "/ui/ideas?tag=pro"), "Idea updated", `value="HTMX"`, `value="proja" selected`, `value="triaged" selected`)
	w = f.post("/ui/ideas/triage", url.Values{"id": {id}, "status": {"bogus"}, "project": {""}, "tag": {"pro"}, "title": {""}, "next": {"/ui/ideas"}})
	mustContain(t, f.follow(w, "/ui/ideas"), `class="flash error"`, "status must be one of")
	w = f.post("/ui/ideas/triage", url.Values{"id": {"x"}, "next": {"/ui/ideas"}})
	mustContain(t, f.follow(w, "/ui/ideas"), `class="flash error"`, "invalid id")
}

func TestIdeasPageFlagsMissingTools(t *testing.T) {
	f := newFixture(t)
	f.svc.Config().FFmpeg = filepath.Join(os.TempDir(), "no-ffmpeg-here")
	f.svc.Config().Whisper.Bin = filepath.Join(os.TempDir(), "no-whisper-here")
	f.svc.Config().PhoneInbox = filepath.Join(os.TempDir(), "no-inbox-here")
	mustContain(t, body(t, f.get("/ui/ideas"), 200),
		"ffmpeg was not found", "whisper was not found", "phone inbox folder cannot be read")
	// Recording without ffmpeg flashes the error.
	w := f.post("/ui/ideas/record", url.Values{"next": {"/ui/ideas"}})
	mustContain(t, f.follow(w, "/ui/ideas"), `class="flash error"`, "ffmpeg")
}

func TestIdeaAudioRoute(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	dir := f.audioDir(t)
	if err := os.WriteFile(filepath.Join(dir, "a.wav"), []byte("RIFFdata"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(filepath.Dir(dir), "secret.txt")
	if err := os.WriteFile(secret, []byte("TOPSECRET"), 0o600); err != nil {
		t.Fatal(err)
	}
	add := func(path string) string {
		i, err := f.svc.AddIdea(ctx, service.IdeaInput{Text: "x" + path})
		if err != nil {
			t.Fatal(err)
		}
		if err := f.setAudio(ctx, i.Idea.ID, path); err != nil {
			t.Fatal(err)
		}
		return itoa(i.Idea.ID)
	}
	ok := f.get("/ui/ideas/" + add("a.wav") + "/audio")
	if ok.Code != 200 || ok.Body.String() != "RIFFdata" || ok.Header().Get("Content-Security-Policy") == "" {
		t.Errorf("ok: %d %q", ok.Code, ok.Body)
	}
	mustContain(t, body(t, f.get("/ui/ideas"), 200), `<audio controls`)
	for _, p := range []string{"../secret.txt", secret, "..", "a.wav/../../secret.txt", "missing.wav"} {
		w := f.get("/ui/ideas/" + add(p) + "/audio")
		if w.Code != 404 || strings.Contains(w.Body.String(), "TOPSECRET") {
			t.Errorf("%q: %d %q", p, w.Code, w.Body)
		}
	}
	// The URL itself cannot carry a path either.
	for _, target := range []string{"/ui/ideas/..%2fsecret.txt/audio", "/ui/ideas/abc/audio", "/ui/ideas/999/audio", "/ui/ideas/1/../audio"} {
		if w := f.get(target); w.Code == 200 {
			t.Errorf("%s: %d", target, w.Code)
		}
	}
	noAudio, _ := f.svc.AddIdea(ctx, service.IdeaInput{Text: "plain"})
	if w := f.get("/ui/ideas/" + itoa(noAudio.Idea.ID) + "/audio"); w.Code != 404 {
		t.Errorf("no audio: %d", w.Code)
	}
}

func TestLinksPage(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<title>Test Site</title>"))
	}))
	defer site.Close()
	f := newFixture(t)
	ctx := context.Background()
	mustContain(t, body(t, f.get("/ui/links"), 200), "No links", ">Links<", `action="/ui/links/add"`)

	w := f.post("/ui/links/add", url.Values{"url": {site.URL}, "tags": {"Dev, tools"}, "next": {"/ui/links"}})
	page := f.follow(w, "/ui/links")
	mustContain(t, page, "Saved: Test Site", site.URL, ">dev<", ">tools<")
	// Duplicate and invalid are flashed as errors.
	w = f.post("/ui/links/add", url.Values{"url": {site.URL}, "next": {"/ui/links"}})
	mustContain(t, f.follow(w, "/ui/links"), `class="flash error"`, "already exists")
	w = f.post("/ui/links/add", url.Values{"url": {"javascript:alert(1)"}, "next": {"/ui/links"}})
	mustContain(t, f.follow(w, "/ui/links"), `class="flash error"`, "not an absolute http")

	list, _ := f.svc.ListBookmarks(ctx, "")
	id := itoa(list[0].ID)
	f.post("/ui/links/pin", url.Values{"id": {id}, "pinned": {"1"}, "next": {"/ui/links"}})
	mustContain(t, body(t, f.get("/ui/links"), 200), "pinned", "Unpin")
	mustContain(t, body(t, f.get("/ui/links?q=TEST"), 200), "Test Site")
	mustContain(t, body(t, f.get("/ui/links?q=nothing"), 200), "No links match")
	f.post("/ui/links/pin", url.Values{"id": {id}, "pinned": {"0"}, "next": {"/ui/links"}})
	if l, _ := f.svc.ListBookmarks(ctx, ""); l[0].Pinned {
		t.Error("not unpinned")
	}
	w = f.post("/ui/links/delete", url.Values{"id": {id}, "next": {"/ui/links"}})
	mustContain(t, f.follow(w, "/ui/links"), "Link deleted", "No links")
	w = f.post("/ui/links/delete", url.Values{"id": {id}, "next": {"/ui/links"}})
	mustContain(t, f.follow(w, "/ui/links"), `class="flash error"`)
}

func TestLinkTextIsEscaped(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<title>&lt;script&gt;alert(1)&lt;/script&gt;</title>`))
	}))
	defer site.Close()
	f := newFixture(t)
	if _, err := f.svc.AddBookmark(context.Background(), site.URL, nil); err != nil {
		t.Fatal(err)
	}
	if page := body(t, f.get("/ui/links"), 200); strings.Contains(page, "<script>alert") {
		t.Error("title not escaped")
	}
}

// setAudio points an idea at an arbitrary audio_path through a second
// database connection, as a corrupted or hand-edited database would.
func (f *fixture) setAudio(ctx context.Context, id int64, path string) error {
	st, err := store.Open(ctx, filepath.Join(f.dir, "stkr.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	i, err := st.GetIdea(ctx, id)
	if err != nil {
		return err
	}
	i.AudioPath = path
	return st.UpdateIdea(ctx, i)
}
