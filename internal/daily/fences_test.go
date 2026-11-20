package daily

import (
	"strings"
	"testing"

	"github.com/chris-reynier/stkr/internal/domain"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		format    Format
		brief     bool
		closeFlag bool
		tops      int
		items     int
		done      int
		ignored   int
		warnSub   string // a substring one warning must contain ("" = no check)
		valid     bool
	}{
		{
			name:   "brief block",
			in:     "Here you go\n```stkr-brief v1\ntop | 1 | Ship P05\ntop | 2 | Review PR\nitem | action | other | Write docs | 2026-11-20\n```\nbye",
			format: FormatFenced, brief: true, tops: 2, items: 1, valid: true,
		},
		{
			name:   "close block with done",
			in:     "```stkr-close v1\nitem | followup | alpha | Call Bob | \ndone | 0123456789abcdef\n```",
			format: FormatFenced, closeFlag: true, items: 1, done: 1, valid: true,
		},
		{
			name:   "tilde fence and indentation and case",
			in:     "   ~~~ STKR-Brief V1\n  top | 3 | third\n  ~~~",
			format: FormatFenced, brief: true, tops: 1, valid: true,
		},
		{
			name:   "crlf and bom",
			in:     string(rune(0xFEFF)) + "```stkr-close v1\r\nitem | note | other | line one\r\n```\r\n",
			format: FormatFenced, closeFlag: true, items: 1, valid: true,
		},
		{
			name:   "both blocks",
			in:     "```stkr-brief v1\ntop | 1 | a\n```\n```stkr-close v1\ndone | x y z\n```",
			format: FormatFenced, brief: true, closeFlag: true, tops: 1, done: 1, valid: true,
		},
		{
			name:   "missing version is v1 with a warning",
			in:     "```stkr-brief\ntop | 1 | a\n```",
			format: FormatFenced, brief: true, tops: 1, warnSub: "no version", valid: true,
		},
		{
			name:   "unsupported version is ignored",
			in:     "```stkr-brief v2\ntop | 1 | a\n```",
			format: FormatFenced, warnSub: "unsupported block version",
		},
		{
			name:   "unterminated block still read",
			in:     "```stkr-close v1\nitem | risk | other | slow tests",
			format: FormatFenced, closeFlag: true, items: 1, warnSub: "not closed", valid: true,
		},
		{
			name:   "pipe fallback with warning",
			in:     "Sure!\n- top | 1 | Do the thing\nitem | action | other | Next step | \nthanks",
			format: FormatPipe, brief: true, tops: 1, items: 1, warnSub: "no stkr block", valid: true,
		},
		{
			name:   "pipe fallback done infers close",
			in:     "done | write docs\nitem | action | other | more",
			format: FormatPipe, closeFlag: true, items: 1, done: 1, warnSub: "unfenced", valid: true,
		},
		{
			name:   "pipe lines outside a block are ignored when a block exists",
			in:     "top | 1 | stray\n```stkr-brief v1\ntop | 2 | real\n```",
			format: FormatFenced, brief: true, tops: 1, valid: true,
		},
		{
			name:   "other fences are skipped",
			in:     "```text\ntop | 1 | inside text fence\n```\n```stkr-close v1\ndone | a\n```",
			format: FormatFenced, closeFlag: true, done: 1, valid: true,
		},
		{
			name:   "garbage",
			in:     "Hello, nothing to see | here\n```go\nfmt.Println()\n```\n",
			format: FormatPipe,
		},
		{name: "empty", in: "", format: FormatPipe},
		{
			name:   "bad lines are counted",
			in:     "```stkr-brief v1\ntop | 9 | x\ntop | 1\nitem | bogus | other | s |\nitem | action | other | |\nwhat is this\nitem | action | other | ok\n```",
			format: FormatFenced, brief: true, items: 1, ignored: 5, valid: true,
		},
		{
			name:   "top allowed in close, done ignored in brief",
			in:     "```stkr-close v1\ntop | 1 | x\nitem | note | other | n\n```\n```stkr-brief v1\ndone | n\ntop | 2 | y\n```",
			format: FormatFenced, brief: true, closeFlag: true, tops: 2, items: 1, ignored: 1, warnSub: "only used in stkr-close", valid: true,
		},
		{
			name:   "comments and blanks",
			in:     "```stkr-brief v1\n# comment\n\ntop | 1 | a\n```",
			format: FormatFenced, brief: true, tops: 1, valid: true,
		},
		{
			name:   "template placeholders import nothing",
			in:     "```stkr-brief v1\ntop | 1 | <priority text>\nitem | <action|decision|note|followup|risk> | <project id> | <summary> | <due YYYY-MM-DD or empty>\n```",
			format: FormatFenced, ignored: 2,
		},
		{
			name:   "empty brief block does not mark brief",
			in:     "```stkr-brief v1\n```\n```stkr-close v1\ndone | a\n```",
			format: FormatFenced, closeFlag: true, done: 1, valid: true,
		},
		{
			name:   "block with nothing does not mark saved",
			in:     "```stkr-close v1\n```",
			format: FormatFenced,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := Parse(tc.in)
			if p.Format != tc.format || p.Brief != tc.brief || p.Close != tc.closeFlag {
				t.Errorf("format/brief/close = %s/%v/%v, want %s/%v/%v", p.Format, p.Brief, p.Close, tc.format, tc.brief, tc.closeFlag)
			}
			if len(p.Tops) != tc.tops || len(p.Items) != tc.items || len(p.Done) != tc.done {
				t.Errorf("tops/items/done = %d/%d/%d, want %d/%d/%d", len(p.Tops), len(p.Items), len(p.Done), tc.tops, tc.items, tc.done)
			}
			if p.Ignored != tc.ignored {
				t.Errorf("ignored = %d, want %d (%v)", p.Ignored, tc.ignored, p.Warnings)
			}
			if p.Valid() != tc.valid {
				t.Errorf("valid = %v, want %v", p.Valid(), tc.valid)
			}
			if tc.warnSub != "" && !strings.Contains(strings.Join(p.Warnings, "\n"), tc.warnSub) {
				t.Errorf("warnings %v lack %q", p.Warnings, tc.warnSub)
			}
		})
	}
}

func TestParseRecordFields(t *testing.T) {
	p := Parse("```stkr-close v1\nitem | Action | Alpha | Fix a | b thing | 2026-12-01\nitem | note | other | has | pipe\nitem | risk | other | Late | 2026-02-30\n```")
	if len(p.Items) != 3 {
		t.Fatalf("items = %+v", p.Items)
	}
	a := p.Items[0]
	if a.Type != domain.ItemAction || a.Project != "Alpha" || a.Summary != "Fix a | b thing" || a.Due != "2026-12-01" {
		t.Errorf("item 0 = %+v", a)
	}
	if b := p.Items[1]; b.Summary != "has | pipe" || b.Due != "" {
		t.Errorf("item 1 = %+v", b)
	}
	if c := p.Items[2]; c.Summary != "Late | 2026-02-30" || c.Due != "" {
		t.Errorf("item 2 (invalid due stays in the summary) = %+v", c)
	}
}

func TestParseLimits(t *testing.T) {
	long := strings.Repeat("é", MaxSummaryRunes+50)
	p := Parse("```stkr-close v1\nitem | note | other | " + long + "\n```")
	if len(p.Items) != 1 || len([]rune(p.Items[0].Summary)) != MaxSummaryRunes {
		t.Fatalf("summary not truncated to %d runes: %d", MaxSummaryRunes, len([]rune(p.Items[0].Summary)))
	}
	var b strings.Builder
	b.WriteString("```stkr-brief v1\n")
	for i := 0; i < 100; i++ {
		b.WriteString("garbage\n")
	}
	b.WriteString("```")
	p = Parse(b.String())
	if p.Ignored != 100 || len(p.Warnings) != maxWarnings+1 {
		t.Errorf("ignored %d, warnings %d", p.Ignored, len(p.Warnings))
	}
	for _, w := range p.Warnings {
		if strings.Contains(w, "garbage") {
			t.Fatalf("warning echoes input: %q", w)
		}
	}
}

func TestFingerprint(t *testing.T) {
	a := Fingerprint(domain.ItemAction, "alpha", "Write the docs.")
	b := Fingerprint(domain.ItemAction, " Alpha ", "  write   THE docs ")
	if a != b || len(a) != 16 {
		t.Fatalf("fingerprints differ or wrong length: %q %q", a, b)
	}
	if Fingerprint(domain.ItemNote, "alpha", "Write the docs") == a {
		t.Error("type must be part of the fingerprint")
	}
	if Fingerprint(domain.ItemAction, "beta", "Write the docs") == a {
		t.Error("project must be part of the fingerprint")
	}
}

func TestFingerprintContractExample(t *testing.T) {
	// Documented in docs/contracts/import-fences.md.
	if got := Fingerprint(domain.ItemAction, "other", "Write docs"); got != "01929f695f28d542" {
		t.Errorf("fingerprint = %s", got)
	}
}
