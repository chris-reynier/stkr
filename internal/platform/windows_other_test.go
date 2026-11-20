//go:build !windows

package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/chris-reynier/stkr/internal/domain"
)

func TestPasteUnavailable(t *testing.T) {
	if err := NewWindows().PasteInto(context.Background(), "x", false, 0); !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
}
