package domain

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorsIs(t *testing.T) {
	all := []*DomainError{ErrNotFound, ErrConflict, ErrInvalid, ErrUnavailable}
	for _, base := range all {
		custom := New(base, "custom text")
		wrapped := fmt.Errorf("layer: %w", custom)
		for _, other := range all {
			want := other == base
			if got := errors.Is(wrapped, other); got != want {
				t.Errorf("Is(%s wrapped, %s) = %v, want %v", base.Code, other.Code, got, want)
			}
		}
		if Code(wrapped) != base.Code {
			t.Errorf("Code = %q", Code(wrapped))
		}
	}
	if Code(errors.New("x")) != "" {
		t.Error("non-domain error should have empty code")
	}
}
