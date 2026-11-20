//go:build windows

package platform

import (
	"context"
	"errors"
	"testing"
	"time"
	"unsafe"

	"github.com/chris-reynier/stkr/internal/domain"
)

// SendInput rejects calls whose INPUT size differs from the system's.
func TestInputStructSize(t *testing.T) {
	want := uintptr(40)
	if unsafe.Sizeof(uintptr(0)) == 4 {
		want = 28
	}
	if got := unsafe.Sizeof(input{}); got != want {
		t.Fatalf("sizeof(input) = %d, want %d", got, want)
	}
}

func TestPasteIntoMissingWindow(t *testing.T) {
	err := NewWindows().PasteInto(context.Background(), "no such window title for stkr tests 4f1e", false, 0)
	if !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if err := NewWindows().PasteInto(context.Background(), " ", false, time.Millisecond); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("empty title: %v", err)
	}
}
