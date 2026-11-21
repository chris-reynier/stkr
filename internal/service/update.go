package service

import (
	"context"
	"encoding/json"
	"os"
	"time"

	"github.com/chris-reynier/stkr/internal/buildinfo"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/store"
	upd "github.com/chris-reynier/stkr/internal/update"
)

const (
	kvUpdate = "update.latest"
	// UpdateCheckTimeout caps the background check (D-012).
	UpdateCheckTimeout = 10 * time.Second
)

// UpdateInfo is the cached result of the last update check.
type UpdateInfo struct {
	Current   string    `json:"current"`
	Latest    string    `json:"latest"`
	URL       string    `json:"url"`
	Checked   time.Time `json:"checked"`
	Available bool      `json:"available"`
}

type updateCache struct {
	Latest  string    `json:"latest"`
	URL     string    `json:"url"`
	Checked time.Time `json:"checked"`
}

func (s *Service) currentVersion() string {
	if s.version != "" {
		return s.version
	}
	return buildinfo.Version
}

// Updater returns the release client (the default one unless a test set it).
func (s *Service) Updater() *upd.Updater {
	if s.updater != nil {
		return s.updater
	}
	return upd.New()
}

// UpdateCheck asks GitHub for the latest release on the configured channel
// and caches the answer. It runs even when update.check is off (it is an
// explicit request).
func (s *Service) UpdateCheck(ctx context.Context) (upd.Result, error) {
	res, err := s.Updater().Check(ctx, s.currentVersion(), s.cfg.Update.Channel)
	if err != nil {
		return upd.Result{}, err
	}
	b, _ := json.Marshal(updateCache{Latest: res.Latest, URL: res.URL, Checked: s.clk.Now().UTC()})
	if err := s.st.SetKV(ctx, kvUpdate, string(b)); err != nil {
		s.log.Warn("cannot store update info", "err", err)
	}
	return res, nil
}

// UpdateTick is the background job: it does nothing when update.check is
// false, never blocks longer than UpdateCheckTimeout and stays silent on
// failure (a debug log only).
func (s *Service) UpdateTick(ctx context.Context) error {
	if !s.cfg.Update.Check {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, UpdateCheckTimeout)
	defer cancel()
	res, err := s.UpdateCheck(ctx)
	if err != nil {
		s.log.Debug("update check failed", "err", err)
		return nil
	}
	if res.Available {
		s.log.Info("update available", "latest", res.Latest)
	}
	return nil
}

// UpdateInfo returns the cached check result. ok is false when nothing is
// cached. Available is computed against the running version, so the banner
// disappears after an update without waiting for the next check.
func (s *Service) UpdateInfo(ctx context.Context) (UpdateInfo, bool) {
	v, err := s.st.GetKV(ctx, kvUpdate)
	if err != nil {
		return UpdateInfo{}, false
	}
	return decodeUpdate(v, s.currentVersion())
}

// UpdatePending returns the cached update when it is newer than the running
// version. ok is false when nothing is cached, nothing newer is known or this
// is a development build. It does not depend on update.check: a check the user
// started with "Check now" counts.
func (s *Service) UpdatePending(ctx context.Context) (UpdateInfo, bool) {
	if upd.IsDev(s.currentVersion()) {
		return UpdateInfo{}, false
	}
	info, ok := s.UpdateInfo(ctx)
	if !ok || !info.Available {
		return UpdateInfo{}, false
	}
	return info, true
}

// UpdateBanner returns the banner text, or "" when no update is known,
// update.check is off, or this is a development build.
func (s *Service) UpdateBanner(ctx context.Context) string {
	if !s.cfg.Update.Check || upd.IsDev(s.currentVersion()) {
		return ""
	}
	info, ok := s.UpdateInfo(ctx)
	if !ok || !info.Available {
		return ""
	}
	return "STKR " + info.Latest + " is available"
}

// CachedUpdateInfo reads the cached result straight from the database file
// without creating or migrating anything. ok is false if the file or value is
// missing. Used by `stkr version`, which makes no network call.
func CachedUpdateInfo(ctx context.Context, dbPath, current string) (UpdateInfo, bool) {
	if _, err := os.Stat(dbPath); err != nil {
		return UpdateInfo{}, false
	}
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		return UpdateInfo{}, false
	}
	defer st.Close()
	v, err := st.GetKV(ctx, kvUpdate)
	if err != nil {
		return UpdateInfo{}, false
	}
	return decodeUpdate(v, current)
}

func decodeUpdate(v, current string) (UpdateInfo, bool) {
	var c updateCache
	if json.Unmarshal([]byte(v), &c) != nil || c.Latest == "" {
		return UpdateInfo{}, false
	}
	return UpdateInfo{Current: current, Latest: c.Latest, URL: c.URL, Checked: c.Checked,
		Available: upd.Compare(c.Latest, current) > 0}, true
}

// CurrentVersion returns the running version.
func (s *Service) CurrentVersion() string { return s.currentVersion() }

// UpdateStage downloads the latest release of the configured channel,
// verifies it and prepares the installation next to exe, without changing any
// installed program (see update.Updater.Fetch). The caller commits it. It
// fails with ErrInvalid for development builds and with ErrConflict when the
// running version is already the latest.
func (s *Service) UpdateStage(ctx context.Context, exe string, opt upd.FetchOptions) (*upd.Staged, error) {
	cur := s.currentVersion()
	if upd.IsDev(cur) {
		return nil, domain.New(domain.ErrInvalid, "this is a development build; it is not updated from releases")
	}
	u := s.Updater()
	rel, err := u.Latest(ctx, s.cfg.Update.Channel)
	if err != nil {
		return nil, err
	}
	if upd.Compare(rel.Tag, cur) <= 0 {
		return nil, domain.New(domain.ErrConflict, "STKR "+cur+" is up to date")
	}
	return u.Fetch(ctx, rel, exe, opt)
}
