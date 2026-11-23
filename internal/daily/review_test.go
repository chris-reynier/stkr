package daily

import (
	"strings"
	"testing"

	"github.com/chris-reynier/stkr/internal/domain"
)

const reviewOK = "Summary first.\n\n```stkr-review v1\ndate|type|context|text\n" +
	"2026-11-20|achievement|acme|Completed API authentication changes\n" +
	"2026-11-20|action|acme|Prepare deployment rollback plan\n" +
	"2026-11-20|risk|acme|Waiting for client security approval\n" +
	"2026-11-20|decision|meeting|Move the weekly review to Tuesday\n" +
	"2026-11-20|gap|administration|Confirm missing Friday meeting follow-up\n" +
	"```\n"

func TestParseReview(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		blocks   int
		records  int
		rejected int
		ignored  int
		warnSub  string
		valid    bool
	}{
		{name: "valid block", in: reviewOK, blocks: 1, records: 5, valid: true},
		{name: "no header, tilde fence, crlf", in: "~~~stkr-review v1\r\n2026-11-20 | note | other | a | b\r\n~~~", blocks: 1, records: 1, valid: true},
		{name: "unknown type is a warning, not a rejection",
			in:     "```stkr-review v1\n2026-11-20|win|acme|x\n2026-11-20|note|acme|y\n```",
			blocks: 1, records: 1, ignored: 1, warnSub: "unknown review record type", valid: true},
		{name: "bad date rejects the whole block",
			in:       "```stkr-review v1\n2026-11-20|note|acme|y\n20-11-2026|note|acme|z\n```",
			rejected: 1, ignored: 2, warnSub: "review block rejected"},
		{name: "too few fields rejects the block",
			in:       "```stkr-review v1\n2026-11-20|note|y\n```",
			rejected: 1, ignored: 1, warnSub: "date|type|context|text"},
		{name: "template placeholders import nothing",
			in:       "```stkr-review v1\ndate|type|context|text\n<YYYY-MM-DD>|<type>|<context id>|<text>\n```",
			rejected: 1, ignored: 1},
		{name: "unclosed block is rejected",
			in:       "```stkr-review v1\n2026-11-20|note|acme|y\n",
			rejected: 1, ignored: 1, warnSub: "not closed"},
		{name: "one bad block does not spoil a good one",
			in:     reviewOK + "```stkr-review v1\n2026-13-40|note|acme|y\n```",
			blocks: 1, records: 5, rejected: 1, ignored: 1, valid: true},
		{name: "unsupported version", in: "```stkr-review v2\n2026-11-20|note|acme|y\n```", warnSub: "unsupported block version"},
		{name: "empty block", in: "```stkr-review v1\ndate|type|context|text\n```", warnSub: "no records"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := Parse(tc.in)
			if len(p.Reviews) != tc.blocks || p.ReviewRecords() != tc.records || p.BlocksRejected != tc.rejected || p.Ignored != tc.ignored || p.Valid() != tc.valid {
				t.Fatalf("got blocks=%d records=%d rejected=%d ignored=%d valid=%v warnings=%v",
					len(p.Reviews), p.ReviewRecords(), p.BlocksRejected, p.Ignored, p.Valid(), p.Warnings)
			}
			if p.Brief || p.Close {
				t.Fatal("a review block must not mark brief or close")
			}
			if tc.warnSub != "" && !strings.Contains(strings.Join(p.Warnings, "\n"), tc.warnSub) {
				t.Fatalf("no warning with %q: %v", tc.warnSub, p.Warnings)
			}
			for _, w := range p.Warnings {
				if strings.Contains(w, "Waiting for client") || strings.Contains(w, "acme|") {
					t.Fatalf("warning repeats line text: %q", w)
				}
			}
		})
	}
}

func TestParseReviewFields(t *testing.T) {
	p := Parse("```stkr-review v1\n2026-11-20|Gap|meeting|Missing minutes | for the 10:00 call\n```\n```stkr-close v1\nitem | note | other | n |\n```")
	if len(p.Reviews) != 1 || !p.Close || len(p.Items) != 1 {
		t.Fatalf("mixed parse: %+v", p)
	}
	r := p.Reviews[0].Records[0]
	want := ReviewRecord{Line: 2, Date: "2026-11-20", Type: domain.ItemGap, Context: "meeting", Text: "Missing minutes | for the 10:00 call"}
	if r != want || p.Reviews[0].Start != 1 {
		t.Fatalf("record = %+v", r)
	}
}
