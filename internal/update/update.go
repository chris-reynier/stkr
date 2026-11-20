// Package update checks GitHub Releases for a newer STKR build and replaces
// the running executable after verifying the archive against checksums.txt.
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
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
)

// DefaultRepo is the repository releases are read from.
const DefaultRepo = "chris-reynier/stkr"

// DefaultAPI is the GitHub API base URL.
const DefaultAPI = "https://api.github.com"

// Channels accepted by Latest. "beta" also accepts prereleases.
const (
	ChannelStable = "stable"
	ChannelBeta   = "beta"
)

const (
	maxAPIBytes      = 4 << 20
	maxChecksumBytes = 1 << 20
	maxArchiveBytes  = 150 << 20
	maxBinaryBytes   = 150 << 20
)

// Release is the part of a GitHub release the updater needs.
type Release struct {
	Tag        string
	Prerelease bool
	URL        string // release notes page
	Assets     []Asset
}

// Version returns the tag without the leading "v".
func (r Release) Version() string { return strings.TrimPrefix(r.Tag, "v") }

// Asset is a downloadable file of a release.
type Asset struct {
	Name string
	URL  string
}

// Updater talks to a GitHub-compatible releases API.
type Updater struct {
	Repo    string
	APIBase string
	Token   string // optional GITHUB_TOKEN; never logged
	HTTP    *http.Client
	GOOS    string
	GOARCH  string
}

// New returns an Updater for the default repository. GITHUB_TOKEN, when set,
// raises the API rate limit.
func New() *Updater {
	return &Updater{Repo: DefaultRepo, APIBase: DefaultAPI, Token: os.Getenv("GITHUB_TOKEN")}
}

func (u *Updater) client() *http.Client {
	if u.HTTP != nil {
		return u.HTTP
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

func (u *Updater) goos() string {
	if u.GOOS != "" {
		return u.GOOS
	}
	return runtime.GOOS
}

func (u *Updater) goarch() string {
	if u.GOARCH != "" {
		return u.GOARCH
	}
	return runtime.GOARCH
}

// checkURL allows https, and http only for loopback (tests, local mirrors).
func checkURL(raw string) error {
	p, err := url.Parse(raw)
	if err != nil || p.Host == "" {
		return domain.New(domain.ErrInvalid, "invalid update URL")
	}
	if p.Scheme == "https" {
		return nil
	}
	h := p.Hostname()
	if p.Scheme == "http" && (h == "127.0.0.1" || h == "localhost" || h == "::1") {
		return nil
	}
	return domain.New(domain.ErrInvalid, "update URL must use https")
}

func (u *Updater) get(ctx context.Context, rawURL string, limit int64, api bool) ([]byte, error) {
	if err := checkURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "stkr-update")
	if api {
		req.Header.Set("Accept", "application/vnd.github+json")
		if u.Token != "" {
			req.Header.Set("Authorization", "Bearer "+u.Token)
		}
	}
	resp, err := u.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrUnavailable, redactErr(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: server answered %d", domain.ErrUnavailable, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrUnavailable, redactErr(err))
	}
	if int64(len(data)) > limit {
		return nil, domain.New(domain.ErrInvalid, "download is too large")
	}
	return data, nil
}

// redactErr drops the URL from network errors so messages stay short.
func redactErr(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err.Error()
	}
	return err.Error()
}

type ghRelease struct {
	Tag        string `json:"tag_name"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	HTMLURL    string `json:"html_url"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (g ghRelease) release() Release {
	r := Release{Tag: g.Tag, Prerelease: g.Prerelease, URL: g.HTMLURL}
	for _, a := range g.Assets {
		r.Assets = append(r.Assets, Asset{Name: a.Name, URL: a.URL})
	}
	return r
}

func (u *Updater) list(ctx context.Context) ([]Release, error) {
	data, err := u.get(ctx, fmt.Sprintf("%s/repos/%s/releases?per_page=30", strings.TrimRight(u.APIBase, "/"), u.Repo), maxAPIBytes, true)
	if err != nil {
		return nil, err
	}
	var raw []ghRelease
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%w: unreadable release list", domain.ErrUnavailable)
	}
	var out []Release
	for _, g := range raw {
		if g.Draft {
			continue
		}
		if _, ok := parseVersion(g.Tag); !ok {
			continue
		}
		out = append(out, g.release())
	}
	return out, nil
}

// Latest returns the newest release visible on channel. The stable channel
// ignores prereleases (also while the project is on 0.x); beta accepts them.
func (u *Updater) Latest(ctx context.Context, channel string) (Release, error) {
	rels, err := u.list(ctx)
	if err != nil {
		return Release{}, err
	}
	var best *Release
	for i := range rels {
		r := rels[i]
		if r.Prerelease && channel != ChannelBeta {
			continue
		}
		if best == nil || Compare(r.Tag, best.Tag) > 0 {
			best = &r
		}
	}
	if best == nil {
		msg := "no release found"
		if channel != ChannelBeta {
			msg = "no stable release found (set update.channel to beta to accept prereleases)"
		}
		return Release{}, domain.New(domain.ErrNotFound, msg)
	}
	return *best, nil
}

// Find returns the release with the given tag (with or without "v").
func (u *Updater) Find(ctx context.Context, version string) (Release, error) {
	rels, err := u.list(ctx)
	if err != nil {
		return Release{}, err
	}
	want := strings.TrimPrefix(version, "v")
	for _, r := range rels {
		if r.Version() == want {
			return r, nil
		}
	}
	return Release{}, domain.New(domain.ErrNotFound, "release "+version+" not found")
}

// Result is the outcome of a check.
type Result struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	Available bool   `json:"available"`
	URL       string `json:"url"`
}

// Check compares the running version with the latest release on channel.
func (u *Updater) Check(ctx context.Context, current, channel string) (Result, error) {
	rel, err := u.Latest(ctx, channel)
	if err != nil {
		return Result{}, err
	}
	return Result{Current: current, Latest: rel.Tag, Available: Compare(rel.Tag, current) > 0, URL: rel.URL}, nil
}

// assetFor picks the archive for this platform.
func (u *Updater) assetFor(rel Release) (Asset, error) {
	ext := ".tar.gz"
	if u.goos() == "windows" {
		ext = ".zip"
	}
	want := fmt.Sprintf("stkr_%s_%s_%s%s", rel.Version(), u.goos(), u.goarch(), ext)
	for _, a := range rel.Assets {
		if a.Name == want {
			return a, nil
		}
	}
	return Asset{}, domain.New(domain.ErrNotFound, fmt.Sprintf("release %s has no build for %s/%s", rel.Tag, u.goos(), u.goarch()))
}

func findChecksum(sums []byte, name string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", domain.New(domain.ErrInvalid, "checksums.txt has no entry for "+name)
}

// Apply downloads rel, verifies its checksum and replaces the executable at
// exePath. On any error the current executable is left untouched.
func (u *Updater) Apply(ctx context.Context, rel Release, exePath string) error {
	asset, err := u.assetFor(rel)
	if err != nil {
		return err
	}
	var sumAsset *Asset
	for i := range rel.Assets {
		if rel.Assets[i].Name == "checksums.txt" {
			sumAsset = &rel.Assets[i]
		}
	}
	if sumAsset == nil {
		return domain.New(domain.ErrInvalid, "release has no checksums.txt; refusing to install")
	}
	sums, err := u.get(ctx, sumAsset.URL, maxChecksumBytes, false)
	if err != nil {
		return err
	}
	want, err := findChecksum(sums, asset.Name)
	if err != nil {
		return err
	}
	archive, err := u.get(ctx, asset.URL, maxArchiveBytes, false)
	if err != nil {
		return err
	}
	got := sha256.Sum256(archive)
	if hex.EncodeToString(got[:]) != want {
		return domain.New(domain.ErrInvalid, "checksum mismatch: download is corrupt or tampered; update aborted, current version kept")
	}
	bin, err := extractBinary(archive, asset.Name)
	if err != nil {
		return err
	}
	return ReplaceExecutable(exePath, bin)
}

func isBinaryName(name string) bool {
	b := path.Base(strings.ReplaceAll(name, "\\", "/"))
	return b == "stkr" || b == "stkr.exe"
}

func extractBinary(archive []byte, name string) ([]byte, error) {
	missing := domain.New(domain.ErrInvalid, "archive does not contain the stkr executable")
	if strings.HasSuffix(name, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, domain.New(domain.ErrInvalid, "unreadable archive")
		}
		for _, f := range zr.File {
			if f.FileInfo().IsDir() || !isBinaryName(f.Name) {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, domain.New(domain.ErrInvalid, "unreadable archive")
			}
			defer rc.Close()
			return readBounded(rc)
		}
		return nil, missing
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, domain.New(domain.ErrInvalid, "unreadable archive")
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, missing
		}
		if err != nil {
			return nil, domain.New(domain.ErrInvalid, "unreadable archive")
		}
		if h.Typeflag == tar.TypeReg && isBinaryName(h.Name) {
			return readBounded(tr)
		}
	}
}

func readBounded(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBinaryBytes+1))
	if err != nil {
		return nil, domain.New(domain.ErrInvalid, "unreadable archive")
	}
	if int64(len(data)) > maxBinaryBytes {
		return nil, domain.New(domain.ErrInvalid, "executable in archive is too large")
	}
	if len(data) == 0 {
		return nil, domain.New(domain.ErrInvalid, "executable in archive is empty")
	}
	return data, nil
}

// rename is os.Rename; tests replace it to simulate failures.
var rename = os.Rename

// OldPath is where the previous executable is kept during a replacement.
func OldPath(exePath string) string { return exePath + ".old" }

// ReplaceExecutable writes data next to exePath, renames the current file to
// <exe>.old and moves the new file into place. A running Windows executable
// can be renamed but not overwritten, so the same steps are used on every OS.
// If the final rename fails the old file is restored.
func ReplaceExecutable(exePath string, data []byte) error {
	dir := filepath.Dir(exePath)
	mode := os.FileMode(0o755)
	if fi, err := os.Stat(exePath); err == nil {
		mode = fi.Mode().Perm() | 0o111
	}
	tmp, err := os.CreateTemp(dir, ".stkr-update-*")
	if err != nil {
		return fmt.Errorf("write new executable (is %s writable?): %w", dir, err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write new executable: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write new executable: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("write new executable: %w", err)
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		cleanup()
		return fmt.Errorf("write new executable: %w", err)
	}
	old := OldPath(exePath)
	_ = os.Remove(old)
	if err := rename(exePath, old); err != nil {
		cleanup()
		return fmt.Errorf("move current executable aside: %w", err)
	}
	if err := rename(tmpName, exePath); err != nil {
		rollback := rename(old, exePath)
		cleanup()
		if rollback != nil {
			return fmt.Errorf("install new executable failed (%v) and rollback failed: %w", err, rollback)
		}
		return fmt.Errorf("install new executable (previous version restored): %w", err)
	}
	if runtime.GOOS != "windows" {
		_ = os.Remove(old)
	}
	return nil
}

// CleanupOld removes the <exe>.old file left by a previous Windows update.
// It is best effort and silent.
func CleanupOld(exePath string) {
	if exePath == "" {
		return
	}
	_ = os.Remove(OldPath(exePath))
}
