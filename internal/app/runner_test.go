package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunnerRunsAndSurvivesErrorsAndPanics(t *testing.T) {
	var buf bytes.Buffer
	var mu sync.Mutex
	lg := slog.New(slog.NewTextHandler(writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return buf.Write(p)
	}), nil))
	r := NewRunner(lg)
	var good, bad, boom atomic.Int32
	r.Add("good", 5*time.Millisecond, func(context.Context) error { good.Add(1); return nil })
	r.Add("bad", 5*time.Millisecond, func(context.Context) error { bad.Add(1); return errors.New("nope") })
	r.Add("boom", 5*time.Millisecond, func(context.Context) error { boom.Add(1); panic("kaboom") })
	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)
	deadline := time.Now().Add(2 * time.Second)
	for (good.Load() < 3 || bad.Load() < 3 || boom.Load() < 3) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	r.Wait()
	if good.Load() < 3 || bad.Load() < 3 || boom.Load() < 3 {
		t.Fatalf("jobs did not keep running: %d %d %d", good.Load(), bad.Load(), boom.Load())
	}
	n := good.Load()
	time.Sleep(30 * time.Millisecond)
	if good.Load() != n {
		t.Fatal("job ran after cancel")
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(buf.String(), "worker failed") || !strings.Contains(buf.String(), "worker panic") {
		t.Fatalf("missing log lines: %s", buf.String())
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
