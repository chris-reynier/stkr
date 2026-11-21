package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
)

// ConfigFile returns the configuration as saved on disk, which may be newer
// than the one the running process uses. Without a config path (tests) it
// returns a copy of the running configuration.
func (s *Service) ConfigFile() (*config.Config, error) {
	if s.configPath == "" {
		cp := *s.cfg
		return &cp, nil
	}
	cfg, _, err := config.Load(s.configPath)
	return cfg, err
}

// UpdateConfig applies key/value pairs (dotted yaml keys, text values) to the
// saved configuration, validates it and writes it atomically. The running
// process keeps its current values until it is restarted. Secret values are
// never logged.
func (s *Service) UpdateConfig(_ context.Context, pairs map[string]string) error {
	if s.configPath == "" {
		return domain.New(domain.ErrUnavailable, "no config file is configured")
	}
	s.cfgMu.Lock()
	defer s.cfgMu.Unlock()
	cfg, _, err := config.Load(s.configPath)
	if err != nil {
		return err
	}
	if err := cfg.SetAll(pairs); err != nil {
		return err
	}
	if v, ok := pairs[AutostartKey]; ok {
		// Applied before saving: if Windows refuses, the file is not changed.
		on, err := strconv.ParseBool(v)
		if err != nil {
			return domain.New(domain.ErrInvalid, AutostartKey+" must be true or false")
		}
		if err := s.autostart.Set(on); err != nil {
			return err
		}
		s.log.Info("autostart changed", "on", on)
	}
	if err := config.Save(s.configPath, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	s.log.Info("config saved", "keys", len(pairs))
	return nil
}

// DesktopExe returns the path of the desktop program (stkrw.exe): the running
// program itself, or the one beside it. It is "" when there is none.
func DesktopExe() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if strings.EqualFold(filepath.Base(exe), "stkrw.exe") {
		return exe
	}
	p := filepath.Join(filepath.Dir(exe), "stkrw.exe")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return ""
}

// AutostartKey is the settings key of the sign-in entry.
const AutostartKey = "desktop.autostart"

// AutostartEnabled reports whether the sign-in entry exists. The entry (not
// the config file) is the truth: the installer writes it too.
func (s *Service) AutostartEnabled() (bool, error) {
	return s.autostart.Enabled()
}
