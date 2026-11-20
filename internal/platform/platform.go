// Package platform holds the operating system integrations: window focus and
// opening a workspace. Windows-only code sits behind build tags; other systems
// get versions that return domain.ErrUnavailable or work best-effort.
package platform

import (
	"context"
	"time"
)

// Windows brings desktop windows to the front.
type Windows interface {
	// Focus brings the first visible top-level window whose title contains
	// titleContains (case-insensitive) to the foreground. It polls for up to
	// wait when no such window exists yet. It returns domain.ErrNotFound when
	// there is no matching window and domain.ErrUnavailable when the system
	// cannot do this.
	Focus(ctx context.Context, titleContains string, wait time.Duration) error
}

// Launcher opens things in other programs.
type Launcher interface {
	// OpenWorkspace opens the folder path in the configured editor.
	OpenWorkspace(ctx context.Context, path string) error
}
