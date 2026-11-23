package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chris-reynier/stkr/internal/daily"
	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/store"
)

const (
	maxImportBytes = 256 << 10
	// ImportRedirect is where a failed import sends the user.
	ImportRedirect = "/ui/today#import"
	// importKeyHold is how long the import key shows its result.
	importKeyHold = 10 * time.Second
	kvImportKey   = "key.import"
	otherContext  = "other"
)

// Import channels, stored as the item source.
const (
	ViaClipboard = "clipboard"
	ViaAPI       = "api"
	ViaCLI       = "cli"
	ViaUI        = "ui"
)

// Import key states.
const (
	ImportOK   = "ok"
	ImportWarn = "warn"
	ImportFail = "fail"
)

// ImportResult describes an import.
type ImportResult struct {
	OK bool `json:"ok"`
	// State is ok, warn or fail (what the Stream Deck key shows).
	State string `json:"state"`
	// Kind is brief, close, both or empty when the kind could not be told.
	Kind          string `json:"kind,omitempty"`
	Format        string `json:"format"`
	ItemsCreated  int    `json:"items_created"`
	ItemsExisting int    `json:"items_existing"`
	ItemsDone     int    `json:"items_done"`
	TopsSaved     int    `json:"tops_saved"`
	// ReviewBlocks counts imported stkr-review blocks, BlocksRejected the
	// review blocks dropped as a whole, Duplicates the repeated records
	// inside one import that were stored once.
	ReviewBlocks   int `json:"review_blocks"`
	BlocksRejected int `json:"blocks_rejected"`
	Duplicates     int `json:"duplicates"`
	// Private counts records filed under personal or system contexts: they
	// are stored but never exported.
	Private  int      `json:"private"`
	Ignored  int      `json:"ignored"`
	Warnings []string `json:"warnings"`
	// Exported lists the files written, relative to onedrive_dir.
	Exported []string `json:"exported"`
	// Redirect is set when nothing was imported: where to paste the text by hand.
	Redirect string `json:"redirect,omitempty"`
}

// NoValidBlockError is returned by Import when the text holds no usable
// record. Nothing was written. It matches domain.ErrInvalid.
type NoValidBlockError struct{ Result ImportResult }

func (e *NoValidBlockError) Error() string {
	return "no stkr-brief, stkr-close or stkr-review block with valid lines was found; nothing was imported"
}

// Is lets errors.Is(err, domain.ErrInvalid) match.
func (e *NoValidBlockError) Is(target error) bool {
	d, ok := target.(*domain.DomainError)
	return ok && d.Code == domain.ErrInvalid.Code
}

// ImportClipboard reads the clipboard and imports it.
func (s *Service) ImportClipboard(ctx context.Context) (ImportResult, error) {
	text, err := s.clipboard.ReadText(ctx)
	if err != nil {
		return ImportResult{}, err
	}
	return s.Import(ctx, text, ViaClipboard)
}

func kindName(p daily.Parsed) string {
	kind := ""
	switch {
	case p.Brief && p.Close:
		kind = "both"
	case p.Brief:
		kind = "brief"
	case p.Close:
		kind = "close"
	}
	switch {
	case len(p.Reviews) == 0:
		return kind
	case kind == "":
		return "review"
	}
	return kind + "+review"
}

// resolveProject maps the project text of a record to a context id (by id,
// then by label) or keeps the cleaned text for an unknown project.
func resolveProject(ctxs map[string]domain.Context, project string) string {
	p := strings.TrimSpace(project)
	if p == "" {
		return otherContext
	}
	if c, ok := ctxs[strings.ToLower(p)]; ok {
		return c.ID
	}
	for _, c := range ctxs {
		if strings.EqualFold(c.Label, p) {
			return c.ID
		}
	}
	r := []rune(p)
	if len(r) > 64 {
		r = r[:64]
	}
	return string(r)
}

// Import parses text (the answer of an AI chat) and stores it: items are
// added unless their fingerprint exists, the top three and the saved flags of
// today are updated, and the project logs are exported. via is one of the
// Via* constants. Without a usable record nothing is written, the import key
// turns red and the error is a *NoValidBlockError. The text is never logged.
func (s *Service) Import(ctx context.Context, text, via string) (ImportResult, error) {
	switch via {
	case ViaClipboard, ViaAPI, ViaCLI, ViaUI:
	default:
		via = ViaAPI
	}
	res := ImportResult{State: ImportFail, Warnings: []string{}, Exported: []string{}}
	if len(text) > maxImportBytes {
		s.setImportKey(ctx, ImportFail)
		return res, invalid("text is larger than %d KiB", maxImportBytes>>10)
	}
	p := daily.Parse(text)
	futureWarn := rejectFutureReviews(&p, s.todayDate())
	res.Format, res.Kind, res.Ignored = string(p.Format), kindName(p), p.Ignored
	res.ReviewBlocks, res.BlocksRejected = len(p.Reviews), p.BlocksRejected
	res.Warnings = append(res.Warnings, p.Warnings...)
	res.Warnings = append(res.Warnings, futureWarn...)
	if !p.Valid() {
		res.Redirect = ImportRedirect
		s.setImportKey(ctx, ImportFail)
		s.log.Info("import: no valid block", "chars", len(text), "ignored", p.Ignored, "via", via)
		return res, &NoValidBlockError{Result: res}
	}

	ctxs, err := s.contextMap(ctx)
	if err != nil {
		return res, err
	}
	today := s.todayDate()
	origin := res.Kind
	if p.Format == daily.FormatPipe {
		origin = "pipe"
	}
	touched := map[string]bool{}
	unmatched := 0
	var rn reviewNotes
	err = s.st.InTx(ctx, func(tx *store.Store) error {
		if err := importReviews(ctx, tx, ctxs, p.Reviews, via, &res, touched, &rn); err != nil {
			return err
		}
		for _, it := range p.Items {
			project := resolveProject(ctxs, it.Project)
			_, created, err := tx.InsertItem(ctx, domain.Item{
				Date: today, Project: project, Type: it.Type, Summary: it.Summary, Source: via, Due: it.Due,
				Origin: origin, Fingerprint: daily.Fingerprint(it.Type, project, it.Summary),
			})
			if err != nil {
				return err
			}
			if created {
				res.ItemsCreated++
			} else {
				res.ItemsExisting++
			}
			touched[exportKey(ctxs, project)] = true
		}
		if len(p.Done) > 0 {
			open, err := tx.ListItems(ctx, "", "open")
			if err != nil {
				return err
			}
			for _, ref := range p.Done {
				n := 0
				for i := range open {
					it := &open[i]
					if it.Status == "done" || !matchesRef(*it, ref) {
						continue
					}
					if err := tx.SetItemStatus(ctx, it.ID, "done"); err != nil {
						return err
					}
					n++
					touched[exportKey(ctxs, it.Project)] = true
					it.Status = "done"
				}
				if n == 0 {
					unmatched++
				}
				res.ItemsDone += n
			}
		}
		day, err := tx.GetDay(ctx, today)
		if err != nil {
			return err
		}
		var ranks [4]bool
		for _, t := range p.Tops {
			day.Top[t.Rank-1] = t.Text
			ranks[t.Rank] = true
		}
		for _, r := range ranks {
			if r {
				res.TopsSaved++
			}
		}
		day.BriefSaved = day.BriefSaved || p.Brief
		if p.Close && !day.CloseSaved {
			day.CloseSavedAt = s.clk.Now()
		}
		day.CloseSaved = day.CloseSaved || p.Close
		return tx.UpsertDay(ctx, day)
	})
	if err != nil {
		return res, err
	}
	if unmatched > 0 {
		res.Warnings = append(res.Warnings, fmt.Sprintf("%d done line(s) matched no open item", unmatched))
	}
	res.Warnings = append(res.Warnings, rn.warnings()...)
	if res.TopsSaved > 0 {
		touched[inboxKey] = true // the inbox file shows the top three
	}
	delete(touched, "")
	res.Exported, err = s.exportLogs(ctx, touched)
	if err != nil {
		s.log.Warn("import: export failed", "err", err)
		res.Warnings = append(res.Warnings, "export failed: "+err.Error())
	}
	res.OK = true
	res.State = ImportOK
	if len(res.Warnings) > 0 || res.Ignored > 0 {
		res.State = ImportWarn
	}
	s.setImportKey(ctx, res.State)
	s.log.Info("import", "via", via, "chars", len(text), "format", res.Format, "kind", res.Kind,
		"created", res.ItemsCreated, "existing", res.ItemsExisting, "done", res.ItemsDone, "tops", res.TopsSaved, "ignored", res.Ignored)
	return res, nil
}

// todayDate returns today's local date as YYYY-MM-DD.
func (s *Service) todayDate() string {
	d, _ := s.ParseDate("today")
	return d.Format(dateLayout)
}

// matchesRef tells whether a done reference (fingerprint or summary) names it.
func matchesRef(it domain.Item, ref string) bool {
	r := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(ref, "#")))
	if len(r) == 16 && strings.EqualFold(it.Fingerprint, r) {
		return true
	}
	return daily.NormalizeSummary(it.Summary) == daily.NormalizeSummary(ref)
}

// setImportKey remembers the import result for the Stream Deck key.
func (s *Service) setImportKey(ctx context.Context, state string) {
	v := state + "|" + s.clk.Now().UTC().Format(time.RFC3339)
	if err := s.st.SetKV(ctx, kvImportKey, v); err != nil {
		s.log.Warn("import key state not saved", "err", err)
	}
}

// ImportKeyState returns "ok", "warn" or "fail" for importKeyHold after an
// import, and "" afterwards (the key then shows its neutral face).
func (s *Service) ImportKeyState(ctx context.Context) (string, error) {
	v, err := s.st.GetKV(ctx, kvImportKey)
	if errors.Is(err, domain.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	state, ts, ok := strings.Cut(v, "|")
	t, perr := time.Parse(time.RFC3339, ts)
	if !ok || perr != nil {
		return "", nil
	}
	if s.clk.Now().Sub(t) >= importKeyHold {
		return "", nil
	}
	return state, nil
}

// Summary is a one-line, human readable description of the import.
func (r ImportResult) Summary() string {
	parts := []string{fmt.Sprintf("%d new item(s)", r.ItemsCreated)}
	if r.ItemsExisting > 0 {
		parts = append(parts, fmt.Sprintf("%d already known", r.ItemsExisting))
	}
	if r.ItemsDone > 0 {
		parts = append(parts, fmt.Sprintf("%d marked done", r.ItemsDone))
	}
	if r.TopsSaved > 0 {
		parts = append(parts, fmt.Sprintf("%d of the top 3 saved", r.TopsSaved))
	}
	s := "Imported: " + strings.Join(parts, ", ") + "."
	if r.Ignored > 0 {
		s += fmt.Sprintf(" %d line(s) ignored.", r.Ignored)
	}
	if len(r.Warnings) > 0 {
		w := r.Warnings[0]
		if len(r.Warnings) > 1 {
			w += fmt.Sprintf(" (+%d more)", len(r.Warnings)-1)
		}
		s += " Note: " + w
	}
	return s
}
