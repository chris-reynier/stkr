// Package logx sets up structured logging with a size-rotated log file.
package logx

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
)

const (
	// MaxFileSize is the size at which the log file rotates.
	MaxFileSize = 5 << 20
	// MaxFiles is the total number of log files kept (current plus rotated).
	MaxFiles = 5
)

// Redact returns a placeholder describing s without revealing it. Use it when
// logging user content (clipboard, prompts, ideas, transcriptions, secrets).
func Redact(s string) string { return fmt.Sprintf("len=%d", len(s)) }

// New creates a logger writing JSON lines to <logDir>/stkr.log. When debug is
// true it also writes text records to stderr. The returned Closer closes the
// log file.
func New(logDir string, debug bool) (*slog.Logger, io.Closer, error) {
	rf, err := OpenRotating(filepath.Join(logDir, "stkr.log"), MaxFileSize, MaxFiles)
	if err != nil {
		return nil, nil, err
	}
	level := slog.LevelInfo
	if debug {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler = slog.NewJSONHandler(rf, opts)
	if debug {
		h = multiHandler{h, slog.NewTextHandler(os.Stderr, opts)}
	}
	return slog.New(h), rf, nil
}

// Rotating is an io.WriteCloser that rotates a file by size.
type Rotating struct {
	mu       sync.Mutex
	path     string
	maxSize  int64
	maxFiles int
	f        *os.File
	size     int64
}

// OpenRotating opens (creating if needed) a rotating log file. Rotated files
// are named path.1 (newest) to path.<maxFiles-1> (oldest).
func OpenRotating(path string, maxSize int64, maxFiles int) (*Rotating, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	r := &Rotating{path: path, maxSize: maxSize, maxFiles: maxFiles}
	if err := r.open(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Rotating) open() error {
	f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("stat log file: %w", err)
	}
	r.f, r.size = f, fi.Size()
	return nil
}

// Write implements io.Writer.
func (r *Rotating) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.size > 0 && r.size+int64(len(p)) > r.maxSize {
		if err := r.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}

func (r *Rotating) rotate() error {
	if err := r.f.Close(); err != nil {
		return fmt.Errorf("close log file: %w", err)
	}
	_ = os.Remove(fmt.Sprintf("%s.%d", r.path, r.maxFiles-1))
	for i := r.maxFiles - 2; i >= 1; i-- {
		_ = os.Rename(fmt.Sprintf("%s.%d", r.path, i), fmt.Sprintf("%s.%d", r.path, i+1))
	}
	if r.maxFiles > 1 {
		if err := os.Rename(r.path, r.path+".1"); err != nil {
			return fmt.Errorf("rotate log file: %w", err)
		}
	} else {
		_ = os.Remove(r.path)
	}
	return r.open()
}

// Close closes the file.
func (r *Rotating) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.f.Close()
}
