package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/atotto/clipboard"

	"github.com/chris-reynier/stkr/internal/domain"
)

func TestWithTimeoutMapsErrors(t *testing.T) {
	if clipboard.Unsupported {
		t.Skip("no clipboard tool on this system")
	}
	if err := withTimeout(context.Background(), func() error { return nil }); err != nil {
		t.Fatalf("ok call: %v", err)
	}
	err := withTimeout(context.Background(), func() error { return errors.New("boom") })
	if !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("err = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	block := make(chan struct{})
	defer close(block)
	err = withTimeout(ctx, func() error { <-block; return nil })
	if !errors.Is(err, domain.ErrUnavailable) {
		t.Fatalf("cancelled: %v", err)
	}
}
