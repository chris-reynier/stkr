package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/capture"
	"github.com/chris-reynier/stkr/internal/clock"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/daily"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/store"
	"github.com/chris-reynier/stkr/internal/testutil"
)

type fakeRecorder struct {
	rec      bool
	path     string
	write    bool
	startErr error
}

func (r *fakeRecorder) Start(_ context.Context, out string) error {
	if r.startErr != nil {
		return r.startErr
	}
	r.rec, r.path = true, out
	return nil
}

func (r *fakeRecorder) Stop() error {
	r.rec = false
	if r.write {
		return os.WriteFile(r.path, []byte("RIFF....WAVE"), 0o600)
	}
	return nil
}

func (r *fakeRecorder) Recording() bool { return r.rec }

type fakeTranscriber struct {
	text  string
	err   error
	calls atomic.Int32
}

func (t *fakeTranscriber) Transcribe(context.Context, string) (string, error) {
	t.calls.Add(1)
	return t.text, t.err
}

type capFix struct {
	svc   *Service
	st    *store.Store
	clk   *clock.Fake
	logs  *bytes.Buffer
	audio string
	inbox string
	rec   *fakeRecorder
	tr    *fakeTranscriber
	clip  *testutil.FakeClipboard
}

func newCapFixture(t *testing.T, mutate ...func(*config.Config, *Deps)) *capFix {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "stkr.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.Migrate(ctx, filepath.Join(dir, "backups"), at("2026-11-16 08:00")); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Timezone = "Europe/Paris"
	cfg.Bookmarks.FetchTitle = true
	f := &capFix{
		st: st, clk: clock.NewFake(at("2026-11-16 09:00")), logs: &bytes.Buffer{},
		audio: filepath.Join(dir, "audio"), inbox: filepath.Join(dir, "inbox"),
		rec: &fakeRecorder{write: true}, tr: &fakeTranscriber{text: "spoken words"}, clip: &testutil.FakeClipboard{},
	}
	deps := Deps{
		Logger: slog.New(slog.NewTextHandler(f.logs, nil)), AudioDir: f.audio,
		Recorder: f.rec, Transcriber: f.tr, Clipboard: f.clip, TitleTimeout: 300 * time.Millisecond,
	}
	for _, m := range mutate {
		m(cfg, &deps)
	}
	svc, err := New(st, f.clk, cfg, deps)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(svc.capt.stop)
	f.svc = svc
	return f
}

func (f *capFix) addProject(t *testing.T, id string) {
	t.Helper()
	if _, err := f.svc.AddContext(context.Background(), domain.Context{ID: id, Label: strings.ToUpper(id), Kind: domain.KindProject, Active: true}); err != nil {
		t.Fatal(err)
	}
}

func (f *capFix) switchTo(t *testing.T, ts, id string) {
	t.Helper()
	f.clk.Set(at(ts))
	if _, err := f.svc.Switch(context.Background(), id, domain.SourceCLI, ""); err != nil {
		t.Fatal(err)
	}
}

func TestAddIdea(t *testing.T) {
	f := newCapFixture(t)
	ctx := context.Background()
	f.addProject(t, "proja")
	f.switchTo(t, "2026-11-16 09:10", "proja")

	res, err := f.svc.AddIdea(ctx, IdeaInput{Text: "  try htmx swaps  "})
	if err != nil {
		t.Fatal(err)
	}
	i := res.Idea
	if i == nil || i.RawText != "try htmx swaps" || i.Tag != "pro" || i.AIState != domain.AIPending || i.Status != domain.IdeaNew ||
		i.Source != IdeaSourceText || i.ContextID != "proja" {
		t.Errorf("idea = %+v", i)
	}
	res, err = f.svc.AddIdea(ctx, IdeaInput{Text: "dentist", Tag: "personal"})
	if err != nil || res.Idea.Tag != "personal" {
		t.Fatalf("personal: %+v %v", res, err)
	}

	bad := []IdeaInput{
		{Text: "  "},
		{Text: "x", Tag: "work"},
		{Text: "x", Type: "todo"},
		{Text: strings.Repeat("x", MaxIdeaRunes+1)},
		{Text: "x", Type: "action", Tag: "personal"},
		{Text: strings.Repeat("x", 501), Type: "decision"},
	}
	for _, in := range bad {
		if _, err := f.svc.AddIdea(ctx, in); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%+v: err = %v, want ErrInvalid", in, err)
		}
	}
	if strings.Contains(f.logs.String(), "htmx") || strings.Contains(f.logs.String(), "dentist") {
		t.Error("idea text was logged")
	}
}

func TestMeetingAttribution(t *testing.T) {
	f := newCapFixture(t)
	ctx := context.Background()

	// No project before the meeting: other.
	f.switchTo(t, "2026-11-16 08:30", "meeting")
	res, err := f.svc.AddIdea(ctx, IdeaInput{Text: "ship it", Type: "action", Via: ViaCLI})
	if err != nil || res.Item == nil || res.Item.Project != "other" {
		t.Fatalf("no project: %+v %v", res, err)
	}

	f.addProject(t, "proja")
	f.addProject(t, "projb")
	f.switchTo(t, "2026-11-16 09:00", "proja")
	f.switchTo(t, "2026-11-16 09:30", "meeting")
	res, err = f.svc.AddIdea(ctx, IdeaInput{Text: "Send the budget", Type: "action", Via: ViaAPI})
	if err != nil {
		t.Fatal(err)
	}
	it := res.Item
	if it.Project != "proja" || it.Origin != "meeting" || it.Type != domain.ItemAction || !res.Created || it.Source != ViaAPI || it.Date != "2026-11-16" {
		t.Errorf("item = %+v", it)
	}
	if it.Fingerprint != daily.Fingerprint(domain.ItemAction, "proja", "Send the budget") {
		t.Error("fingerprint is not the shared daily fingerprint")
	}
	// Same capture twice is one item.
	again, err := f.svc.AddIdea(ctx, IdeaInput{Text: "send the  budget", Type: "action"})
	if err != nil || again.Created || again.Item.ID != it.ID {
		t.Errorf("duplicate = %+v %v", again, err)
	}
	// A different project after the first meeting.
	f.switchTo(t, "2026-11-16 10:00", "projb")
	f.switchTo(t, "2026-11-16 10:30", "meeting")
	res, err = f.svc.AddIdea(ctx, IdeaInput{Text: "Use Postgres", Type: "decision"})
	if err != nil || res.Item.Project != "projb" || res.Item.Type != domain.ItemDecision {
		t.Errorf("decision = %+v %v", res, err)
	}
	// A note is an idea with source meeting.
	res, err = f.svc.AddIdea(ctx, IdeaInput{Text: "they like it", Type: "note"})
	if err != nil || res.Idea == nil || res.Idea.Source != IdeaSourceMeeting || res.Item != nil {
		t.Errorf("note = %+v %v", res, err)
	}
	items, _ := f.st.ListItems(ctx, "", "")
	if len(items) != 3 {
		t.Errorf("items = %d, want 3", len(items))
	}
}

func TestVoiceToggle(t *testing.T) {
	f := newCapFixture(t)
	ctx := context.Background()
	if f.svc.Recording() {
		t.Fatal("recording before start")
	}
	res, err := f.svc.ToggleRecording(ctx, "")
	if err != nil || !res.Recording || res.Idea != nil || !f.svc.Recording() {
		t.Fatalf("start: %+v %v", res, err)
	}
	res, err = f.svc.ToggleRecording(ctx, "")
	if err != nil || res.Recording || res.Idea == nil {
		t.Fatalf("stop: %+v %v", res, err)
	}
	if res.Idea.RawText != "" || res.Idea.AudioPath == "" || res.Idea.Source != IdeaSourceVoice || res.Idea.Tag != "pro" {
		t.Errorf("idea before transcription = %+v", res.Idea)
	}
	f.svc.WaitCapture()
	got, _ := f.svc.GetIdea(ctx, res.Idea.ID)
	if got.RawText != "spoken words" || got.AIState != domain.AIPending {
		t.Errorf("after transcription = %+v", got)
	}
	if p, err := f.svc.AudioFile(ctx, got.ID); err != nil || filepath.Base(p) != got.AudioPath {
		t.Errorf("AudioFile = %q %v", p, err)
	}
	if strings.Contains(f.logs.String(), "spoken words") {
		t.Error("transcript was logged")
	}
}

func TestVoiceTagAndStatuses(t *testing.T) {
	f := newCapFixture(t)
	ctx := context.Background()
	if _, err := f.svc.ToggleRecording(ctx, "secret"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("bad tag err = %v", err)
	}
	if _, err := f.svc.ToggleRecording(ctx, "personal"); err != nil {
		t.Fatal(err)
	}
	res, err := f.svc.ToggleRecording(ctx, "")
	if err != nil || res.Idea.Tag != "personal" {
		t.Fatalf("tag: %+v %v", res, err)
	}
	f.svc.WaitCapture()
}

func TestMissingWhisperKeepsIdea(t *testing.T) {
	f := newCapFixture(t)
	ctx := context.Background()
	f.tr.err = domain.New(domain.ErrUnavailable, "whisper not found")
	f.tr.text = ""
	_, _ = f.svc.ToggleRecording(ctx, "")
	res, err := f.svc.ToggleRecording(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	f.svc.WaitCapture()
	got, err := f.svc.GetIdea(ctx, res.Idea.ID)
	if err != nil || got.AIState != domain.AISkipped || got.AudioPath == "" || got.RawText != "" {
		t.Errorf("idea = %+v %v", got, err)
	}
}

func TestMissingFFmpegIs503(t *testing.T) {
	f := newCapFixture(t, func(c *config.Config, d *Deps) {
		c.FFmpeg = filepath.Join(t.TempDir(), "no-such-ffmpeg")
		d.Recorder = capture.NewFFmpegRecorder(c.FFmpeg, "Mic")
	})
	_, err := f.svc.ToggleRecording(context.Background(), "")
	if !errors.Is(err, domain.ErrUnavailable) || !strings.Contains(err.Error(), "ffmpeg") {
		t.Errorf("err = %v", err)
	}
	if h := f.svc.CaptureHealth(context.Background()); h.FFmpeg != "missing" || h.Recording {
		t.Errorf("health = %+v", h)
	}
}

func TestRecordingProducedNothing(t *testing.T) {
	f := newCapFixture(t)
	f.rec.write = false
	ctx := context.Background()
	_, _ = f.svc.ToggleRecording(ctx, "")
	if _, err := f.svc.ToggleRecording(ctx, ""); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("err = %v", err)
	}
	if list, _ := f.svc.ListIdeas(ctx, IdeaFilter{}); len(list) != 0 {
		t.Errorf("ideas = %d, want 0", len(list))
	}
	if f.svc.Recording() {
		t.Error("still recording")
	}
}

func TestNoAudioDir(t *testing.T) {
	f := newCapFixture(t, func(_ *config.Config, d *Deps) { d.AudioDir = "" })
	if _, err := f.svc.ToggleRecording(context.Background(), ""); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("err = %v", err)
	}
}

func (f *capFix) inboxFile(t *testing.T, rel, content string, age time.Duration) string {
	t.Helper()
	p := filepath.Join(f.inbox, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	mt := f.clk.Now().Add(-age)
	if err := os.Chtimes(p, mt, mt); err != nil {
		t.Fatal(err)
	}
	return p
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestInboxText(t *testing.T) {
	f := newCapFixture(t)
	f.svc.cfg.PhoneInbox = f.inbox
	ctx := context.Background()
	f.inboxFile(t, "note.txt", string(rune(0xFEFF))+"buy milk for the team\n", time.Minute)
	f.inboxFile(t, "P_dentist.TXT", "dentist tuesday", time.Minute)
	f.inboxFile(t, "perso_gift.txt", "gift idea", time.Minute)
	f.inboxFile(t, "personal/anything.txt", "in the personal folder", time.Minute)
	f.inboxFile(t, "pro_p_not_a_prefix.txt", "pro idea", time.Minute)
	f.inboxFile(t, "empty.txt", "  \n", time.Minute)
	f.inboxFile(t, "readme.md", "ignored", time.Minute)
	f.inboxFile(t, "processed/old.txt", "already done", time.Hour)

	res, err := f.svc.ScanInbox(ctx)
	if err != nil || res.Ingested != 6 || res.Failed != 0 {
		t.Fatalf("scan = %+v %v", res, err)
	}
	ideas, _ := f.svc.ListIdeas(ctx, IdeaFilter{})
	byText := map[string]domain.Idea{}
	for _, i := range ideas {
		byText[i.RawText] = i
		if i.Source != IdeaSourcePhone || i.AIState != domain.AIPending {
			t.Errorf("idea %+v", i)
		}
	}
	want := map[string]string{
		"buy milk for the team": "pro", "dentist tuesday": "personal", "gift idea": "personal",
		"in the personal folder": "personal", "pro idea": "pro",
	}
	if len(ideas) != len(want) {
		t.Fatalf("ideas = %d, want %d", len(ideas), len(want))
	}
	for text, tag := range want {
		if byText[text].Tag != tag {
			t.Errorf("%q tag = %q, want %q", text, byText[text].Tag, tag)
		}
	}
	for _, name := range []string{"note.txt", "P_dentist.TXT", "perso_gift.txt", "pro_p_not_a_prefix.txt", "empty.txt"} {
		if exists(filepath.Join(f.inbox, name)) || !exists(filepath.Join(f.inbox, "processed", name)) {
			t.Errorf("%s not moved to processed", name)
		}
	}
	if !exists(filepath.Join(f.inbox, "processed", "anything.txt")) || !exists(filepath.Join(f.inbox, "readme.md")) {
		t.Error("personal folder file not moved or unsupported file touched")
	}
	// A second scan finds nothing.
	if res, _ := f.svc.ScanInbox(ctx); res.Ingested != 0 {
		t.Errorf("rescan = %+v", res)
	}
}

func TestInboxCollisionAndStability(t *testing.T) {
	f := newCapFixture(t)
	f.svc.cfg.PhoneInbox = f.inbox
	ctx := context.Background()
	f.inboxFile(t, "processed/note.txt", "old", time.Hour)
	f.inboxFile(t, "processed/note-2.txt", "old2", time.Hour)
	f.inboxFile(t, "note.txt", "first", time.Minute)
	fresh := f.inboxFile(t, "growing.txt", "partial", time.Second)

	res, err := f.svc.ScanInbox(ctx)
	if err != nil || res.Ingested != 1 {
		t.Fatalf("scan = %+v %v", res, err)
	}
	if !exists(filepath.Join(f.inbox, "processed", "note-3.txt")) {
		t.Error("collision suffix missing")
	}
	if !exists(fresh) {
		t.Fatal("a file written 1 s ago must wait")
	}
	f.clk.Advance(10 * time.Second)
	if res, _ = f.svc.ScanInbox(ctx); res.Ingested != 1 || exists(fresh) {
		t.Errorf("stable scan = %+v", res)
	}
}

func TestInboxAudio(t *testing.T) {
	fake := testutil.BuildFakebin(t)
	f := newCapFixture(t, func(c *config.Config, _ *Deps) { c.FFmpeg = fake })
	f.svc.cfg.PhoneInbox = f.inbox
	ctx := context.Background()
	src := f.inboxFile(t, "p_walk.m4a", "audio", time.Minute)
	f.inboxFile(t, "meeting.mp3", "audio", time.Minute)

	res, err := f.svc.ScanInbox(ctx)
	if err != nil || res.Ingested != 2 || res.Failed != 0 {
		t.Fatalf("scan = %+v %v", res, err)
	}
	f.svc.WaitCapture()
	if exists(src) || !exists(filepath.Join(f.inbox, "processed", "p_walk.m4a")) {
		t.Error("source not moved")
	}
	ideas, _ := f.svc.ListIdeas(ctx, IdeaFilter{})
	if len(ideas) != 2 || f.tr.calls.Load() != 2 {
		t.Fatalf("ideas = %d, transcriptions = %d", len(ideas), f.tr.calls.Load())
	}
	tags := map[string]int{}
	for _, i := range ideas {
		if i.RawText != "spoken words" || i.Source != IdeaSourcePhone || !strings.HasSuffix(i.AudioPath, ".wav") {
			t.Errorf("idea = %+v", i)
		}
		tags[i.AudioPath[:strings.Index(i.AudioPath, "-")]+"/"+i.Tag]++
	}
	if tags["phone/personal"] != 1 || tags["phone/pro"] != 1 {
		t.Errorf("tags = %v", tags)
	}
}

func TestInboxAudioWithoutFFmpegStaysPut(t *testing.T) {
	f := newCapFixture(t, func(c *config.Config, _ *Deps) { c.FFmpeg = filepath.Join(os.TempDir(), "no-such-ffmpeg-bin") })
	f.svc.cfg.PhoneInbox = f.inbox
	src := f.inboxFile(t, "memo.ogg", "audio", time.Minute)
	for i := 0; i < 2; i++ {
		res, err := f.svc.ScanInbox(context.Background())
		if err != nil || res.Failed != 1 || res.Ingested != 0 {
			t.Fatalf("scan %d = %+v %v", i, res, err)
		}
	}
	if !exists(src) {
		t.Error("file must stay for a later retry")
	}
	if n := strings.Count(f.logs.String(), "file not processed"); n != 1 {
		t.Errorf("warning logged %d times, want once", n)
	}
	if list, _ := f.svc.ListIdeas(context.Background(), IdeaFilter{}); len(list) != 0 {
		t.Error("idea created without audio")
	}
}

func TestInboxDisabledAndHealth(t *testing.T) {
	f := newCapFixture(t)
	if res, err := f.svc.ScanInbox(context.Background()); err != nil || res != (InboxResult{}) {
		t.Errorf("disabled scan = %+v %v", res, err)
	}
	h := f.svc.CaptureHealth(context.Background())
	if h.PhoneInbox != "disabled" {
		t.Errorf("health = %+v", h)
	}
	f.svc.cfg.PhoneInbox = filepath.Join(f.inbox, "missing")
	if h = f.svc.CaptureHealth(context.Background()); h.PhoneInbox != "error" {
		t.Errorf("health = %+v", h)
	}
	if _, err := f.svc.ScanInbox(context.Background()); err == nil {
		t.Error("scan of a missing folder must fail")
	}
	f.svc.cfg.PhoneInbox = t.TempDir()
	if h = f.svc.CaptureHealth(context.Background()); h.PhoneInbox != "ok" {
		t.Errorf("health = %+v", h)
	}
}

func TestTriage(t *testing.T) {
	f := newCapFixture(t)
	ctx := context.Background()
	f.addProject(t, "proja")
	a, _ := f.svc.AddIdea(ctx, IdeaInput{Text: "one"})
	b, _ := f.svc.AddIdea(ctx, IdeaInput{Text: "two", Tag: "personal"})
	_, _ = f.svc.AddIdea(ctx, IdeaInput{Text: "three"})

	if n, err := f.svc.IdeasInboxCount(ctx); err != nil || n != 2 {
		t.Errorf("inbox count = %d %v, want 2 (new pro ideas)", n, err)
	}
	st, proj, title, tag := domain.IdeaTriaged, "proja", " Nice title ", "pro"
	got, err := f.svc.TriageIdea(ctx, a.Idea.ID, IdeaPatch{Status: &st, Project: &proj, Title: &title})
	if err != nil || got.Status != domain.IdeaTriaged || got.SuggestedProject != "proja" || got.Title != "Nice title" || got.RawText != "one" {
		t.Fatalf("triage = %+v %v", got, err)
	}
	if got, err = f.svc.TriageIdea(ctx, b.Idea.ID, IdeaPatch{Tag: &tag}); err != nil || got.Tag != "pro" || got.Status != domain.IdeaNew {
		t.Errorf("retag = %+v %v", got, err)
	}
	if n, _ := f.svc.IdeasInboxCount(ctx); n != 2 {
		t.Errorf("inbox count after = %d, want 2", n)
	}
	empty := ""
	if got, _ = f.svc.TriageIdea(ctx, a.Idea.ID, IdeaPatch{Project: &empty}); got.SuggestedProject != "" {
		t.Errorf("project not cleared: %+v", got)
	}

	for name, p := range map[string]IdeaPatch{
		"status":  {Status: statusPtr("later")},
		"project": {Project: strPtr("nope")},
		"mode":    {Project: strPtr("meeting")},
		"tag":     {Tag: strPtr("x")},
		"title":   {Title: strPtr(strings.Repeat("t", MaxTitleRunes+1))},
	} {
		if _, err := f.svc.TriageIdea(ctx, a.Idea.ID, p); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if _, err := f.svc.TriageIdea(ctx, 999, IdeaPatch{}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("missing idea err = %v", err)
	}

	list, err := f.svc.ListIdeas(ctx, IdeaFilter{Tag: "personal"})
	if err != nil || len(list) != 0 {
		t.Errorf("personal = %d %v (b was retagged)", len(list), err)
	}
	if list, _ = f.svc.ListIdeas(ctx, IdeaFilter{Status: domain.IdeaTriaged}); len(list) != 1 {
		t.Errorf("triaged = %d", len(list))
	}
	if _, err := f.svc.ListIdeas(ctx, IdeaFilter{Status: "weird"}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("bad status filter err = %v", err)
	}
	if _, err := f.svc.ListIdeas(ctx, IdeaFilter{Tag: "weird"}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("bad tag filter err = %v", err)
	}
}

func statusPtr(s string) *domain.IdeaStatus { v := domain.IdeaStatus(s); return &v }
func strPtr(s string) *string               { return &s }

func TestAudioFileRejectsEscapes(t *testing.T) {
	f := newCapFixture(t)
	ctx := context.Background()
	if err := os.MkdirAll(f.audio, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(filepath.Dir(f.audio), "secret.txt")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.audio, "ok.wav"), []byte("RIFF"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]bool{
		"ok.wav":                       true,
		"../secret.txt":                false,
		"..":                           false,
		".":                            false,
		secret:                         false,
		"sub/../../secret.txt":         false,
		"..\\secret.txt":               false,
		"missing.wav":                  false,
		"":                             false,
		"ok.wav/../../secret.txt":      false,
		filepath.Dir(f.audio):          false,
		filepath.Join("..", "..", "x"): false,
	}
	for path, ok := range cases {
		i, err := f.st.InsertIdea(ctx, domain.Idea{TS: f.clk.Now(), RawText: "x", AudioPath: path})
		if err != nil {
			t.Fatal(err)
		}
		p, err := f.svc.AudioFile(ctx, i.ID)
		if ok != (err == nil) {
			t.Errorf("%q: path=%q err=%v, want ok=%v", path, p, err, ok)
		}
		if err != nil && !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%q: err = %v, want ErrNotFound", path, err)
		}
	}
}

func TestAudioFileRejectsSymlinkEscape(t *testing.T) {
	f := newCapFixture(t)
	ctx := context.Background()
	if err := os.MkdirAll(f.audio, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(filepath.Dir(f.audio), "outside.wav")
	_ = os.WriteFile(secret, []byte("x"), 0o600)
	if err := os.Symlink(secret, filepath.Join(f.audio, "link.wav")); err != nil {
		t.Skip("symlinks not available:", err)
	}
	i, _ := f.st.InsertIdea(ctx, domain.Idea{TS: f.clk.Now(), AudioPath: "link.wav"})
	if _, err := f.svc.AudioFile(ctx, i.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("symlink escape err = %v", err)
	}
}

// ---- bookmarks ----

func page(title string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head>\n<TITLE>\n  " + title + "  </TITLE></head><body>hi</body></html>"))
	}
}

func TestBookmarkTitleFetch(t *testing.T) {
	var hits atomic.Int32
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) { hits.Add(1); page("Go &amp; Docs")(w, r) })
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(3 * time.Second):
		case <-r.Context().Done():
		}
		page("Too late")(w, r)
	})
	mux.HandleFunc("/notitle", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>none</html>")) })
	mux.HandleFunc("/err", func(w http.ResponseWriter, _ *http.Request) { http.Error(w, "boom", 500) })
	mux.HandleFunc("/big", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("a", 600<<10) + "<title>Late title</title>"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	f := newCapFixture(t)
	ctx := context.Background()
	cases := []struct{ path, title string }{
		{"/ok", "Go & Docs"},
		{"/slow", srv.URL + "/slow"},
		{"/notitle", srv.URL + "/notitle"},
		{"/err", srv.URL + "/err"},
		{"/big", srv.URL + "/big"},
	}
	for _, c := range cases {
		start := time.Now()
		b, err := f.svc.AddBookmark(ctx, srv.URL+c.path, []string{"Go, Docs", "go", " "})
		if err != nil {
			t.Fatalf("%s: %v", c.path, err)
		}
		if b.Title != c.title {
			t.Errorf("%s: title = %q, want %q", c.path, b.Title, c.title)
		}
		if time.Since(start) > 2*time.Second {
			t.Errorf("%s: too slow", c.path)
		}
		if b.Created.IsZero() || b.ID == 0 || strings.Join(b.Tags, ",") != "go,docs" {
			t.Errorf("%s: bookmark = %+v", c.path, b)
		}
	}
	// Duplicate: error with the stored bookmark, and no second fetch.
	before := hits.Load()
	_, err := f.svc.AddBookmark(ctx, srv.URL+"/ok", nil)
	var dup *DuplicateBookmarkError
	if !errors.As(err, &dup) || !errors.Is(err, domain.ErrConflict) || dup.Existing.Title != "Go & Docs" {
		t.Fatalf("dup err = %v", err)
	}
	if hits.Load() != before {
		t.Error("duplicate must not fetch the page")
	}
}

func TestBookmarkFetchOff(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); page("T")(w, r) }))
	defer srv.Close()
	f := newCapFixture(t, func(c *config.Config, _ *Deps) { c.Bookmarks.FetchTitle = false })
	b, err := f.svc.AddBookmark(context.Background(), srv.URL, nil)
	if err != nil || b.Title != srv.URL || hits.Load() != 0 {
		t.Errorf("b = %+v err=%v hits=%d", b, err, hits.Load())
	}
}

func TestBookmarkValidation(t *testing.T) {
	f := newCapFixture(t, func(c *config.Config, _ *Deps) { c.Bookmarks.FetchTitle = false })
	for _, raw := range []string{"", "not a url", "example.com", "ftp://example.com/x", "javascript:alert(1)", "file:///etc/passwd",
		"http://", "//example.com", "http://user:pw@example.com/", "https://example.com/" + strings.Repeat("a", MaxURLLen)} {
		if _, err := f.svc.AddBookmark(context.Background(), raw, nil); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%q: err = %v, want ErrInvalid", raw, err)
		}
	}
	if b, err := f.svc.AddBookmark(context.Background(), "  https://example.com/a?b=1#c  ", nil); err != nil || b.URL != "https://example.com/a?b=1#c" {
		t.Errorf("valid url: %+v %v", b, err)
	}
}

func TestBookmarkClipboardListPinDelete(t *testing.T) {
	f := newCapFixture(t, func(c *config.Config, _ *Deps) { c.Bookmarks.FetchTitle = false })
	ctx := context.Background()
	if _, err := f.svc.BookmarkFromClipboard(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("empty clipboard err = %v", err)
	}
	_ = f.clip.WriteText(ctx, "\n  https://example.com/one  \nsecond line")
	b1, err := f.svc.BookmarkFromClipboard(ctx, []string{"read"})
	if err != nil || b1.URL != "https://example.com/one" {
		t.Fatalf("clipboard = %+v %v", b1, err)
	}
	_ = f.clip.WriteText(ctx, "not a link")
	if _, err := f.svc.BookmarkFromClipboard(ctx, nil); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("bad clipboard err = %v", err)
	}
	f.clip.Err = domain.New(domain.ErrUnavailable, "no clipboard")
	if _, err := f.svc.BookmarkFromClipboard(ctx, nil); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("unavailable err = %v", err)
	}
	b2, _ := f.svc.AddBookmark(ctx, "https://golang.org/doc", []string{"go"})

	if all, _ := f.svc.ListBookmarks(ctx, ""); len(all) != 2 {
		t.Errorf("all = %d", len(all))
	}
	if got, _ := f.svc.ListBookmarks(ctx, "GOLANG"); len(got) != 1 || got[0].ID != b2.ID {
		t.Errorf("search url = %+v", got)
	}
	if got, _ := f.svc.ListBookmarks(ctx, "read"); len(got) != 1 || got[0].ID != b1.ID {
		t.Errorf("search tag = %+v", got)
	}
	if got, _ := f.svc.ListBookmarks(ctx, "zzz"); len(got) != 0 {
		t.Errorf("search none = %+v", got)
	}
	if err := f.svc.PinBookmark(ctx, b2.ID, true); err != nil {
		t.Fatal(err)
	}
	if all, _ := f.svc.ListBookmarks(ctx, ""); all[0].ID != b2.ID || !all[0].Pinned {
		t.Errorf("pinned not first: %+v", all)
	}
	if err := f.svc.DeleteBookmark(ctx, b2.ID); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.DeleteBookmark(ctx, b2.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second delete err = %v", err)
	}
	if err := f.svc.PinBookmark(ctx, 999, true); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("pin missing err = %v", err)
	}
}
