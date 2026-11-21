// Package update checks GitHub Releases for a newer STKR build and replaces
// the running executable after verifying the archive against checksums.txt.
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
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
	// Start runs the setup of a setup-installed copy (default: StartDetached).
	Start Starter
	// TempDir is where a downloaded setup is kept (default os.TempDir()).
	TempDir string
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

// Apply downloads rel, verifies its checksum and installs it next to exePath
// (see Fetch for how the install method is chosen). On any error the current
// programs are left untouched.
func (u *Updater) Apply(ctx context.Context, rel Release, exePath string) error {
	st, err := u.Fetch(ctx, rel, exePath, FetchOptions{})
	if err != nil {
		return err
	}
	if err := st.Commit(ctx); err != nil {
		st.Discard()
		return err
	}
	return nil
}

// programKind maps a program file name to "stkr" or "stkrw" ("" for any
// other file).
func programKind(name string) string {
	switch strings.ToLower(path.Base(strings.ReplaceAll(name, "\\", "/"))) {
	case "stkr", "stkr.exe":
		return "stkr"
	case "stkrw.exe":
		return "stkrw"
	}
	return ""
}

// program is one STKR executable found in an archive.
type program struct {
	Name string // base name inside the archive, e.g. "stkrw.exe"
	Kind string // "stkr" or "stkrw"
	Data []byte
}

// extractPrograms returns every STKR executable of the archive (stkr and, on
// Windows, stkrw), in archive order. It fails when there is none.
func extractPrograms(archive []byte, name string) ([]program, error) {
	unreadable := domain.New(domain.ErrInvalid, "unreadable archive")
	var out []program
	seen := map[string]bool{}
	add := func(entry string, r io.Reader) error {
		kind := programKind(entry)
		if kind == "" || seen[kind] {
			return nil
		}
		data, err := readBounded(r)
		if err != nil {
			return err
		}
		seen[kind] = true
		out = append(out, program{Name: path.Base(strings.ReplaceAll(entry, "\\", "/")), Kind: kind, Data: data})
		return nil
	}
	if strings.HasSuffix(name, ".zip") {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, unreadable
		}
		for _, f := range zr.File {
			if f.FileInfo().IsDir() || programKind(f.Name) == "" {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, unreadable
			}
			err = add(f.Name, rc)
			_ = rc.Close()
			if err != nil {
				return nil, err
			}
		}
	} else {
		gz, err := gzip.NewReader(bytes.NewReader(archive))
		if err != nil {
			return nil, unreadable
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, unreadable
			}
			if h.Typeflag == tar.TypeReg && programKind(h.Name) != "" {
				if err := add(h.Name, tr); err != nil {
					return nil, err
				}
			}
		}
	}
	if len(out) == 0 {
		return nil, domain.New(domain.ErrInvalid, "archive does not contain the stkr executable")
	}
	return out, nil
}

// extractBinary returns the stkr (CLI) executable of the archive.
func extractBinary(archive []byte, name string) ([]byte, error) {
	progs, err := extractPrograms(archive, name)
	if err != nil {
		return nil, err
	}
	for _, p := range progs {
		if p.Kind == "stkr" {
			return p.Data, nil
		}
	}
	return nil, domain.New(domain.ErrInvalid, "archive does not contain the stkr executable")
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

// OldPath is where the previous executable is kept during a replacement.
func OldPath(exePath string) string { return exePath + ".old" }
