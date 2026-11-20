//go:build !windows

package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/chris-reynier/stkr/internal/domain"
)

func TestFocusUnavailable(t *testing.T) {
	if err := NewWindows().Focus(context.Background(), "x", 0); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}
