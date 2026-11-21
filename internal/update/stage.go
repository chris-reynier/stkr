package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/chris-reynier/stkr/internal/domain"
)

// InstallFile is the file the Windows setup writes next to the programs to
// say how this copy was installed (contract install-update.md).
const InstallFile = "install.json"

// Install methods. MethodSetup is only ever written by the Windows setup.
const (
	MethodSetup   = "setup"   // run the verified setup.exe silently
	MethodReplace = "replace" // replace the program files from the archive
)

// Install is the content of install.json.
type Install struct {
	Method  string `json:"method"`
	Version string `json:"version"`
}

// ReadInstall reads <dir>/install.json. ok is false when the file is missing
// or not valid, which means "not installed by the setup".
func ReadInstall(dir string) (Install, bool) {
	f, err := os.Open(filepath.Join(dir, InstallFile)) //nolint:gosec // G304: dir is the folder of the running program
	if err != nil {
		return Install{}, false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return Install{}, false
	}
	var in Install
	if json.Unmarshal(data, &in) != nil || in.Method == "" {
		return Install{}, false
	}
	return in, true
}

// SetupArgs are the arguments the setup is started with: silent, no restart,
// per-user. The setup closes a running STKR itself.
var SetupArgs = []string{"/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART", "/CURRENTUSER"}

// RelaunchArg makes the silent setup start stkrw again when it is done.
const RelaunchArg = "/RELAUNCH=1"

// Starter starts a program and does not wait for it.
type Starter func(ctx context.Context, path string, args []string) error

// StartDetached starts path with args and returns without waiting. The child
// is not tied to ctx: it must outlive the caller (a setup replaces the
// caller's files, a relaunched app takes over from it).
func StartDetached(ctx context.Context, path string, args []string) error {
	cmd := exec.CommandContext(context.WithoutCancel(ctx), path, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", filepath.Base(path), err)
	}
	return cmd.Process.Release()
}

func (u *Updater) start() Starter {
	if u.Start != nil {
		return u.Start
	}
	return StartDetached
}

func (u *Updater) tempDir() string {
	if u.TempDir != "" {
		return u.TempDir
	}
	return os.TempDir()
}

// FetchOptions tune Fetch.
type FetchOptions struct {
	// Relaunch asks the setup to start stkrw again when it has finished.
	Relaunch bool
}

// Staged is a verified update that is ready to be committed. Nothing about
// the installed programs has changed until Commit.
type Staged struct {
	Tag     string
	Version string
	Method  string // MethodSetup or MethodReplace
	// Files are the program files Commit replaces (MethodReplace).
	Files []string

	files    []stagedFile
	setup    string // setup.exe path (MethodSetup)
	setupDir string
	args     []string
	start    Starter
}

type stagedFile struct {
	target string
	tmp    string
	exists bool // the target is moved aside; false = a new program is installed
}

// setupName is the setup asset of rel for the updater's platform.
func (u *Updater) setupName(rel Release) string {
	return fmt.Sprintf("stkr_%s_windows_%s_setup.exe", rel.Version(), u.goarch())
}

func findAsset(rel Release, name string) (Asset, bool) {
	for _, a := range rel.Assets {
		if a.Name == name {
			return a, true
		}
	}
	return Asset{}, false
}

// download fetches the asset and checks its SHA-256 against checksums.txt of
// the release. A missing checksums.txt or entry refuses the update.
func (u *Updater) download(ctx context.Context, rel Release, asset Asset) ([]byte, error) {
	sumAsset, ok := findAsset(rel, "checksums.txt")
	if !ok {
		return nil, domain.New(domain.ErrInvalid, "release has no checksums.txt; refusing to install")
	}
	sums, err := u.get(ctx, sumAsset.URL, maxChecksumBytes, false)
	if err != nil {
		return nil, err
	}
	want, err := findChecksum(sums, asset.Name)
	if err != nil {
		return nil, err
	}
	data, err := u.get(ctx, asset.URL, maxArchiveBytes, false)
	if err != nil {
		return nil, err
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != want {
		return nil, domain.New(domain.ErrInvalid, "checksum mismatch: download is corrupt or tampered; update aborted, current version kept")
	}
	return data, nil
}

// Method reports how an update of the programs at exePath would be installed
// from rel: MethodSetup when install.json (next to exePath) says the setup
// installed them on Windows and the release has a setup for this platform,
// else MethodReplace.
func (u *Updater) Method(rel Release, exePath string) string {
	if u.goos() != "windows" {
		return MethodReplace
	}
	in, ok := ReadInstall(filepath.Dir(exePath))
	if !ok || in.Method != MethodSetup {
		return MethodReplace
	}
	if _, ok := findAsset(rel, u.setupName(rel)); !ok {
		return MethodReplace
	}
	return MethodSetup
}

// Fetch downloads rel, verifies it against checksums.txt and prepares the
// installation, without changing any installed program. Commit finishes it,
// Discard throws it away.
//
// A copy installed by the Windows setup (install.json says "setup") is
// updated by running the verified setup silently. Every other copy has all
// STKR programs next to exePath replaced from the archive, all or none.
func (u *Updater) Fetch(ctx context.Context, rel Release, exePath string, opt FetchOptions) (*Staged, error) {
	if u.Method(rel, exePath) == MethodSetup {
		return u.fetchSetup(ctx, rel, opt)
	}
	return u.fetchReplace(ctx, rel, exePath)
}

func (u *Updater) fetchSetup(ctx context.Context, rel Release, opt FetchOptions) (*Staged, error) {
	name := u.setupName(rel)
	asset, _ := findAsset(rel, name)
	data, err := u.download(ctx, rel, asset)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(u.tempDir(), "stkr-update-")
	if err != nil {
		return nil, fmt.Errorf("prepare the setup: %w", err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o700); err != nil { //nolint:gosec // G306: the setup must be executable, the folder is private (MkdirTemp, 0700)
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("prepare the setup: %w", err)
	}
	args := append([]string(nil), SetupArgs...)
	if opt.Relaunch {
		args = append(args, RelaunchArg)
	}
	return &Staged{Tag: rel.Tag, Version: rel.Version(), Method: MethodSetup, setup: p, setupDir: dir, args: args, start: u.start()}, nil
}

func (u *Updater) fetchReplace(ctx context.Context, rel Release, exePath string) (*Staged, error) {
	asset, err := u.assetFor(rel)
	if err != nil {
		return nil, err
	}
	archive, err := u.download(ctx, rel, asset)
	if err != nil {
		return nil, err
	}
	progs, err := extractPrograms(archive, asset.Name)
	if err != nil {
		return nil, err
	}
	targets, err := planTargets(exePath, progs)
	if err != nil {
		return nil, err
	}
	st := &Staged{Tag: rel.Tag, Version: rel.Version(), Method: MethodReplace}
	for _, t := range targets {
		sf, err := stageFile(t)
		if err != nil {
			st.Discard()
			return nil, err
		}
		st.files = append(st.files, sf)
		st.Files = append(st.Files, t.path)
	}
	return st, nil
}

type target struct {
	path     string
	data     []byte
	required bool // must exist already (the running program)
}

// planTargets decides which files an archive replaces. The program kind of
// exePath (stkr, or stkrw when the desktop app updates) replaces exePath
// itself, whatever the file is called; every other STKR program replaces the
// file of the same name beside it. A program that is not there yet is
// installed only when the folder was set up by the installer (it has an
// install.json); a plain unzipped folder only gets what it already has.
func planTargets(exePath string, progs []program) ([]target, error) {
	dir := filepath.Dir(exePath)
	primary := programKind(filepath.Base(exePath))
	if primary == "" {
		primary = "stkr"
	}
	_, installDir := ReadInstall(dir)
	var out []target
	found := false
	for _, p := range progs {
		if p.Kind == primary {
			out = append(out, target{path: exePath, data: p.Data, required: true})
			found = true
			continue
		}
		tp := filepath.Join(dir, p.Name)
		if _, err := os.Stat(tp); err == nil || installDir {
			out = append(out, target{path: tp, data: p.Data})
		}
	}
	if !found {
		return nil, domain.New(domain.ErrInvalid, "archive does not contain the "+primary+" executable")
	}
	return out, nil
}

// stageFile writes the new program next to its target (same folder, so the
// final rename is atomic) and flushes it to disk.
func stageFile(t target) (stagedFile, error) {
	dir := filepath.Dir(t.path)
	mode := os.FileMode(0o755)
	fi, statErr := os.Stat(t.path)
	exists := statErr == nil
	if exists {
		mode = fi.Mode().Perm() | 0o111
	}
	tmp, err := os.CreateTemp(dir, ".stkr-update-*")
	if err != nil {
		return stagedFile{}, fmt.Errorf("write new executable (is %s writable?): %w", dir, err)
	}
	name := tmp.Name()
	fail := func(err error) (stagedFile, error) {
		_ = tmp.Close()
		_ = os.Remove(name)
		return stagedFile{}, fmt.Errorf("write new executable: %w", err)
	}
	if _, err := tmp.Write(t.data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return stagedFile{}, fmt.Errorf("write new executable: %w", err)
	}
	if err := os.Chmod(name, mode); err != nil {
		_ = os.Remove(name)
		return stagedFile{}, fmt.Errorf("write new executable: %w", err)
	}
	if t.required && !exists {
		_ = os.Remove(name)
		return stagedFile{}, fmt.Errorf("move current executable aside: %s does not exist", filepath.Base(t.path))
	}
	return stagedFile{target: t.path, tmp: name, exists: exists}, nil
}

// Discard removes what Fetch prepared. It is safe to call after Commit.
func (s *Staged) Discard() {
	for _, f := range s.files {
		_ = os.Remove(f.tmp)
	}
	s.files = nil
	// The setup is deliberately kept after Commit: it is still running.
}

// rename is os.Rename; tests replace it to simulate failures.
var rename = os.Rename

// Commit installs the staged update. For MethodReplace every program is moved
// aside (<exe>.old) and replaced; if any step fails the ones already done are
// restored, so the result is all or nothing. A running Windows program can be
// renamed but not overwritten, so the same steps are used on every OS. For
// MethodSetup the verified setup is started and Commit returns at once: the
// setup replaces the caller's files, so the caller should exit.
func (s *Staged) Commit(ctx context.Context) error {
	if s.Method == MethodSetup {
		if err := s.start(ctx, s.setup, s.args); err != nil {
			_ = os.RemoveAll(s.setupDir)
			return err
		}
		return nil
	}
	var done []stagedFile
	restore := func() error {
		var first error
		for i := len(done) - 1; i >= 0; i-- {
			d := done[i]
			var err error
			if d.exists {
				err = rename(OldPath(d.target), d.target)
			} else {
				err = os.Remove(d.target)
			}
			if err != nil && first == nil {
				first = err
			}
		}
		return first
	}
	fail := func(what string, err error) error {
		s.Discard()
		if rb := restore(); rb != nil {
			return fmt.Errorf("%s failed (%v) and rollback failed: %w", what, err, rb)
		}
		return fmt.Errorf("%s (previous version restored): %w", what, err)
	}
	for _, f := range s.files {
		old := OldPath(f.target)
		if f.exists {
			_ = os.Remove(old)
			if err := rename(f.target, old); err != nil {
				s.Discard()
				if rb := restore(); rb != nil {
					return fmt.Errorf("move current executable aside failed (%v) and rollback failed: %w", err, rb)
				}
				return fmt.Errorf("move current executable aside: %w", err)
			}
		}
		if err := rename(f.tmp, f.target); err != nil {
			if f.exists {
				if rb := rename(old, f.target); rb != nil {
					s.Discard()
					_ = restore()
					return fmt.Errorf("install new executable failed (%v) and rollback failed: %w", err, rb)
				}
			}
			return fail("install new executable", err)
		}
		done = append(done, f)
	}
	s.files = nil
	if runtime.GOOS != "windows" {
		for _, d := range done {
			if d.exists {
				_ = os.Remove(OldPath(d.target))
			}
		}
	}
	return nil
}

// ReplaceExecutable replaces the single program exePath with data, keeping
// the previous file as <exe>.old until the next start (Windows) or removing
// it (elsewhere). If the final rename fails the old file is restored.
func ReplaceExecutable(exePath string, data []byte) error {
	sf, err := stageFile(target{path: exePath, data: data, required: true})
	if err != nil {
		return err
	}
	st := &Staged{Method: MethodReplace, files: []stagedFile{sf}}
	return st.Commit(context.Background())
}

// CleanupOld removes the <exe>.old files left by a previous Windows update:
// the one of exePath and those of the other STKR programs beside it. It is
// best effort and silent.
func CleanupOld(exePath string) {
	if exePath == "" {
		return
	}
	_ = os.Remove(OldPath(exePath))
	dir := filepath.Dir(exePath)
	for _, n := range []string{"stkr.exe", "stkrw.exe"} {
		if !strings.EqualFold(n, filepath.Base(exePath)) {
			_ = os.Remove(OldPath(filepath.Join(dir, n)))
		}
	}
}
