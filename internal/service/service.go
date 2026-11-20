// Package service is the single API used by every adapter (CLI, HTTP, web,
// MCP). It owns business rules and is the only package that uses the store.
package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/chris-reynier/stkr/internal/ai"
	"github.com/chris-reynier/stkr/internal/capture"
	"github.com/chris-reynier/stkr/internal/clock"
	"github.com/chris-reynier/stkr/internal/config"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/logx"
	"github.com/chris-reynier/stkr/internal/platform"
	"github.com/chris-reynier/stkr/internal/store"
	"github.com/chris-reynier/stkr/internal/timeline"
)

// SwitchDebounce is the window in which a repeated switch to the active
// context is a no-op (double clicks on a Stream Deck key).
const SwitchDebounce = 2 * time.Second

// MaxNoteLen is the maximum length of an event note, in characters.
const MaxNoteLen = 1000

// Deps holds optional collaborators.
type Deps struct {
	// BackupDir is where backups are written. Required for Backup.
	BackupDir string
	// ConfigPath is the config file the settings page edits. Empty disables saving.
	ConfigPath string
	// Logger defaults to a discarding logger.
	Logger *slog.Logger
	// Windows focuses windows and Launcher opens workspaces. Nil values use
	// the platform implementations (Launcher from the launch.* config keys).
	Windows  platform.Windows
	Launcher platform.Launcher
	// Clipboard is the system clipboard (nil uses platform.NewClipboard).
	Clipboard platform.Clipboard
	// PromptsDir holds user overrides of the prompt templates. Empty means defaults only.
	PromptsDir string
	// AudioDir is where recordings and converted phone audio are kept. Empty disables voice capture.
	AudioDir string
	// Recorder and Transcriber replace the ffmpeg and whisper defaults (tests).
	Recorder    capture.Recorder
	Transcriber capture.Transcriber
	// TitleTimeout is the bookmark title fetch budget (default 2 s).
	TitleTimeout time.Duration
	// AI replaces the provider chosen by the config (tests).
	AI ai.Provider
}

// Service is the application facade.
type Service struct {
	st         *store.Store
	clk        clock.Clock
	cfg        *config.Config
	loc        *time.Location
	tl         timeline.Config
	backupDir  string
	configPath string
	cfgMu      sync.Mutex
	log        *slog.Logger
	windows    platform.Windows
	launcher   platform.Launcher
	clipboard  platform.Clipboard
	promptsDir string
	capt       *captureState
	ai         *aiState
}

// New builds a Service. cfg must be valid.
func New(st *store.Store, clk clock.Clock, cfg *config.Config, deps Deps) (*Service, error) {
	loc, err := cfg.Location()
	if err != nil {
		return nil, err
	}
	ds, err := timeline.ParseTimeOfDay(cfg.DayStart)
	if err != nil {
		return nil, fmt.Errorf("day_start: %w", err)
	}
	de, err := timeline.ParseTimeOfDay(cfg.DayEnd)
	if err != nil {
		return nil, fmt.Errorf("day_end: %w", err)
	}
	lg := deps.Logger
	if lg == nil {
		lg = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	win, lau := deps.Windows, deps.Launcher
	if win == nil {
		win = platform.NewWindows()
	}
	if lau == nil {
		lau = platform.NewLauncher(cfg.Launch.Command, cfg.Launch.Args)
	}
	clip := deps.Clipboard
	if clip == nil {
		clip = platform.NewClipboard()
	}
	s := &Service{
		windows:    win,
		launcher:   lau,
		clipboard:  clip,
		promptsDir: deps.PromptsDir,
		capt:       newCaptureState(cfg, deps),
		st:         st,
		clk:        clk,
		cfg:        cfg,
		loc:        loc,
		tl:         timeline.Config{Loc: loc, DayStart: ds, DayEnd: de, IdleGap: time.Duration(cfg.IdleGapMinutes) * time.Minute},
		backupDir:  deps.BackupDir,
		configPath: deps.ConfigPath,
		log:        lg,
	}
	s.ai = newAIState(s, cfg, deps)
	return s, nil
}

// Location returns the time zone used for days and display.
func (s *Service) Location() *time.Location { return s.loc }

// Config returns the configuration the service was built with.
func (s *Service) Config() *config.Config { return s.cfg }

func invalid(format string, a ...any) error {
	return domain.New(domain.ErrInvalid, fmt.Sprintf(format, a...))
}

func checkNote(note string) error {
	if utf8.RuneCountInString(note) > MaxNoteLen {
		return invalid("note is longer than %d characters", MaxNoteLen)
	}
	return nil
}

// Switch makes ctxID the current context by recording an event. A repeated
// switch to the context that is already active within SwitchDebounce returns
// the existing event with Created=false. An unknown context returns
// ErrNotFound; contexts are never created implicitly.
func (s *Service) Switch(ctx context.Context, ctxID string, src domain.EventSource, note string) (domain.SwitchResult, error) {
	if err := domain.CheckContextID(ctxID); err != nil {
		return domain.SwitchResult{}, err
	}
	return s.record(ctx, ctxID, src, note, false)
}

// Stop switches to the off context. When nothing is being tracked it does
// nothing and returns Created=false.
func (s *Service) Stop(ctx context.Context, src domain.EventSource) (domain.SwitchResult, error) {
	return s.record(ctx, domain.ContextOff, src, "", true)
}

func (s *Service) record(ctx context.Context, ctxID string, src domain.EventSource, note string, stop bool) (domain.SwitchResult, error) {
	var res domain.SwitchResult
	if !src.Valid() {
		return res, invalid("invalid event source %q", src)
	}
	if err := checkNote(note); err != nil {
		return res, err
	}
	now := s.clk.Now()
	err := s.st.InTx(ctx, func(tx *store.Store) error {
		c, err := tx.GetContext(ctx, ctxID)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.New(domain.ErrNotFound, fmt.Sprintf(
				"unknown context %q: run `stkr ctx list` to see contexts or `stkr ctx add %s` to create it", ctxID, ctxID))
		}
		if err != nil {
			return err
		}
		if !c.Active {
			return domain.New(domain.ErrConflict, fmt.Sprintf("context %q is parked: run `stkr ctx activate %s` first", ctxID, ctxID))
		}
		last, err := tx.LastEvent(ctx, now)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			if stop {
				return nil
			}
		case err != nil:
			return err
		case stop && last.ContextID == domain.ContextOff:
			res.Event = last
			return nil
		case last.ContextID == ctxID && now.Sub(last.TS) < SwitchDebounce:
			res.Event = last
			return nil
		}
		e, err := tx.InsertEvent(ctx, domain.Event{TS: now, ContextID: ctxID, Source: src, Note: note})
		res = domain.SwitchResult{Event: e, Created: true}
		return err
	})
	if err != nil {
		return domain.SwitchResult{}, err
	}
	res.Event.TS = res.Event.TS.In(s.loc)
	s.log.Info("switch", "context", ctxID, "event", res.Event.ID, "created", res.Created, "source", string(src), "note", logx.Redact(note))
	return res, nil
}

// Now returns the current context (the last one switched to, unless that was
// off) and today's counted time.
func (s *Service) Now(ctx context.Context) (domain.NowStatus, error) {
	now := s.clk.Now()
	day, err := s.Day(ctx, "")
	if err != nil {
		return domain.NowStatus{}, err
	}
	st := domain.NowStatus{Today: day.Counted}
	last, err := s.st.LastEvent(ctx, now)
	if errors.Is(err, domain.ErrNotFound) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	if last.ContextID == domain.ContextOff {
		return st, nil
	}
	c, err := s.st.GetContext(ctx, last.ContextID)
	if err != nil {
		return st, err
	}
	elapsed := now.Sub(last.TS)
	st.Tracking = true
	st.Context = &c
	st.EventID = last.ID
	st.Since = last.TS.In(s.loc)
	st.Elapsed = domain.Duration(elapsed)
	st.IdleSuspect = elapsed > s.tl.IdleGap && !last.Confirmed
	st.Note = last.Note
	return st, nil
}

// Open opens and migrates the database at dbPath and returns a service that
// owns it; call Close when done. Before a pending migration is applied to an
// existing database, a copy is written to deps.BackupDir.
func Open(ctx context.Context, dbPath string, clk clock.Clock, cfg *config.Config, deps Deps) (*Service, error) {
	st, err := store.Open(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	applied, err := st.Migrate(ctx, deps.BackupDir, clk.Now())
	if err != nil {
		_ = st.Close()
		return nil, fmt.Errorf("migrate database: %w", err)
	}
	s, err := New(st, clk, cfg, deps)
	if err != nil {
		_ = st.Close()
		return nil, err
	}
	if len(applied) > 0 {
		s.log.Info("database migrated", "versions", applied)
	}
	return s, nil
}

// Close closes the database.
func (s *Service) Close() error {
	s.capt.stop()
	return s.st.Close()
}
