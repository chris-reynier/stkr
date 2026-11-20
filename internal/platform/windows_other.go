//go:build !windows

package platform

import (
	"context"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
)

type noWindows struct{}

// NewWindows returns the window focus implementation of this system, which
// reports domain.ErrUnavailable here.
func NewWindows() Windows { return noWindows{} }

func (noWindows) Focus(context.Context, string, time.Duration) error {
	return domain.New(domain.ErrUnavailable, "window focus is only supported on Windows")
}
