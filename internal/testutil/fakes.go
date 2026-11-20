package testutil

import (
	"context"
	"sync"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
)

// FakeClipboard is an in-memory platform.Clipboard.
type FakeClipboard struct {
	mu   sync.Mutex
	text string
	// Err is returned by both methods when set.
	Err error
}

// ReadText returns the stored text.
func (c *FakeClipboard) ReadText(context.Context) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.text, c.Err
}

// WriteText stores text.
func (c *FakeClipboard) WriteText(_ context.Context, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.Err != nil {
		return c.Err
	}
	c.text = text
	return nil
}

// Text returns the stored text.
func (c *FakeClipboard) Text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.text
}

// Set stores text without going through the interface.
func (c *FakeClipboard) Set(text string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.text = text
}

// PasteCall records one PasteInto call.
type PasteCall struct {
	Title string
	Enter bool
}

// FakeWindows is a platform.Windows with a fixed set of window titles.
type FakeWindows struct {
	mu sync.Mutex
	// Present holds the titles that exist (exact match on the needle).
	Present map[string]bool
	// Err is returned instead of the not-found/ok result when set.
	Err     error
	Focused []string
	Pastes  []PasteCall
}

// Focus records the call and reports whether the window exists.
func (w *FakeWindows) Focus(_ context.Context, title string, _ time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Focused = append(w.Focused, title)
	return w.result(title)
}

// PasteInto records the call and reports whether the window exists.
func (w *FakeWindows) PasteInto(_ context.Context, title string, enter bool, _ time.Duration) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.Pastes = append(w.Pastes, PasteCall{Title: title, Enter: enter})
	return w.result(title)
}

func (w *FakeWindows) result(title string) error {
	if w.Err != nil {
		return w.Err
	}
	if w.Present[title] {
		return nil
	}
	return domain.New(domain.ErrNotFound, "no window")
}
