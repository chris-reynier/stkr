//go:build windows

package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/chris-reynier/stkr/internal/domain"
)

func TestFocusNoMatchingWindow(t *testing.T) {
	err := NewWindows().Focus(context.Background(), "stkr-test-no-such-window-7f3a9c", 0)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if err := NewWindows().Focus(context.Background(), " ", 0); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("blank title: %v", err)
	}
}
