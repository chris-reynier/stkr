package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/chris-reynier/stkr/internal/domain"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.1.0", "v0.1.0", 0},
		{"v0.1.1", "v0.1.0", 1},
		{"0.2.0", "v0.10.0", -1},
		{"v1.0.0", "v1.0.0-rc.1", 1},
		{"v1.0.0-rc.2", "v1.0.0-rc.10", -1},
		{"v1.0.0-alpha", "v1.0.0-1", 1},
		{"v1.0.0-rc", "v1.0.0-rc.1", -1},
		{"dev", "v0.0.1", -1},
		{"v0.0.1", "dev", 1},
		{"dev", "dev", 0},
		{"v1.2.3+build", "v1.2.3", 0},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
	if !IsDev("dev") || IsDev("v1.0.0") {
		t.Error("IsDev wrong")
	}
}

// fakeGitHub serves a release list and assets.
type fakeGitHub struct {
	srv      *httptest.Server
	releases []map[string]any
	files    map[string][]byte
	auth     string
	hits     int
}

func newFake(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{files: map[string][]byte{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits++
		if strings.HasPrefix(r.URL.Path, "/repos/o/r/releases") {
			f.auth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(f.releases)
			return
		}
		if b, ok := f.files[r.URL.Path]; ok {
			_, _ = w.Write(b)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeGitHub) updater(goos, goarch string) *Updater {
	return &Updater{Repo: "o/r", APIBase: f.srv.URL, HTTP: f.srv.Client(), GOOS: goos, GOARCH: goarch}
}

func tarGz(t *testing.T, name string, data []byte) []byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "README.md", Mode: 0o644, Size: 2, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("hi"))
	_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg})
	_, _ = tw.Write(data)
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func zipFile(t *testing.T, name string, data []byte) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create(name)
	_, _ = w.Write(data)
	_ = zw.Close()
	return buf.Bytes()
}

// addRelease publishes tag with an archive for goos/goarch. sum overrides the
// recorded checksum when non-empty.
func (f *fakeGitHub) addRelease(t *testing.T, tag string, pre bool, goos, goarch string, bin []byte, sum string) {
	ver := strings.TrimPrefix(tag, "v")
	name := fmt.Sprintf("stkr_%s_%s_%s.tar.gz", ver, goos, goarch)
	arc := tarGz(t, "stkr", bin)
	if goos == "windows" {
		name = fmt.Sprintf("stkr_%s_%s_%s.zip", ver, goos, goarch)
		arc = zipFile(t, "stkr.exe", bin)
	}
	if sum == "" {
		h := sha256.Sum256(arc)
		sum = hex.EncodeToString(h[:])
	}
	base := "/dl/" + tag + "/"
	f.files[base+name] = arc
	f.files[base+"checksums.txt"] = []byte(sum + "  " + name + "\n" + strings.Repeat("0", 64) + "  other\n")
	f.releases = append(f.releases, map[string]any{
		"tag_name": tag, "prerelease": pre, "html_url": "https://example.test/rel/" + tag,
		"assets": []map[string]string{
			{"name": name, "browser_download_url": f.srv.URL + base + name},
			{"name": "checksums.txt", "browser_download_url": f.srv.URL + base + "checksums.txt"},
		},
	})
}

func TestCheck(t *testing.T) {
	f := newFake(t)
	f.addRelease(t, "v0.1.0", false, "linux", "amd64", []byte("a"), "")
	f.addRelease(t, "v0.2.0", false, "linux", "amd64", []byte("b"), "")
	f.addRelease(t, "v0.3.0-rc.1", true, "linux", "amd64", []byte("c"), "")
	f.releases = append(f.releases, map[string]any{"tag_name": "v9.9.9", "draft": true})
	f.releases = append(f.releases, map[string]any{"tag_name": "nightly"})
	u := f.updater("linux", "amd64")
	ctx := context.Background()

	cases := []struct {
		current, channel, latest string
		avail                    bool
	}{
		{"v0.2.0", "stable", "v0.2.0", false},
		{"v0.1.0", "stable", "v0.2.0", true},
		{"dev", "stable", "v0.2.0", true},
		{"v0.3.0", "stable", "v0.2.0", false}, // never offers a downgrade
		{"v0.2.0", "beta", "v0.3.0-rc.1", true},
	}
	for _, c := range cases {
		r, err := u.Check(ctx, c.current, c.channel)
		if err != nil {
			t.Fatal(err)
		}
		if r.Latest != c.latest || r.Available != c.avail {
			t.Errorf("%+v: got %+v", c, r)
		}
	}
	r, _ := u.Check(ctx, "v0.1.0", "stable")
	if r.URL != "https://example.test/rel/v0.2.0" {
		t.Errorf("url %q", r.URL)
	}
}

func TestStableIgnoresPrereleaseOnly(t *testing.T) {
	f := newFake(t)
	f.addRelease(t, "v0.1.0-rc.1", true, "linux", "amd64", []byte("a"), "")
	u := f.updater("linux", "amd64")
	_, err := u.Latest(context.Background(), "stable")
	if !errors.Is(err, domain.ErrNotFound) || !strings.Contains(err.Error(), "beta") {
		t.Fatalf("stable err = %v", err)
	}
	rel, err := u.Latest(context.Background(), "beta")
	if err != nil || rel.Tag != "v0.1.0-rc.1" {
		t.Fatalf("beta = %+v %v", rel, err)
	}
}

func TestCheckErrors(t *testing.T) {
	f := newFake(t)
	u := f.updater("linux", "amd64")
	if _, err := u.Latest(context.Background(), "stable"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("empty list: %v", err)
	}
	f.srv.Close()
	if _, err := u.Latest(context.Background(), "stable"); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("closed server: %v", err)
	}
	bad := &Updater{Repo: "o/r", APIBase: "http://example.com"}
	if _, err := bad.Latest(context.Background(), "stable"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("plain http must be refused: %v", err)
	}
}

func TestTokenSentOnAPIOnly(t *testing.T) {
	f := newFake(t)
	f.addRelease(t, "v0.1.0", false, "linux", "amd64", []byte("a"), "")
	u := f.updater("linux", "amd64")
	u.Token = "tok123"
	if _, err := u.Latest(context.Background(), "stable"); err != nil {
		t.Fatal(err)
	}
	if f.auth != "Bearer tok123" {
		t.Errorf("auth = %q", f.auth)
	}
}

func newExe(t *testing.T, content string) string {
	p := filepath.Join(t.TempDir(), "stkr")
	if runtime.GOOS == "windows" {
		p += ".exe"
	}
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func read(t *testing.T, p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestApplyTarGzAndZip(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			f := newFake(t)
			f.addRelease(t, "v0.2.0", false, goos, "arm64", []byte("NEW BINARY"), "")
			u := f.updater(goos, "arm64")
			rel, err := u.Latest(context.Background(), "stable")
			if err != nil {
				t.Fatal(err)
			}
			exe := newExe(t, "OLD")
			if err := u.Apply(context.Background(), rel, exe); err != nil {
				t.Fatal(err)
			}
			if got := read(t, exe); got != "NEW BINARY" {
				t.Errorf("exe = %q", got)
			}
			// No stray temp files; .old only kept on Windows hosts.
			entries, _ := os.ReadDir(filepath.Dir(exe))
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".stkr-update-") {
					t.Errorf("leftover %s", e.Name())
				}
			}
			_, oldErr := os.Stat(OldPath(exe))
			if runtime.GOOS == "windows" && oldErr != nil {
				t.Error("expected .old on windows")
			}
			CleanupOld(exe)
			if _, err := os.Stat(OldPath(exe)); err == nil {
				t.Error("CleanupOld did not remove .old")
			}
		})
	}
}

func TestChecksumMismatchKeepsCurrent(t *testing.T) {
	f := newFake(t)
	f.addRelease(t, "v0.2.0", false, "linux", "amd64", []byte("EVIL"), strings.Repeat("a", 64))
	u := f.updater("linux", "amd64")
	rel, _ := u.Latest(context.Background(), "stable")
	exe := newExe(t, "OLD")
	err := u.Apply(context.Background(), rel, exe)
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v", err)
	}
	if read(t, exe) != "OLD" {
		t.Error("current binary was changed")
	}
	if _, err := os.Stat(OldPath(exe)); err == nil {
		t.Error(".old created on abort")
	}
}

func TestApplyMissingPieces(t *testing.T) {
	f := newFake(t)
	f.addRelease(t, "v0.2.0", false, "linux", "amd64", []byte("x"), "")
	rel, _ := f.updater("linux", "amd64").Latest(context.Background(), "stable")
	exe := newExe(t, "OLD")

	// platform without an asset
	err := f.updater("darwin", "arm64").Apply(context.Background(), rel, exe)
	if !errors.Is(err, domain.ErrNotFound) || !strings.Contains(err.Error(), "darwin/arm64") {
		t.Errorf("missing asset: %v", err)
	}
	// no checksums.txt
	noSum := rel
	noSum.Assets = rel.Assets[:1]
	if err := f.updater("linux", "amd64").Apply(context.Background(), noSum, exe); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("no checksums: %v", err)
	}
	// checksums.txt without our entry
	f.files["/dl/v0.2.0/checksums.txt"] = []byte(strings.Repeat("0", 64) + "  other\n")
	if err := f.updater("linux", "amd64").Apply(context.Background(), rel, exe); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("no entry: %v", err)
	}
	if read(t, exe) != "OLD" {
		t.Error("current binary changed")
	}
}

func TestExtractErrors(t *testing.T) {
	if _, err := extractBinary([]byte("junk"), "x.tar.gz"); err == nil {
		t.Error("junk tar.gz accepted")
	}
	if _, err := extractBinary([]byte("junk"), "x.zip"); err == nil {
		t.Error("junk zip accepted")
	}
	if _, err := extractBinary(tarGz(t, "other", []byte("x")), "x.tar.gz"); err == nil {
		t.Error("archive without stkr accepted")
	}
	if _, err := extractBinary(zipFile(t, "other.txt", []byte("x")), "x.zip"); err == nil {
		t.Error("zip without stkr accepted")
	}
	if _, err := extractBinary(zipFile(t, "stkr.exe", nil), "x.zip"); err == nil {
		t.Error("empty exe accepted")
	}
}

func TestReplaceRollback(t *testing.T) {
	exe := newExe(t, "OLD")
	orig := rename
	defer func() { rename = orig }()
	calls := 0
	rename = func(a, b string) error {
		calls++
		if calls == 2 { // moving the new file into place fails
			return errors.New("boom")
		}
		return os.Rename(a, b)
	}
	err := ReplaceExecutable(exe, []byte("NEW"))
	if err == nil || !strings.Contains(err.Error(), "previous version restored") {
		t.Fatalf("err = %v", err)
	}
	if read(t, exe) != "OLD" {
		t.Error("not rolled back")
	}
	entries, _ := os.ReadDir(filepath.Dir(exe))
	if len(entries) != 1 {
		t.Errorf("leftover files: %v", entries)
	}
}

func TestReplaceFailsWhenDirMissing(t *testing.T) {
	if err := ReplaceExecutable(filepath.Join(t.TempDir(), "nope", "stkr"), []byte("x")); err == nil {
		t.Error("expected error")
	}
	// current exe missing: nothing to move aside, keep the error path clean
	dir := t.TempDir()
	if err := ReplaceExecutable(filepath.Join(dir, "stkr"), []byte("x")); err == nil {
		t.Error("expected error when exe does not exist")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("leftovers: %v", entries)
	}
}

func TestFind(t *testing.T) {
	f := newFake(t)
	f.addRelease(t, "v0.1.0", false, "linux", "amd64", []byte("a"), "")
	f.addRelease(t, "v0.2.0", false, "linux", "amd64", []byte("b"), "")
	u := f.updater("linux", "amd64")
	rel, err := u.Find(context.Background(), "0.1.0")
	if err != nil || rel.Tag != "v0.1.0" {
		t.Fatalf("%+v %v", rel, err)
	}
	if _, err := u.Find(context.Background(), "v7.0.0"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestOversizeAPIRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte("x"), maxAPIBytes+10))
	}))
	defer srv.Close()
	u := &Updater{Repo: "o/r", APIBase: srv.URL, HTTP: srv.Client()}
	if _, err := u.Latest(context.Background(), "stable"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("err = %v", err)
	}
}
