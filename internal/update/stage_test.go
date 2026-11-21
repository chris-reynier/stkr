package update

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/testutil"
)

func zipFiles(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte(data))
	}
	_ = zw.Close()
	return buf.Bytes()
}

func sumOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// addWindowsRelease publishes a Windows zip holding the given programs and,
// when setup is not nil, the setup.exe too. Both are listed in checksums.txt.
func (f *fakeGitHub) addWindowsRelease(t *testing.T, tag string, programs map[string]string, setup []byte) {
	t.Helper()
	ver := strings.TrimPrefix(tag, "v")
	zname := fmt.Sprintf("stkr_%s_windows_amd64.zip", ver)
	arc := zipFiles(t, programs)
	base := "/dl/" + tag + "/"
	f.files[base+zname] = arc
	sums := sumOf(arc) + "  " + zname + "\n"
	assets := []map[string]string{
		{"name": zname, "browser_download_url": f.srv.URL + base + zname},
		{"name": "checksums.txt", "browser_download_url": f.srv.URL + base + "checksums.txt"},
	}
	if setup != nil {
		sname := fmt.Sprintf("stkr_%s_windows_amd64_setup.exe", ver)
		f.files[base+sname] = setup
		sums += sumOf(setup) + "  " + sname + "\n"
		assets = append(assets, map[string]string{"name": sname, "browser_download_url": f.srv.URL + base + sname})
	}
	f.files[base+"checksums.txt"] = []byte(sums)
	f.releases = append(f.releases, map[string]any{"tag_name": tag, "html_url": "https://example.test/rel/" + tag, "assets": assets})
}

// installDir returns a folder holding stkr.exe and stkrw.exe with the given
// content.
func installDir(t *testing.T, cli, desk string, withInstall bool) (dir string) {
	t.Helper()
	dir = t.TempDir()
	for name, body := range map[string]string{"stkr.exe": cli, "stkrw.exe": desk} {
		if body == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if withInstall {
		if err := os.WriteFile(filepath.Join(dir, InstallFile), []byte(`{"method":"zip-test"}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func latest(t *testing.T, u *Updater) Release {
	t.Helper()
	rel, err := u.Latest(context.Background(), "stable")
	if err != nil {
		t.Fatal(err)
	}
	return rel
}

func TestApplyReplacesEveryProgram(t *testing.T) {
	f := newFake(t)
	f.addWindowsRelease(t, "v0.2.0", map[string]string{"stkr.exe": "NEW CLI", "stkrw.exe": "NEW DESK", "README.md": "x"}, nil)
	u := f.updater("windows", "amd64")
	rel := latest(t, u)

	// Called from the CLI and from the desktop app: same result.
	for _, running := range []string{"stkr.exe", "stkrw.exe"} {
		t.Run(running, func(t *testing.T) {
			dir := installDir(t, "OLD CLI", "OLD DESK", false)
			if err := u.Apply(context.Background(), rel, filepath.Join(dir, running)); err != nil {
				t.Fatal(err)
			}
			if got := read(t, filepath.Join(dir, "stkr.exe")); got != "NEW CLI" {
				t.Errorf("stkr.exe = %q", got)
			}
			if got := read(t, filepath.Join(dir, "stkrw.exe")); got != "NEW DESK" {
				t.Errorf("stkrw.exe = %q", got)
			}
			if _, err := os.Stat(filepath.Join(dir, "README.md")); err == nil {
				t.Error("a non-program file was installed")
			}
			CleanupOld(filepath.Join(dir, running))
			entries, _ := os.ReadDir(dir)
			if len(entries) != 2 {
				t.Errorf("leftover files after CleanupOld: %v", entries)
			}
		})
	}
}

func TestApplyIsAllOrNothing(t *testing.T) {
	f := newFake(t)
	f.addWindowsRelease(t, "v0.2.0", map[string]string{"stkr.exe": "NEW CLI", "stkrw.exe": "NEW DESK"}, nil)
	u := f.updater("windows", "amd64")
	rel := latest(t, u)

	for _, failAt := range []int{3, 4} { // second file moved aside / second file moved into place
		t.Run(fmt.Sprintf("rename %d fails", failAt), func(t *testing.T) {
			dir := installDir(t, "OLD CLI", "OLD DESK", false)
			orig := rename
			defer func() { rename = orig }()
			calls := 0
			rename = func(a, b string) error {
				calls++
				if calls == failAt {
					return errors.New("boom")
				}
				return os.Rename(a, b)
			}
			err := u.Apply(context.Background(), rel, filepath.Join(dir, "stkr.exe"))
			if err == nil || !strings.Contains(err.Error(), "boom") {
				t.Fatalf("err = %v", err)
			}
			if got := read(t, filepath.Join(dir, "stkr.exe")); got != "OLD CLI" {
				t.Errorf("stkr.exe = %q, want the old one back", got)
			}
			if got := read(t, filepath.Join(dir, "stkrw.exe")); got != "OLD DESK" {
				t.Errorf("stkrw.exe = %q, want the old one back", got)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 2 {
				names := []string{}
				for _, e := range entries {
					names = append(names, e.Name())
				}
				t.Errorf("leftover files: %v", names)
			}
		})
	}
}

func TestMissingProgramsInstalledOnlyInInstallDir(t *testing.T) {
	f := newFake(t)
	f.addWindowsRelease(t, "v0.2.0", map[string]string{"stkr.exe": "NEW CLI", "stkrw.exe": "NEW DESK"}, nil)
	u := f.updater("windows", "amd64")
	rel := latest(t, u)

	plain := installDir(t, "OLD CLI", "", false)
	if err := u.Apply(context.Background(), rel, filepath.Join(plain, "stkr.exe")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(plain, "stkrw.exe")); err == nil {
		t.Error("stkrw.exe was added to a folder that is not the install dir")
	}

	inst := installDir(t, "OLD CLI", "", true)
	if err := u.Apply(context.Background(), rel, filepath.Join(inst, "stkr.exe")); err != nil {
		t.Fatal(err)
	}
	if got := read(t, filepath.Join(inst, "stkrw.exe")); got != "NEW DESK" {
		t.Errorf("stkrw.exe = %q", got)
	}
	// All or nothing also covers the new program: a failure removes it again.
	inst2 := installDir(t, "OLD CLI", "", true)
	orig := rename
	defer func() { rename = orig }()
	calls := 0
	rename = func(a, b string) error {
		calls++
		if calls == 3 {
			return errors.New("boom")
		}
		return os.Rename(a, b)
	}
	if err := u.Apply(context.Background(), rel, filepath.Join(inst2, "stkr.exe")); err == nil {
		t.Fatal("expected failure")
	}
	if _, err := os.Stat(filepath.Join(inst2, "stkrw.exe")); err == nil {
		t.Error("new program left behind after a failed update")
	}
	if got := read(t, filepath.Join(inst2, "stkr.exe")); got != "OLD CLI" {
		t.Errorf("stkr.exe = %q", got)
	}
}

func TestArchiveWithoutRunningProgramRefused(t *testing.T) {
	f := newFake(t)
	f.addWindowsRelease(t, "v0.2.0", map[string]string{"stkr.exe": "NEW CLI"}, nil)
	u := f.updater("windows", "amd64")
	rel := latest(t, u)
	dir := installDir(t, "OLD CLI", "OLD DESK", false)
	err := u.Apply(context.Background(), rel, filepath.Join(dir, "stkrw.exe"))
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "stkrw") {
		t.Fatalf("err = %v", err)
	}
	if read(t, filepath.Join(dir, "stkr.exe")) != "OLD CLI" {
		t.Error("program changed")
	}
}

func TestFetchChangesNothingUntilCommit(t *testing.T) {
	f := newFake(t)
	f.addWindowsRelease(t, "v0.2.0", map[string]string{"stkr.exe": "NEW CLI", "stkrw.exe": "NEW DESK"}, nil)
	u := f.updater("windows", "amd64")
	rel := latest(t, u)
	dir := installDir(t, "OLD CLI", "OLD DESK", false)
	exe := filepath.Join(dir, "stkrw.exe")
	st, err := u.Fetch(context.Background(), rel, exe, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if st.Method != MethodReplace || st.Version != "0.2.0" || len(st.Files) != 2 {
		t.Fatalf("staged = %+v", st)
	}
	if read(t, exe) != "OLD DESK" || read(t, filepath.Join(dir, "stkr.exe")) != "OLD CLI" {
		t.Fatal("programs changed before Commit")
	}
	st.Discard()
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("Discard left files: %v", entries)
	}
	// Fetch on a read-only target folder fails before anything changes.
	if _, err := u.Fetch(context.Background(), rel, filepath.Join(dir, "nope", "stkr.exe"), FetchOptions{}); err == nil {
		t.Error("expected an error for a missing folder")
	}
}

func TestReadInstallAndMethod(t *testing.T) {
	dir := t.TempDir()
	if _, ok := ReadInstall(dir); ok {
		t.Error("no file must not be an install")
	}
	for _, bad := range []string{"", "not json", `{"version":"1"}`} {
		_ = os.WriteFile(filepath.Join(dir, InstallFile), []byte(bad), 0o644)
		if _, ok := ReadInstall(dir); ok {
			t.Errorf("%q accepted", bad)
		}
	}
	_ = os.WriteFile(filepath.Join(dir, InstallFile), []byte(`{"method":"setup","version":"0.1.0"}`+"\r\n"), 0o644)
	in, ok := ReadInstall(dir)
	if !ok || in.Method != MethodSetup || in.Version != "0.1.0" {
		t.Fatalf("install = %+v, %v", in, ok)
	}

	rel := Release{Tag: "v0.2.0", Assets: []Asset{{Name: "stkr_0.2.0_windows_amd64_setup.exe"}}}
	exe := filepath.Join(dir, "stkrw.exe")
	win := &Updater{GOOS: "windows", GOARCH: "amd64"}
	if got := win.Method(rel, exe); got != MethodSetup {
		t.Errorf("windows + install.json = %s", got)
	}
	if got := (&Updater{GOOS: "linux", GOARCH: "amd64"}).Method(rel, exe); got != MethodReplace {
		t.Errorf("linux = %s", got)
	}
	if got := (&Updater{GOOS: "windows", GOARCH: "arm64"}).Method(rel, exe); got != MethodReplace {
		t.Errorf("no setup for the arch = %s", got)
	}
	if got := win.Method(rel, filepath.Join(t.TempDir(), "stkr.exe")); got != MethodReplace {
		t.Errorf("no install.json = %s", got)
	}
}

func setupInstall(t *testing.T) string {
	t.Helper()
	dir := installDir(t, "OLD CLI", "OLD DESK", false)
	if err := os.WriteFile(filepath.Join(dir, InstallFile), []byte(`{"method":"setup","version":"0.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSetupPathStartsVerifiedSetup(t *testing.T) {
	f := newFake(t)
	f.addWindowsRelease(t, "v0.2.0", map[string]string{"stkr.exe": "NEW CLI", "stkrw.exe": "NEW DESK"}, []byte("SETUP BYTES"))
	u := f.updater("windows", "amd64")
	u.TempDir = t.TempDir()
	rel := latest(t, u)
	dir := setupInstall(t)

	for _, relaunch := range []bool{false, true} {
		var gotPath string
		var gotArgs []string
		var gotBody string
		u.Start = func(_ context.Context, path string, args []string) error {
			gotPath, gotArgs = path, args
			gotBody = read(t, path)
			return nil
		}
		st, err := u.Fetch(context.Background(), rel, filepath.Join(dir, "stkrw.exe"), FetchOptions{Relaunch: relaunch})
		if err != nil {
			t.Fatal(err)
		}
		if st.Method != MethodSetup {
			t.Fatalf("method = %s", st.Method)
		}
		if gotPath != "" {
			t.Fatal("the setup was started by Fetch")
		}
		if err := st.Commit(context.Background()); err != nil {
			t.Fatal(err)
		}
		if gotBody != "SETUP BYTES" || filepath.Base(gotPath) != "stkr_0.2.0_windows_amd64_setup.exe" {
			t.Errorf("started %s with %q", gotPath, gotBody)
		}
		want := "/VERYSILENT /SUPPRESSMSGBOXES /NORESTART /CURRENTUSER"
		if relaunch {
			want += " /RELAUNCH=1"
		}
		if strings.Join(gotArgs, " ") != want {
			t.Errorf("args = %q, want %q", gotArgs, want)
		}
		// The installed programs are not touched by the setup path.
		if read(t, filepath.Join(dir, "stkr.exe")) != "OLD CLI" {
			t.Error("program replaced")
		}
	}
}

func TestSetupChecksumMismatchNotStarted(t *testing.T) {
	f := newFake(t)
	f.addWindowsRelease(t, "v0.2.0", map[string]string{"stkr.exe": "NEW CLI"}, []byte("SETUP BYTES"))
	f.files["/dl/v0.2.0/checksums.txt"] = []byte(strings.Repeat("a", 64) + "  stkr_0.2.0_windows_amd64_setup.exe\n")
	u := f.updater("windows", "amd64")
	u.TempDir = t.TempDir()
	started := false
	u.Start = func(context.Context, string, []string) error { started = true; return nil }
	rel := latest(t, u)
	_, err := u.Fetch(context.Background(), rel, filepath.Join(setupInstall(t), "stkrw.exe"), FetchOptions{})
	if !errors.Is(err, domain.ErrInvalid) || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("err = %v", err)
	}
	if started {
		t.Error("an unverified setup was started")
	}
	if entries, _ := os.ReadDir(u.TempDir); len(entries) != 0 {
		t.Errorf("temp files left: %v", entries)
	}
}

func TestSetupFallsBackToReplaceWithoutSetupAsset(t *testing.T) {
	f := newFake(t)
	f.addWindowsRelease(t, "v0.2.0", map[string]string{"stkr.exe": "NEW CLI", "stkrw.exe": "NEW DESK"}, nil)
	u := f.updater("windows", "amd64")
	dir := setupInstall(t)
	if err := u.Apply(context.Background(), latest(t, u), filepath.Join(dir, "stkrw.exe")); err != nil {
		t.Fatal(err)
	}
	if read(t, filepath.Join(dir, "stkrw.exe")) != "NEW DESK" {
		t.Error("fallback did not replace")
	}
}

func TestSetupStartFailureCleansUp(t *testing.T) {
	f := newFake(t)
	f.addWindowsRelease(t, "v0.2.0", map[string]string{"stkr.exe": "NEW CLI"}, []byte("SETUP"))
	u := f.updater("windows", "amd64")
	u.TempDir = t.TempDir()
	u.Start = func(context.Context, string, []string) error { return errors.New("cannot start") }
	rel := latest(t, u)
	st, err := u.Fetch(context.Background(), rel, filepath.Join(setupInstall(t), "stkr.exe"), FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Commit(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	if entries, _ := os.ReadDir(u.TempDir); len(entries) != 0 {
		t.Errorf("temp files left: %v", entries)
	}
}

// A real child process: the downloaded "setup" is the fakebin program, which
// records its arguments. It also proves the child is not tied to the context.
func TestStartDetachedRunsTheSetup(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a helper program")
	}
	fake := testutil.BuildFakebin(t)
	body, err := os.ReadFile(fake)
	if err != nil {
		t.Fatal(err)
	}
	argsFile := filepath.Join(t.TempDir(), "args.txt")
	t.Setenv("FAKEBIN_ARGS_FILE", argsFile)

	f := newFake(t)
	f.addWindowsRelease(t, "v0.2.0", map[string]string{"stkr.exe": "NEW CLI"}, body)
	u := f.updater("windows", "amd64")
	u.TempDir = t.TempDir()
	rel := latest(t, u)
	ctx, cancel := context.WithCancel(context.Background())
	st, err := u.Fetch(ctx, rel, filepath.Join(setupInstall(t), "stkr.exe"), FetchOptions{Relaunch: true})
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		// The staged file is named .exe but is a plain executable.
		if err := os.Chmod(st.setup, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	cancel()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, err := os.ReadFile(argsFile)
		if err == nil && len(b) > 0 {
			want := "/VERYSILENT\n/SUPPRESSMSGBOXES\n/NORESTART\n/CURRENTUSER\n/RELAUNCH=1"
			if string(b) != want {
				t.Fatalf("setup args = %q", b)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the setup never ran")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestExtractProgramsAndKinds(t *testing.T) {
	progs, err := extractPrograms(zipFiles(t, map[string]string{"bin/STKRW.EXE": "d", "stkr.exe": "c", "notes.txt": "n"}), "x.zip")
	if err != nil || len(progs) != 2 {
		t.Fatalf("progs = %v, %v", progs, err)
	}
	kinds := map[string]string{}
	for _, p := range progs {
		kinds[p.Kind] = string(p.Data)
	}
	if kinds["stkr"] != "c" || kinds["stkrw"] != "d" {
		t.Errorf("kinds = %v", kinds)
	}
	for in, want := range map[string]string{"stkr": "stkr", "STKR.exe": "stkr", `a\stkrw.exe`: "stkrw", "stkrw": "", "other.exe": ""} {
		if got := programKind(in); got != want {
			t.Errorf("programKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCleanupOldRemovesSiblings(t *testing.T) {
	dir := installDir(t, "c", "d", false)
	for _, n := range []string{"stkr.exe.old", "stkrw.exe.old"} {
		_ = os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	CleanupOld(filepath.Join(dir, "stkrw.exe"))
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Errorf("left: %v", entries)
	}
	CleanupOld("")
}
