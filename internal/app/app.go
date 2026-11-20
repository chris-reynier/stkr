// Package app wires configuration, clock and logging into a service.Service.
package app

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/chris-reynier/stkr/internal/clock"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/service"
	"github.com/chris-reynier/stkr/internal/update"
)

// DBName is the database file name inside the data directory.
const DBName = "stkr.db"

// Options customise Open. Zero values use the real clock and slog.Default().
type Options struct {
	Clock  clock.Clock
	Logger *slog.Logger
	// Updater and Version replace the GitHub client and running version (tests).
	Updater *update.Updater
	Version string
}

// Open opens (and migrates) the database in home and returns a service and a
// function that closes it. home must already exist (config.EnsureHome).
func Open(ctx context.Context, home string, cfg *config.Config, opts Options) (*service.Service, func() error, error) {
	clk := opts.Clock
	if clk == nil {
		clk = clock.Real{}
	}
	lg := opts.Logger
	if lg == nil {
		lg = slog.Default()
	}
	svc, err := service.Open(ctx, filepath.Join(home, DBName), clk, cfg,
		service.Deps{BackupDir: filepath.Join(home, "backups"), ConfigPath: config.Path(home), PromptsDir: filepath.Join(home, "prompts"), AudioDir: filepath.Join(home, "audio"), Logger: lg, Updater: opts.Updater, Version: opts.Version})
	if err != nil {
		return nil, nil, err
	}
	return svc, svc.Close, nil
}
