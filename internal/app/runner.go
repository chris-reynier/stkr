package app

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// Runner runs background jobs while serve is up. Jobs run once at start and
// then every interval. A failing or panicking job is logged and tried again
// at the next interval; it never stops the runner.
type Runner struct {
	log  *slog.Logger
	mu   sync.Mutex
	jobs []job
	wg   sync.WaitGroup
}

type job struct {
	name     string
	interval time.Duration
	fn       func(ctx context.Context) error
}

// NewRunner returns an empty Runner. A nil logger uses slog.Default().
func NewRunner(lg *slog.Logger) *Runner {
	if lg == nil {
		lg = slog.Default()
	}
	return &Runner{log: lg}
}

// Add registers a job. It must be called before Start.
func (r *Runner) Add(name string, interval time.Duration, fn func(ctx context.Context) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs = append(r.jobs, job{name, interval, fn})
}

// Start launches every registered job. Jobs stop when ctx is cancelled.
func (r *Runner) Start(ctx context.Context) {
	r.mu.Lock()
	jobs := append([]job(nil), r.jobs...)
	r.mu.Unlock()
	for _, j := range jobs {
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			r.loop(ctx, j)
		}()
	}
}

// Wait blocks until all jobs have returned (after ctx is cancelled).
func (r *Runner) Wait() { r.wg.Wait() }

func (r *Runner) loop(ctx context.Context, j job) {
	t := time.NewTicker(j.interval)
	defer t.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		r.runOnce(ctx, j)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (r *Runner) runOnce(ctx context.Context, j job) {
	defer func() {
		if v := recover(); v != nil {
			r.log.Error("worker panic", "worker", j.name, "panic", fmt.Sprint(v))
		}
	}()
	if err := j.fn(ctx); err != nil && ctx.Err() == nil {
		r.log.Error("worker failed", "worker", j.name, "err", err)
	}
}
