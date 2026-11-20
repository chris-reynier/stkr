package service

import (
	"context"
	"fmt"

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
	if err := config.Save(s.configPath, cfg); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	s.log.Info("config saved", "keys", len(pairs))
	return nil
}
