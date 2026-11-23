package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/chris-reynier/stkr/internal/daily"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/store"
)

// OriginReview is the item origin of stkr-review records.
const OriginReview = "review"

// maxLineList is how many line numbers a grouped warning names.
const maxLineList = 8

// rejectFutureReviews drops review blocks holding a record dated after
// today: a date in the future is a malformed block, not a plan.
func rejectFutureReviews(p *daily.Parsed, today string) []string {
	var warns []string
	kept := p.Reviews[:0]
	for _, b := range p.Reviews {
		bad := 0
		for _, r := range b.Records {
			if r.Date > today {
				bad = r.Line
				break
			}
		}
		if bad == 0 {
			kept = append(kept, b)
			continue
		}
		p.BlocksRejected++
		p.Ignored += len(b.Records)
		warns = append(warns, fmt.Sprintf("line %d: review block rejected, nothing from it was imported (line %d: the date is in the future)", b.Start, bad))
	}
	p.Reviews = kept
	return warns
}

// reviewNotes collects the line numbers of review records that need the
// user's attention, so the warnings stay short.
type reviewNotes struct {
	unknown, private []int
}

func lineList(lines []int) string {
	parts := make([]string, 0, maxLineList)
	for i, l := range lines {
		if i == maxLineList {
			parts = append(parts, "...")
			break
		}
		parts = append(parts, strconv.Itoa(l))
	}
	return strings.Join(parts, ", ")
}

func (n reviewNotes) warnings() []string {
	var out []string
	if len(n.unknown) > 0 {
		out = append(out, fmt.Sprintf("%d review record(s) name an unknown context and were filed in the inbox; check them (line %s)", len(n.unknown), lineList(n.unknown)))
	}
	if len(n.private) > 0 {
		out = append(out, fmt.Sprintf("%d review record(s) name a personal or system context: stored locally, never exported (line %s)", len(n.private), lineList(n.private)))
	}
	return out
}

// reviewItem turns a review record into an item. Achievements are finished
// work, so they are stored done.
func reviewItem(ctxs map[string]domain.Context, r daily.ReviewRecord, via string) domain.Item {
	project := resolveProject(ctxs, r.Context)
	it := domain.Item{
		Date: r.Date, Project: project, Type: r.Type, Summary: r.Text, Source: via,
		Origin: OriginReview, Fingerprint: daily.Fingerprint(r.Type, project, r.Text),
	}
	if r.Type == domain.ItemAchievement {
		it.Status = "done"
	}
	return it
}

// importReviews stores the records of the accepted review blocks inside the
// import transaction. Repeated records within the import are stored once.
func importReviews(ctx context.Context, tx *store.Store, ctxs map[string]domain.Context, blocks []daily.ReviewBlock,
	via string, res *ImportResult, touched map[string]bool, notes *reviewNotes) error {
	seen := map[string]bool{}
	for _, b := range blocks {
		for _, r := range b.Records {
			it := reviewItem(ctxs, r, via)
			if seen[it.Fingerprint] {
				res.Duplicates++
				continue
			}
			seen[it.Fingerprint] = true
			c, known := ctxs[it.Project]
			switch {
			case !known:
				notes.unknown = append(notes.unknown, r.Line)
			case !c.Kind.Exportable():
				notes.private = append(notes.private, r.Line)
				res.Private++
			}
			_, created, err := tx.InsertItem(ctx, it)
			if err != nil {
				return err
			}
			if created {
				res.ItemsCreated++
			} else {
				res.ItemsExisting++
			}
			touched[exportKey(ctxs, it.Project)] = true
		}
	}
	return nil
}

// PreviewRecord is one record an import would store.
type PreviewRecord struct {
	Line        int             `json:"line,omitempty"`
	Date        string          `json:"date"`
	Type        domain.ItemType `json:"type"`
	Context     string          `json:"context"`
	Text        string          `json:"text"`
	Fingerprint string          `json:"fingerprint"`
	// Status is new (would be created), existing (already stored) or
	// duplicate (repeated earlier in the same text).
	Status string `json:"status"`
	// KnownContext is false when the context names no STKR context.
	KnownContext bool `json:"known_context"`
	// Private is true for personal and system contexts (never exported).
	Private bool `json:"private"`
}

// ImportPreview is what Import would do with a text, without doing it.
type ImportPreview struct {
	Valid          bool            `json:"valid"`
	Kind           string          `json:"kind,omitempty"`
	Format         string          `json:"format"`
	Records        []PreviewRecord `json:"records"`
	Tops           int             `json:"tops"`
	Done           int             `json:"done"`
	BlocksRejected int             `json:"blocks_rejected"`
	Ignored        int             `json:"ignored"`
	Warnings       []string        `json:"warnings"`
}

// PreviewImport parses text like Import and reports the records it would
// store (review records and items), and which of them are new. It writes
// nothing, touches no key state and exports nothing. The text is never logged.
func (s *Service) PreviewImport(ctx context.Context, text string) (ImportPreview, error) {
	out := ImportPreview{Records: []PreviewRecord{}, Warnings: []string{}}
	if len(text) > maxImportBytes {
		return out, invalid("text is larger than %d KiB", maxImportBytes>>10)
	}
	today := s.todayDate()
	p := daily.Parse(text)
	futureWarn := rejectFutureReviews(&p, today)
	out.Valid, out.Kind, out.Format = p.Valid(), kindName(p), string(p.Format)
	out.Tops, out.Done, out.BlocksRejected, out.Ignored = len(p.Tops), len(p.Done), p.BlocksRejected, p.Ignored
	out.Warnings = append(append(out.Warnings, p.Warnings...), futureWarn...)
	ctxs, err := s.contextMap(ctx)
	if err != nil {
		return out, err
	}
	seen := map[string]bool{}
	add := func(line int, it domain.Item) error {
		c, known := ctxs[it.Project]
		r := PreviewRecord{Line: line, Date: it.Date, Type: it.Type, Context: it.Project, Text: it.Summary,
			Fingerprint: it.Fingerprint, Status: "new", KnownContext: known, Private: known && !c.Kind.Exportable()}
		switch _, err := s.st.GetItemByFingerprint(ctx, it.Fingerprint); {
		case seen[it.Fingerprint]:
			r.Status = "duplicate"
		case err == nil:
			r.Status = "existing"
		case !errors.Is(err, domain.ErrNotFound):
			return err
		}
		seen[it.Fingerprint] = true
		out.Records = append(out.Records, r)
		return nil
	}
	for _, b := range p.Reviews {
		for _, r := range b.Records {
			if err := add(r.Line, reviewItem(ctxs, r, ViaAPI)); err != nil {
				return out, err
			}
		}
	}
	for _, it := range p.Items {
		project := resolveProject(ctxs, it.Project)
		if err := add(0, domain.Item{Date: today, Project: project, Type: it.Type, Summary: it.Summary,
			Fingerprint: daily.Fingerprint(it.Type, project, it.Summary)}); err != nil {
			return out, err
		}
	}
	return out, nil
}

// ReadClipboard returns the clipboard text (for a dry-run import).
func (s *Service) ReadClipboard(ctx context.Context) (string, error) {
	return s.clipboard.ReadText(ctx)
}
