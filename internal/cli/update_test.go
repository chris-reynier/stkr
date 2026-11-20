package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/chris-reynier/stkr/internal/buildinfo"
	"github.com/chris-reynier/stkr/internal/update"
)

// releaseServer serves one release v0.2.0 whose binary is "NEW".
func releaseServer(t *testing.T, badSum bool) *update.Updater {
	t.Helper()
	name := fmt.Sprintf("stkr_0.2.0_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	var buf bytes.Buffer
	if runtime.GOOS == "windows" {
		name = fmt.Sprintf("stkr_0.2.0_%s_%s.zip", runtime.GOOS, runtime.GOARCH)
		zw := zip.NewWriter(&buf)
		w, _ := zw.Create("stkr.exe")
		_, _ = w.Write([]byte("NEW"))
		_ = zw.Close()
	} else {
		gz := gzip.NewWriter(&buf)
		tw := tar.NewWriter(gz)
		_ = tw.WriteHeader(&tar.Header{Name: "stkr", Mode: 0o755, Size: 3, Typeflag: tar.TypeReg})
		_, _ = tw.Write([]byte("NEW"))
		_ = tw.Close()
		_ = gz.Close()
	}
	sum := sha256.Sum256(buf.Bytes())
	hexSum := hex.EncodeToString(sum[:])
	if badSum {
		hexSum = strings.Repeat("0", 64)
	}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/repos/o/r/releases"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"tag_name": "v0.2.0", "html_url": "https://example.test/v0.2.0",
				"assets": []map[string]string{
					{"name": name, "browser_download_url": srv.URL + "/a/" + name},
					{"name": "checksums.txt", "browser_download_url": srv.URL + "/a/checksums.txt"},
				}}})
		case r.URL.Path == "/a/"+name:
			_, _ = w.Write(buf.Bytes())
		case r.URL.Path == "/a/checksums.txt":
			fmt.Fprintf(w, "%s  %s\n", hexSum, name)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &update.Updater{Repo: "o/r", APIBase: srv.URL, HTTP: srv.Client()}
}

func execUpdate(t *testing.T, u *update.Updater, version, exe string, args ...string) (string, string, int) {
	t.Helper()
	home := t.TempDir()
	var so, se bytes.Buffer
	root, o := newRoot()
	o.updater, o.version, o.exePath = u, version, exe
	root.SetIn(strings.NewReader(""))
	code := run(root, o, append([]string{"--home", home}, args...), &so, &se)
	return so.String(), se.String(), code
}

func tempExe(t *testing.T) string {
	p := filepath.Join(t.TempDir(), "stkr")
	if err := os.WriteFile(p, []byte("OLD"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestUpdateCheck(t *testing.T) {
	u := releaseServer(t, false)
	out, _, code := execUpdate(t, u, "v0.1.0", "", "update", "--check")
	if code != ExitUpdateAvailable || !strings.Contains(out, "update available: v0.2.0") || !strings.Contains(out, "https://example.test/v0.2.0") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	out, _, code = execUpdate(t, u, "v0.2.0", "", "update", "--check")
	if code != 0 || !strings.Contains(out, "up to date") {
		t.Fatalf("code=%d out=%q", code, out)
	}
	out, _, code = execUpdate(t, u, "dev", "", "update", "--check", "--json")
	var res update.Result
	if code != ExitUpdateAvailable || json.Unmarshal([]byte(out), &res) != nil || !res.Available || res.Latest != "v0.2.0" {
		t.Fatalf("dev check: code=%d out=%q", code, out)
	}
	if _, se, code := execUpdate(t, u, "v0.1.0", "", "update", "--check", "--version", "v0.1.0"); code != ExitUsage || !strings.Contains(se, "cannot be combined") {
		t.Fatalf("code=%d se=%q", code, se)
	}
}

func TestUpdateApply(t *testing.T) {
	u := releaseServer(t, false)
	exe := tempExe(t)

	// Not a terminal and no --yes: refuse.
	if _, se, code := execUpdate(t, u, "v0.1.0", exe, "update"); code != ExitUsage || !strings.Contains(se, "--yes") {
		t.Fatalf("no yes: code=%d se=%q", code, se)
	}
	// Development build refuses without --force.
	if _, se, code := execUpdate(t, u, "dev", exe, "update", "--yes"); code != ExitUsage || !strings.Contains(se, "development build") {
		t.Fatalf("dev: code=%d se=%q", code, se)
	}
	if b, _ := os.ReadFile(exe); string(b) != "OLD" {
		t.Fatal("exe changed by refused update")
	}
	// Already newest: nothing to do.
	if out, _, code := execUpdate(t, u, "v0.2.0", exe, "update", "--yes"); code != 0 || !strings.Contains(out, "up to date") {
		t.Fatalf("latest: code=%d out=%q", code, out)
	}
	// Never downgrade without --version.
	if out, _, code := execUpdate(t, u, "v0.3.0", exe, "update", "--yes"); code != 0 || !strings.Contains(out, "up to date") {
		t.Fatalf("downgrade: code=%d out=%q", code, out)
	}
	if b, _ := os.ReadFile(exe); string(b) != "OLD" {
		t.Fatal("exe changed")
	}
	// Real update.
	out, _, code := execUpdate(t, u, "v0.1.0", exe, "update", "--yes")
	if code != 0 || !strings.Contains(out, "updated stkr v0.1.0 -> v0.2.0") {
		t.Fatalf("apply: code=%d out=%q", code, out)
	}
	if b, _ := os.ReadFile(exe); string(b) != "NEW" {
		t.Fatalf("exe = %q", b)
	}
	// Explicit --version may go down (here: onto a dev build with --force).
	exe2 := tempExe(t)
	if _, _, code := execUpdate(t, u, "dev", exe2, "update", "--yes", "--force", "--version", "0.2.0"); code != 0 {
		t.Fatalf("forced dev update: code=%d", code)
	}
	if b, _ := os.ReadFile(exe2); string(b) != "NEW" {
		t.Fatalf("exe2 = %q", b)
	}
	if _, se, code := execUpdate(t, u, "v0.1.0", tempExe(t), "update", "--yes", "--version", "v9.9.9"); code != ExitError || !strings.Contains(se, "not found") {
		t.Fatalf("unknown version: code=%d se=%q", code, se)
	}
}

func TestUpdateChecksumMismatch(t *testing.T) {
	u := releaseServer(t, true)
	exe := tempExe(t)
	_, se, code := execUpdate(t, u, "v0.1.0", exe, "update", "--yes")
	if code != ExitUsage || !strings.Contains(se, "checksum mismatch") {
		t.Fatalf("code=%d se=%q", code, se)
	}
	if b, _ := os.ReadFile(exe); string(b) != "OLD" {
		t.Fatalf("exe = %q", b)
	}
}

func TestVersionShowsCachedUpdateOnly(t *testing.T) {
	old := buildinfo.Version
	buildinfo.Version = "v0.1.0"
	defer func() { buildinfo.Version = old }()

	home := t.TempDir()
	// Fresh home: no database, nothing shown, nothing created.
	var so, se bytes.Buffer
	root, o := newRoot()
	if code := run(root, o, []string{"--home", home, "version"}, &so, &se); code != 0 || strings.Contains(so.String(), "update available") {
		t.Fatalf("fresh: %d %q", code, so.String())
	}
	if _, err := os.Stat(filepath.Join(home, "stkr.db")); err == nil {
		t.Fatal("version created the database")
	}

	// Prime the cache with a check, then `version` must use it offline.
	live := releaseServer(t, false)
	so.Reset()
	root, o = newRoot()
	o.updater, o.version = live, "v0.1.0"
	if code := run(root, o, []string{"--home", home, "update", "--check"}, &so, &se); code != ExitUpdateAvailable {
		t.Fatalf("prime: %d", code)
	}
	so.Reset()
	root, o = newRoot()
	if code := run(root, o, []string{"--home", home, "version"}, &so, &se); code != 0 || !strings.Contains(so.String(), "update available: v0.2.0") {
		t.Fatalf("version: %d %q", code, so.String())
	}
	so.Reset()
	root, o = newRoot()
	run(root, o, []string{"--home", home, "--json", "version"}, &so, &se)
	if !strings.Contains(so.String(), `"update_available": "v0.2.0"`) {
		t.Fatalf("json: %q", so.String())
	}
	// Once the running version is current, the line disappears.
	buildinfo.Version = "v0.2.0"
	so.Reset()
	root, o = newRoot()
	run(root, o, []string{"--home", home, "version"}, &so, &se)
	if strings.Contains(so.String(), "update available") {
		t.Fatalf("stale: %q", so.String())
	}
}
