package platform

import (
	"context"
	"fmt"
	"time"

	"github.com/atotto/clipboard"

	"github.com/chris-reynier/stkr/internal/domain"
)

// clipboardTimeout bounds a clipboard call; the system clipboard can hang
// when another program holds it.
const clipboardTimeout = 5 * time.Second

// Clipboard reads and writes the system text clipboard.
type Clipboard interface {
	// ReadText returns the clipboard text.
	ReadText(ctx context.Context) (string, error)
	// WriteText replaces the clipboard text.
	WriteText(ctx context.Context, text string) error
}

type sysClipboard struct{}

// NewClipboard returns the system clipboard. When the system has none (for
// example Linux without xclip, xsel or wl-clipboard) its methods return
// domain.ErrUnavailable.
func NewClipboard() Clipboard { return sysClipboard{} }

func (sysClipboard) ReadText(ctx context.Context) (string, error) {
	var out string
	err := withTimeout(ctx, func() error {
		var err error
		out, err = clipboard.ReadAll()
		return err
	})
	return out, err
}

func (sysClipboard) WriteText(ctx context.Context, text string) error {
	return withTimeout(ctx, func() error { return clipboard.WriteAll(text) })
}

func withTimeout(ctx context.Context, fn func() error) error {
	if clipboard.Unsupported {
		return domain.New(domain.ErrUnavailable, "no clipboard tool found (install xclip, xsel or wl-clipboard)")
	}
	done := make(chan error, 1)
	go func() { done <- fn() }()
	ctx, cancel := context.WithTimeout(ctx, clipboardTimeout)
	defer cancel()
	select {
	case err := <-done:
		if err != nil {
			return domain.New(domain.ErrUnavailable, fmt.Sprintf("clipboard: %v", err))
		}
		return nil
	case <-ctx.Done():
		return domain.New(domain.ErrUnavailable, "clipboard did not answer in time")
	}
}
