package service

import (
	"context"
	"encoding/csv"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/timeline"
)

// TempoHeader is the header row of the Tempo CSV (contract tempo-csv v1).
var TempoHeader = []string{"date", "ticket", "hours", "context", "description"}

const maxDescription = 500

// TempoRow is one (date, ticket) line of the Tempo export.
type TempoRow struct {
	Date        string   `json:"date"`
	Ticket      string   `json:"ticket"`
	Hours       float64  `json:"hours"`
	Contexts    []string `json:"contexts"`
	Description string   `json:"description"`
}

// UnmappedContext is an exportable context with counted time but no ticket.
type UnmappedContext struct {
	ContextID string          `json:"context_id"`
	Label     string          `json:"label"`
	Counted   domain.Duration `json:"counted_seconds"`
}

// TempoReport is the rounded export of one ISO week.
type TempoReport struct {
	Week          string            `json:"week"`
	Start         string            `json:"start"`
	End           string            `json:"end"`
	RoundingHours float64           `json:"rounding_hours"`
	Rows          []TempoRow        `json:"rows"`
	Unmapped      []UnmappedContext `json:"unmapped"`
}

// Tempo builds the Tempo export for an ISO week ("" = current). Counted time
// of exportable contexts that have a ticket is summed per (date, ticket) and
// rounded to rounding_hours after summing. Personal and system contexts are
// excluded; exportable contexts without a ticket are listed in Unmapped and
// produce no rows.
func (s *Service) Tempo(ctx context.Context, week string) (TempoReport, error) {
	w, err := s.Week(ctx, week)
	if err != nil {
		return TempoReport{}, err
	}
	return s.TempoFromWeek(w), nil
}

type tempoKey struct{ date, ticket string }

type tempoAcc struct {
	total    time.Duration
	contexts []string
	notes    []string
}

// TempoFromWeek builds the export from an already loaded week report.
func (s *Service) TempoFromWeek(w domain.WeekReport) TempoReport {
	step := time.Duration(s.cfg.RoundingHours * float64(time.Hour))
	rep := TempoReport{Week: w.Week, Start: w.Start, End: w.End, RoundingHours: s.cfg.RoundingHours, Rows: []TempoRow{}, Unmapped: []UnmappedContext{}}
	acc := map[tempoKey]*tempoAcc{}
	for _, d := range w.Days {
		tickets := map[string]string{}
		for _, t := range d.Totals {
			if t.Exportable && t.Counted > 0 {
				tickets[t.ContextID] = strings.TrimSpace(t.TempoTicket)
			}
		}
		for _, b := range d.Blocks {
			tk, ok := tickets[b.ContextID]
			if !ok || !b.Counted || tk == "" {
				continue
			}
			a := acc[tempoKey{d.Date, tk}]
			if a == nil {
				a = &tempoAcc{}
				acc[tempoKey{d.Date, tk}] = a
			}
			a.total += b.Duration.D()
			if !contains(a.contexts, b.ContextID) {
				a.contexts = append(a.contexts, b.ContextID)
			}
			if n := strings.TrimSpace(b.Note); n != "" && !contains(a.notes, n) {
				a.notes = append(a.notes, n)
			}
		}
	}
	labels := map[string]string{}
	for _, t := range w.Totals {
		labels[t.ContextID] = t.Label
		if t.Exportable && t.Counted > 0 && strings.TrimSpace(t.TempoTicket) == "" {
			rep.Unmapped = append(rep.Unmapped, UnmappedContext{ContextID: t.ContextID, Label: t.Label, Counted: t.Counted})
		}
	}
	for k, a := range acc {
		hours := timeline.RoundForExport(a.total, step).Hours()
		if hours <= 0 {
			continue
		}
		sort.Strings(a.contexts)
		desc := strings.Join(a.notes, "; ")
		if desc == "" {
			names := make([]string, len(a.contexts))
			for i, id := range a.contexts {
				names[i] = labels[id]
			}
			desc = strings.Join(names, "; ")
		}
		if r := []rune(desc); len(r) > maxDescription {
			desc = string(r[:maxDescription])
		}
		rep.Rows = append(rep.Rows, TempoRow{Date: k.date, Ticket: k.ticket, Hours: hours, Contexts: a.contexts, Description: desc})
	}
	sort.Slice(rep.Rows, func(i, j int) bool {
		if rep.Rows[i].Date != rep.Rows[j].Date {
			return rep.Rows[i].Date < rep.Rows[j].Date
		}
		return rep.Rows[i].Ticket < rep.Rows[j].Ticket
	})
	return rep
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// WriteCSV writes the report as RFC 4180 CSV (UTF-8, no BOM, CRLF line ends)
// with the header row. Text that could be read as a spreadsheet formula is
// prefixed with an apostrophe.
func (r TempoReport) WriteCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	_ = cw.Write(TempoHeader)
	for _, row := range r.Rows {
		_ = cw.Write([]string{
			row.Date, csvSafe(row.Ticket), strconv.FormatFloat(row.Hours, 'f', 2, 64),
			csvSafe(strings.Join(row.Contexts, "+")), csvSafe(row.Description),
		})
	}
	cw.Flush()
	return cw.Error()
}

// csvSafe stops spreadsheet formula injection from user-entered text.
func csvSafe(s string) string {
	if s != "" && (s[0] == '=' || s[0] == '+' || s[0] == '-' || s[0] == '@' || s[0] == '\t' || s[0] == '\r') {
		return "'" + s
	}
	return s
}

// UnmappedIDs returns the context ids of r.Unmapped.
func (r TempoReport) UnmappedIDs() []string {
	ids := make([]string, len(r.Unmapped))
	for i, u := range r.Unmapped {
		ids[i] = u.ContextID
	}
	return ids
}
