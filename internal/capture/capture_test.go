package capture

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/testutil"
)

func TestTagFor(t *testing.T) {
	cases := []struct {
		name     string
		personal bool
		want     string
	}{
		{"idea.txt", false, TagPro},
		{"p_idea.txt", false, TagPersonal},
		{"P_Idea.m4a", false, TagPersonal},
		{"perso_x.txt", false, TagPersonal},
		{"PERSO_x.txt", false, TagPersonal},
		{"personal_x.txt", false, TagPro},
		{"pro_p_x.txt", false, TagPro},
		{"plain.txt", true, TagPersonal},
	}
	for _, c := range cases {
		if got := TagFor(c.name, c.personal); got != c.want {
			t.Errorf("TagFor(%q, %v) = %q, want %q", c.name, c.personal, got, c.want)
		}
	}
}

func TestScanInbox(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	put := func(rel string, age time.Duration) {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o700)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, now.Add(-age), now.Add(-age)); err != nil {
			t.Fatal(err)
		}
	}
	put("b.m4a", 2*time.Minute)
	put("a.txt", 3*time.Minute)
	put("c.MP3", time.Minute)
	put("d.wav", time.Minute)
	put("e.ogg", time.Minute)
	put("f.opus", time.Minute)
	put("p_g.txt", time.Minute)
	put("personal/h.txt", time.Minute)
	put("writing.txt", 2*time.Second)
	put("future.txt", -time.Second) // clock skew just ahead: still being written
	put("skewed.txt", -3*time.Hour) // far in the future: treated as stable
	put("notes.pdf", time.Minute)
	put(".hidden.txt", time.Minute)
	put("processed/old.txt", time.Minute)
	put("personal/processed/old.txt", time.Minute)
	if err := os.Mkdir(filepath.Join(dir, "folder.txt"), 0o700); err != nil {
		t.Fatal(err)
	}

	files, err := ScanInbox(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]InboxFile{}
	for _, f := range files {
		got[filepath.ToSlash(strings.TrimPrefix(f.Path, dir+string(filepath.Separator)))] = f
	}
	want := map[string]string{
		"a.txt": TagPro, "b.m4a": TagPro, "c.MP3": TagPro, "d.wav": TagPro, "e.ogg": TagPro, "f.opus": TagPro,
		"p_g.txt": TagPersonal, "personal/h.txt": TagPersonal, "skewed.txt": TagPro,
	}
	if len(got) != len(want) {
		t.Errorf("found %v, want %d files", keys(got), len(want))
	}
	for name, tag := range want {
		f, ok := got[name]
		if !ok || f.Tag != tag {
			t.Errorf("%s: %+v ok=%v, want tag %s", name, f, ok, tag)
		}
		if ok && f.Audio != (!strings.HasSuffix(name, ".txt")) {
			t.Errorf("%s: Audio = %v", name, f.Audio)
		}
	}
	if files[0].Path != filepath.Join(dir, "skewed.txt") && files[0].Path != filepath.Join(dir, "a.txt") {
		t.Errorf("oldest first expected, got %s", files[0].Path)
	}
	if _, err := ScanInbox(filepath.Join(dir, "missing"), now); err == nil {
		t.Error("missing dir must fail")
	}
}

func keys(m map[string]InboxFile) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestScanInboxLimit(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for i := 0; i < MaxInboxFiles+5; i++ {
		p := filepath.Join(dir, "f"+string(rune('a'+i%26))+string(rune('a'+i/26))+".txt")
		_ = os.WriteFile(p, []byte("x"), 0o600)
		_ = os.Chtimes(p, now.Add(-time.Hour), now.Add(-time.Hour))
	}
	files, err := ScanInbox(dir, now)
	if err != nil || len(files) != MaxInboxFiles {
		t.Errorf("files = %d %v", len(files), err)
	}
}

func TestMoveProcessed(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 3; i++ {
		src := filepath.Join(dir, "a.txt")
		_ = os.WriteFile(src, []byte{byte('0' + i)}, 0o600)
		dst, err := MoveProcessed(dir, src)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"a.txt", "a-2.txt", "a-3.txt"}[i]
		if filepath.Base(dst) != want || filepath.Base(filepath.Dir(dst)) != "processed" {
			t.Errorf("moved to %s, want processed/%s", dst, want)
		}
		if _, err := os.Stat(src); !os.IsNotExist(err) {
			t.Error("source still present")
		}
	}
	b, _ := os.ReadFile(filepath.Join(dir, "processed", "a-3.txt"))
	if string(b) != "2" {
		t.Errorf("content = %q", b)
	}
	if _, err := MoveProcessed(dir, filepath.Join(dir, "gone.txt")); err == nil {
		t.Error("moving a missing file must fail")
	}
}

func TestReadText(t *testing.T) {
	p := filepath.Join(t.TempDir(), "n.txt")
	_ = os.WriteFile(p, []byte("\xef\xbb\xbf  hello \xff world \n"), 0o600)
	got, err := ReadText(p)
	if err != nil || got != "hello  world" {
		t.Errorf("got %q %v", got, err)
	}
	_ = os.WriteFile(p, []byte(strings.Repeat("a", MaxInboxText+100)), 0o600)
	if got, _ = ReadText(p); len(got) != MaxInboxText {
		t.Errorf("len = %d, want %d", len(got), MaxInboxText)
	}
	if _, err := ReadText(p + ".missing"); err == nil {
		t.Error("missing file must fail")
	}
}

func TestParseTitle(t *testing.T) {
	cases := map[string]string{
		"<title>Hello</title>":                            "Hello",
		"<HEAD><TITLE lang=en>\n  A &amp; B \n </TITLE>":  "A & B",
		"<title>one</title><title>two</title>":            "one",
		"no title here":                                   "",
		"<title></title>":                                 "",
		"<title>" + strings.Repeat("x", 300) + "</title>": strings.Repeat("x", 200),
	}
	for in, want := range cases {
		if got := ParseTitle(in); got != want {
			t.Errorf("ParseTitle(%.30q) = %q, want %q", in, got, want)
		}
	}
}

func TestFetchTitle(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<title>Fine</title>")) })
	mux.HandleFunc("/redir", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/ok", http.StatusFound) })
	mux.HandleFunc("/loop", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/loop", http.StatusFound) })
	mux.HandleFunc("/ftp", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "ftp://example.com/x", http.StatusFound)
	})
	mux.HandleFunc("/404", http.NotFound)
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(5 * time.Second):
		case <-r.Context().Done():
		}
	})
	mux.HandleFunc("/ua", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<title>" + r.UserAgent() + "</title>"))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	ctx := context.Background()

	for path, want := range map[string]string{"/ok": "Fine", "/redir": "Fine", "/ua": "stkr-bookmark/1"} {
		if got, err := FetchTitle(ctx, srv.URL+path, time.Second); err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", path, got, err, want)
		}
	}
	for _, path := range []string{"/loop", "/ftp", "/404"} {
		if _, err := FetchTitle(ctx, srv.URL+path, time.Second); err == nil {
			t.Errorf("%s: want error", path)
		}
	}
	start := time.Now()
	if _, err := FetchTitle(ctx, srv.URL+"/slow", 200*time.Millisecond); err == nil || time.Since(start) > 2*time.Second {
		t.Errorf("slow: err=%v after %v", err, time.Since(start))
	}
	if _, err := FetchTitle(ctx, "http://127.0.0.1:1/", 200*time.Millisecond); err == nil {
		t.Error("refused connection must fail")
	}
	if _, err := FetchTitle(ctx, "://bad", time.Second); err == nil {
		t.Error("bad URL must fail")
	}
}

func TestWhich(t *testing.T) {
	if Which("") || Which("   ") || Which("definitely-not-a-program-xyz") {
		t.Error("Which found a missing program")
	}
	if !Which(testutil.BuildFakebin(t)) {
		t.Error("Which must find an absolute path")
	}
}

func TestWhisper(t *testing.T) {
	fake := testutil.BuildFakebin(t)
	args := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKEBIN_STDOUT", "  hello from whisper \n")
	t.Setenv("FAKEBIN_ARGS_FILE", args)
	got, err := Whisper{Bin: fake, Model: "m.bin"}.Transcribe(context.Background(), "a.wav")
	if err != nil || got != "hello from whisper" {
		t.Fatalf("got %q %v", got, err)
	}
	b, _ := os.ReadFile(args)
	if string(b) != "-m\nm.bin\n-f\na.wav\n-nt\n-np" {
		t.Errorf("args = %q", b)
	}
	if _, err := (Whisper{Bin: fake}).Transcribe(context.Background(), "a.wav"); err != nil {
		t.Error(err)
	} else if b, _ = os.ReadFile(args); strings.Contains(string(b), "-m") {
		t.Errorf("no model must omit -m: %q", b)
	}

	t.Setenv("FAKEBIN_EXIT", "3")
	t.Setenv("FAKEBIN_STDOUT", "secret transcript")
	_, err = Whisper{Bin: fake}.Transcribe(context.Background(), "a.wav")
	if err == nil || !strings.Contains(err.Error(), "status 3") || strings.Contains(err.Error(), "secret") {
		t.Errorf("exit error = %v", err)
	}

	_, err = Whisper{Bin: filepath.Join(t.TempDir(), "nope")}.Transcribe(context.Background(), "a.wav")
	if !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("missing bin err = %v", err)
	}

	t.Setenv("FAKEBIN_EXIT", "0")
	t.Setenv("FAKEBIN_SLEEP_MS", "5000")
	start := time.Now()
	if _, err = (Whisper{Bin: fake, Timeout: 200 * time.Millisecond}).Transcribe(context.Background(), "a.wav"); err == nil || time.Since(start) > 3*time.Second {
		t.Errorf("timeout: err=%v after %v", err, time.Since(start))
	}
}

func TestConvertToWAV(t *testing.T) {
	fake := testutil.BuildFakebin(t)
	args := filepath.Join(t.TempDir(), "args")
	t.Setenv("FAKEBIN_ARGS_FILE", args)
	if err := ConvertToWAV(context.Background(), fake, "in.m4a", "out.wav"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(args)
	if string(b) != "-y\n-loglevel\nerror\n-i\nin.m4a\n-ac\n1\n-ar\n16000\nout.wav" {
		t.Errorf("args = %q", b)
	}
	t.Setenv("FAKEBIN_EXIT", "1")
	if err := ConvertToWAV(context.Background(), fake, "in.m4a", "out.wav"); err == nil {
		t.Error("failing ffmpeg must error")
	}
	if err := ConvertToWAV(context.Background(), filepath.Join(t.TempDir(), "nope"), "i", "o"); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("missing ffmpeg err = %v", err)
	}
}

func TestFFmpegRecorder(t *testing.T) {
	fake := testutil.BuildFakebin(t)
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	t.Setenv("FAKEBIN_ARGS_FILE", args)
	r := NewFFmpegRecorder(fake, "Mic (USB)")
	r.StopGrace = time.Second
	if r.Recording() || r.Stop() != nil {
		t.Fatal("idle recorder misbehaves")
	}
	out := filepath.Join(dir, "out.wav")
	if err := r.Start(context.Background(), out); err != nil {
		t.Fatal(err)
	}
	if !r.Recording() {
		t.Error("not recording after Start")
	}
	if err := r.Start(context.Background(), out); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("second Start err = %v", err)
	}
	if err := r.Stop(); err != nil {
		t.Errorf("Stop: %v", err)
	}
	if r.Recording() {
		t.Error("still recording after Stop")
	}
	b, _ := os.ReadFile(args)
	a := strings.Split(string(b), "\n")
	joined := strings.Join(a, " ")
	for _, want := range []string{"-ac 1", "-ar 16000", "-t 1800", "-loglevel error", out} {
		if !strings.Contains(joined, want) {
			t.Errorf("args %q lack %q", joined, want)
		}
	}
	switch runtime.GOOS {
	case "windows":
		if !strings.Contains(joined, "-f dshow -i audio=Mic (USB)") {
			t.Errorf("windows args = %q", joined)
		}
	case "darwin":
		if !strings.Contains(joined, "-f avfoundation -i :Mic (USB)") {
			t.Errorf("darwin args = %q", joined)
		}
	default:
		if !strings.Contains(joined, "-f pulse -i Mic (USB)") {
			t.Errorf("linux args = %q", joined)
		}
	}
}

func TestFFmpegRecorderKillsAfterGrace(t *testing.T) {
	fake := testutil.BuildFakebin(t)
	t.Setenv("FAKEBIN_SLEEP_MS", "20000")
	r := NewFFmpegRecorder(fake, "Mic")
	r.StopGrace = 200 * time.Millisecond
	if err := r.Start(context.Background(), filepath.Join(t.TempDir(), "o.wav")); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if err := r.Stop(); err != nil {
		t.Errorf("Stop after kill: %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Errorf("Stop took %v", d)
	}
	if r.Recording() {
		t.Error("still recording")
	}
}

func TestFFmpegRecorderErrors(t *testing.T) {
	r := NewFFmpegRecorder(filepath.Join(t.TempDir(), "nope"), "Mic")
	if err := r.Start(context.Background(), "o.wav"); !errors.Is(err, domain.ErrUnavailable) || r.Recording() {
		t.Errorf("missing ffmpeg: err=%v recording=%v", err, r.Recording())
	}
	if runtime.GOOS == "windows" {
		r = NewFFmpegRecorder(testutil.BuildFakebin(t), "")
		if err := r.Start(context.Background(), "o.wav"); !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "mic_device") {
			t.Errorf("no device err = %v", err)
		}
	}
	// ffmpeg that fails: Stop reports its first stderr line.
	fake := testutil.BuildFakebin(t)
	t.Setenv("FAKEBIN_EXIT", "1")
	t.Setenv("FAKEBIN_STDERR", "Could not find audio device\nmore")
	r = NewFFmpegRecorder(fake, "Mic")
	if err := r.Start(context.Background(), "o.wav"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := r.Stop(); err == nil || !strings.Contains(err.Error(), "Could not find audio device") || strings.Contains(err.Error(), "more") {
		t.Errorf("Stop err = %v", err)
	}
}
