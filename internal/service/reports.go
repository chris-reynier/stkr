package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/timeline"
)

const dateLayout = "2006-01-02"

// ParseDate resolves "", "today", "yesterday" or YYYY-MM-DD to local midnight.
func (s *Service) ParseDate(date string) (time.Time, error) {
	now := s.clk.Now()
	switch strings.ToLower(strings.TrimSpace(date)) {
	case "", "today":
		return timeline.Midnight(now, s.loc), nil
	case "yesterday":
		y, m, d := now.In(s.loc).Date()
		return time.Date(y, m, d-1, 0, 0, 0, 0, s.loc), nil
	}
	t, err := time.ParseInLocation(dateLayout, date, s.loc)
	if err != nil {
		return time.Time{}, invalid("invalid date %q (want YYYY-MM-DD, today or yesterday)", date)
	}
	return t, nil
}

// ParseWeek resolves "" (current week) or YYYY-Www to the local midnight of
// that ISO week's Monday.
func (s *Service) ParseWeek(week string) (time.Time, error) {
	week = strings.ToUpper(strings.TrimSpace(week))
	if week == "" {
		today := timeline.Midnight(s.clk.Now(), s.loc)
		off := (int(today.Weekday()) + 6) % 7
		y, m, d := today.Date()
		return time.Date(y, m, d-off, 0, 0, 0, 0, s.loc), nil
	}
	var y, w int
	if n, err := fmt.Sscanf(week, "%4d-W%2d", &y, &w); err != nil || n != 2 || len(week) != 8 || w < 1 || w > 53 {
		return time.Time{}, invalid("invalid week %q (want YYYY-Www, for example 2026-W07)", week)
	}
	jan4 := time.Date(y, 1, 4, 0, 0, 0, 0, s.loc)
	off := (int(jan4.Weekday()) + 6) % 7
	mon := time.Date(y, 1, 4-off+(w-1)*7, 0, 0, 0, 0, s.loc)
	if gy, gw := mon.ISOWeek(); gy != y || gw != w {
		return time.Time{}, invalid("week %q does not exist", week)
	}
	return mon, nil
}

// Day returns the report for a local date ("" or "today", "yesterday",
// YYYY-MM-DD).
func (s *Service) Day(ctx context.Context, date string) (domain.DayReport, error) {
	day, err := s.ParseDate(date)
	if err != nil {
		return domain.DayReport{}, err
	}
	from, to := day, timeline.NextMidnight(day, s.loc)
	events, err := s.st.EventsBetween(ctx, from, to)
	if err != nil {
		return domain.DayReport{}, err
	}
	ctxs, err := s.contextMap(ctx)
	if err != nil {
		return domain.DayReport{}, err
	}
	return s.dayReport(day, events, ctxs, s.clk.Now()), nil
}

// Week returns the report for an ISO week ("" for the current one).
func (s *Service) Week(ctx context.Context, week string) (domain.WeekReport, error) {
	mon, err := s.ParseWeek(week)
	if err != nil {
		return domain.WeekReport{}, err
	}
	y, m, d := mon.Date()
	end := time.Date(y, m, d+7, 0, 0, 0, 0, s.loc)
	events, err := s.st.EventsBetween(ctx, mon, end)
	if err != nil {
		return domain.WeekReport{}, err
	}
	ctxs, err := s.contextMap(ctx)
	if err != nil {
		return domain.WeekReport{}, err
	}
	now := s.clk.Now()
	iy, iw := mon.ISOWeek()
	rep := domain.WeekReport{
		Week:  fmt.Sprintf("%04d-W%02d", iy, iw),
		Start: mon.Format(dateLayout),
		End:   time.Date(y, m, d+6, 0, 0, 0, 0, s.loc).Format(dateLayout),
	}
	var all []domain.Block
	for i := 0; i < 7; i++ {
		day := time.Date(y, m, d+i, 0, 0, 0, 0, s.loc)
		dr := s.dayReport(day, events, ctxs, now)
		rep.Days = append(rep.Days, dr)
		all = append(all, dr.Blocks...)
		rep.IdleSuspect += dr.IdleSuspect
		rep.GapTotal += dr.GapTotal
		if dr.WorkingDay {
			rep.WorkingDays++
		}
	}
	rep.Totals, rep.Counted = totals(all, ctxs)
	return rep, nil
}

// dayReport builds the report of the local day starting at day. events may
// include events from other days; they are filtered here.
func (s *Service) dayReport(day time.Time, events []domain.Event, ctxs map[string]domain.Context, now time.Time) domain.DayReport {
	from, to := day, timeline.NextMidnight(day, s.loc)
	dayEvents := []domain.Event{}
	for _, e := range events {
		if !e.TS.Before(from) && e.TS.Before(to) {
			e.TS = e.TS.In(s.loc)
			dayEvents = append(dayEvents, e)
		}
	}
	blocks := timeline.Blocks(dayEvents, from, to, now, s.tl)
	for i := range blocks {
		c := ctxs[blocks[i].ContextID]
		blocks[i].Label, blocks[i].Kind = c.Label, c.Kind
	}
	gaps := timeline.Gaps(dayEvents, blocks, from, to, now, s.tl)
	rep := domain.DayReport{
		Date:        day.Format(dateLayout),
		Weekday:     day.Weekday().String(),
		WorkingDay:  timeline.WorkingDay(dayEvents, day, s.tl),
		WindowStart: s.tl.DayStart.On(day, s.loc),
		WindowEnd:   s.tl.DayEnd.On(day, s.loc),
		Events:      dayEvents,
		Blocks:      blocks,
		Gaps:        gaps,
		IdleSuspect: domain.Duration(timeline.Uncounted(blocks)),
	}
	for _, g := range gaps {
		rep.GapTotal += g.Duration
	}
	rep.Totals, rep.Counted = totals(blocks, ctxs)
	return rep
}

// totals returns counted time per context (largest first) and the sum.
func totals(blocks []domain.Block, ctxs map[string]domain.Context) ([]domain.ContextTotal, domain.Duration) {
	out := []domain.ContextTotal{}
	var sum domain.Duration
	for id, d := range timeline.Aggregate(blocks) {
		c, ok := ctxs[id]
		if !ok {
			c = domain.Context{ID: id, Label: id}
		}
		out = append(out, domain.ContextTotal{
			ContextID: id, Label: c.Label, Kind: c.Kind, TempoTicket: c.TempoTicket,
			Exportable: c.Kind.Exportable(), Counted: domain.Duration(d),
		})
		sum += domain.Duration(d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Counted != out[j].Counted {
			return out[i].Counted > out[j].Counted
		}
		return out[i].ContextID < out[j].ContextID
	})
	return out, sum
}

func (s *Service) contextMap(ctx context.Context) (map[string]domain.Context, error) {
	list, err := s.st.ListContexts(ctx, true)
	if err != nil {
		return nil, err
	}
	m := make(map[string]domain.Context, len(list))
	for _, c := range list {
		m[c.ID] = c
	}
	return m, nil
}

// ExportRows returns, for each day of the week, the unrounded counted time of
// every exportable context (personal and system contexts are left out).
func ExportRows(w domain.WeekReport) []ExportRow {
	var rows []ExportRow
	for _, d := range w.Days {
		for _, t := range d.Totals {
			if t.Exportable && t.Counted > 0 {
				rows = append(rows, ExportRow{Date: d.Date, Total: t})
			}
		}
	}
	return rows
}

// ExportRow is one day/context pair of exportable time.
type ExportRow struct {
	Date  string
	Total domain.ContextTotal
}
