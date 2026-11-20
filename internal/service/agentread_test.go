package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/chris-reynier/stkr/internal/domain"
)

func TestListItemsAndDigest(t *testing.T) {
	f := newDailyFixture(t)
	svc := f.svc
	ctx := context.Background()
	if _, err := svc.AddIdea(ctx, IdeaInput{Text: "ship it", Type: "action", Via: "mcp"}); err != nil {
		t.Fatal(err)
	}
	items, err := svc.ListItems(ctx, ItemFilter{Status: "open", Since: "today"})
	if err != nil || len(items) != 1 {
		t.Fatalf("items=%v err=%v", items, err)
	}
	if got, _ := svc.ListItems(ctx, ItemFilter{Project: "nope"}); len(got) != 0 {
		t.Fatalf("project filter: %v", got)
	}
	if _, err := svc.ListItems(ctx, ItemFilter{Status: "x"}); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("want invalid, got %v", err)
	}
	if _, err := svc.ListItems(ctx, ItemFilter{Since: "garbage"}); err == nil {
		t.Fatal("want error for bad since")
	}
	for _, k := range []string{DigestStart, DigestClose} {
		txt, err := svc.Digest(ctx, k)
		if err != nil || !strings.Contains(txt, "stkr-") {
			t.Fatalf("%s: %q %v", k, txt, err)
		}
	}
	if _, err := svc.Digest(ctx, "x"); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("want invalid, got %v", err)
	}
	// Digest must not have switched to planning.
	if n, _ := svc.Now(ctx); n.Tracking {
		t.Fatalf("digest changed tracking: %+v", n)
	}
}
