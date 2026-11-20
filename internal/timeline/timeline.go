// Package timeline holds the pure time rules: events become blocks, blocks
// are flagged and clipped, uncovered working hours become gaps, and totals
// are aggregated. It does no I/O and never reads the wall clock.
package timeline

import (
	"fmt"
	"sort"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
)

// TimeOfDay is a local wall-clock time.
type TimeOfDay struct{ Hour, Minute int }

// ParseTimeOfDay parses "HH:MM".
func ParseTimeOfDay(s string) (TimeOfDay, error) {
	var t TimeOfDay
	if _, err := fmt.Sscanf(s, "%d:%d", &t.Hour, &t.Minute); err != nil || len(s) != 5 ||
		t.Hour < 0 || t.Hour > 23 || t.Minute < 0 || t.Minute > 59 {
		return TimeOfDay{}, domain.New(domain.ErrInvalid, fmt.Sprintf("invalid time of day %q (want HH:MM)", s))
	}
	return t, nil
}

// On returns this time of day on the local date of day, in loc.
func (t TimeOfDay) On(day time.Time, loc *time.Location) time.Time {
	y, m, d := day.In(loc).Date()
	return time.Date(y, m, d, t.Hour, t.Minute, 0, 0, loc)
}

// Config holds the settings the time rules need.
type Config struct {
	Loc      *time.Location
	DayStart TimeOfDay
	DayEnd   TimeOfDay
	IdleGap  time.Duration
}

func (c Config) loc() *time.Location {
	if c.Loc == nil {
		return time.Local
	}
	return c.Loc
}

// Midnight returns the start of the local day containing t.
func Midnight(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// NextMidnight returns the start of the local day after the one containing t.
func NextMidnight(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, loc)
}

// SortEvents returns a copy of events ordered by (TS, ID).
func SortEvents(events []domain.Event) []domain.Event {
	evs := append([]domain.Event(nil), events...)
	sort.SliceStable(evs, func(i, j int) bool {
		if !evs[i].TS.Equal(evs[j].TS) {
			return evs[i].TS.Before(evs[j].TS)
		}
		return evs[i].ID < evs[j].ID
	})
	return evs
}

// lastEnd is where a block ends when no later event exists on its local day:
// day_end if the block starts before it, otherwise the next local midnight.
func (c Config) lastEnd(start time.Time) time.Time {
	loc := c.loc()
	if de := c.DayEnd.On(start, loc); start.Before(de) {
		return de
	}
	return NextMidnight(start, loc)
}

// Blocks turns events into time blocks clipped to [rangeStart, rangeEnd).
//
// Rules:
//   - each non-off event starts a block; off events start nothing;
//   - a block ends at the next event on the same local day;
//   - the last block of a local day ends at day_end (or at local midnight if
//     it starts after day_end); no block crosses local midnight;
//   - no block extends past now;
//   - a block longer than IdleGap is idle-suspect and counts only when its
//     event is confirmed. The flag uses the unclipped block length.
func Blocks(events []domain.Event, rangeStart, rangeEnd, now time.Time, cfg Config) []domain.Block {
	evs := SortEvents(events)
	loc := cfg.loc()
	var out []domain.Block
	for i, e := range evs {
		if e.ContextID == domain.ContextOff {
			continue
		}
		start := e.TS
		end := cfg.lastEnd(start)
		if i+1 < len(evs) && evs[i+1].TS.Before(NextMidnight(start, loc)) {
			end = evs[i+1].TS
		}
		if now.Before(end) {
			end = now
		}
		if !end.After(start) {
			continue
		}
		suspect := end.Sub(start) > cfg.IdleGap
		b := domain.Block{
			EventID:     e.ID,
			ContextID:   e.ContextID,
			Start:       start.In(loc),
			End:         end.In(loc),
			IdleSuspect: suspect,
			Confirmed:   e.Confirmed,
			Counted:     !suspect || e.Confirmed,
			Note:        e.Note,
		}
		b.Duration = domain.Duration(end.Sub(start))
		out = append(out, b)
	}
	return Clip(out, rangeStart, rangeEnd)
}

// Clip restricts blocks to [start, end) and drops empty ones. Flags are kept.
// Clip is idempotent.
func Clip(blocks []domain.Block, start, end time.Time) []domain.Block {
	out := []domain.Block{}
	for _, b := range blocks {
		s, e := b.Start, b.End
		if s.Before(start) {
			s = start.In(s.Location())
		}
		if e.After(end) {
			e = end.In(e.Location())
		}
		if !e.After(s) {
			continue
		}
		b.Start, b.End, b.Duration = s, e, domain.Duration(e.Sub(s))
		out = append(out, b)
	}
	return out
}

// IsWeekday reports whether t falls on Monday to Friday in loc.
func IsWeekday(t time.Time, loc *time.Location) bool {
	wd := t.In(loc).Weekday()
	return wd != time.Saturday && wd != time.Sunday
}

// WorkingDay reports whether the local day containing day is a weekday with
// at least one non-off event.
func WorkingDay(events []domain.Event, day time.Time, cfg Config) bool {
	loc := cfg.loc()
	if !IsWeekday(day, loc) {
		return false
	}
	from, to := Midnight(day, loc), NextMidnight(day, loc)
	for _, e := range events {
		if e.ContextID != domain.ContextOff && !e.TS.Before(from) && e.TS.Before(to) {
			return true
		}
	}
	return false
}

// Gaps returns uncovered working-hours time within [rangeStart, rangeEnd) and
// before now. Only working days have gaps. The working window of a day is
// day_start to day_end. Idle-suspect blocks still cover time.
func Gaps(events []domain.Event, blocks []domain.Block, rangeStart, rangeEnd, now time.Time, cfg Config) []domain.Gap {
	loc := cfg.loc()
	out := []domain.Gap{}
	for day := Midnight(rangeStart, loc); day.Before(rangeEnd); day = NextMidnight(day, loc) {
		if !WorkingDay(events, day, cfg) {
			continue
		}
		ws := maxTime(cfg.DayStart.On(day, loc), rangeStart)
		we := minTime(minTime(cfg.DayEnd.On(day, loc), rangeEnd), now)
		cursor := ws
		for _, b := range sortedBlocks(blocks) {
			if !b.End.After(cursor) || !b.Start.Before(we) {
				continue
			}
			if b.Start.After(cursor) {
				out = appendGap(out, cursor, b.Start, loc)
			}
			cursor = b.End
		}
		if we.After(cursor) {
			out = appendGap(out, cursor, we, loc)
		}
	}
	return out
}

func appendGap(out []domain.Gap, s, e time.Time, loc *time.Location) []domain.Gap {
	return append(out, domain.Gap{Start: s.In(loc), End: e.In(loc), Duration: domain.Duration(e.Sub(s))})
}

func sortedBlocks(blocks []domain.Block) []domain.Block {
	bs := append([]domain.Block(nil), blocks...)
	sort.SliceStable(bs, func(i, j int) bool { return bs[i].Start.Before(bs[j].Start) })
	return bs
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// Aggregate sums counted time per context. Uncounted (unconfirmed
// idle-suspect) time is excluded. No rounding is applied.
func Aggregate(blocks []domain.Block) map[string]time.Duration {
	out := map[string]time.Duration{}
	for _, b := range blocks {
		if b.Counted {
			out[b.ContextID] += b.Duration.D()
		}
	}
	return out
}

// ExportTotals is Aggregate restricted to exportable contexts: personal,
// system and unknown kinds are dropped. Values are not rounded; use
// RoundForExport on each total at export time.
func ExportTotals(blocks []domain.Block, kinds map[string]domain.ContextKind) map[string]time.Duration {
	out := map[string]time.Duration{}
	for id, d := range Aggregate(blocks) {
		if kinds[id].Exportable() {
			out[id] = d
		}
	}
	return out
}

// RoundForExport rounds d to the nearest multiple of step (halves round up).
// It is the only place rounding happens. Negative values yield 0; a step <= 0
// returns d unchanged.
func RoundForExport(d, step time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	if step <= 0 {
		return d
	}
	return ((d + step/2) / step) * step
}

// Uncounted sums idle-suspect time that is not confirmed.
func Uncounted(blocks []domain.Block) time.Duration {
	var d time.Duration
	for _, b := range blocks {
		if !b.Counted {
			d += b.Duration.D()
		}
	}
	return d
}
