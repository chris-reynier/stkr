package timeline

import (
	"math/rand"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/chris-reynier/stkr/internal/domain"
)

var paris = mustLoc("Europe/Paris")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

var cfg = Config{Loc: paris, DayStart: TimeOfDay{9, 0}, DayEnd: TimeOfDay{17, 0}, IdleGap: 90 * time.Minute}

// at parses "2006-01-02 15:04" in Paris.
func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, paris)
	if err != nil {
		panic(err)
	}
	return t
}

func ev(id int64, ts, ctx string) domain.Event {
	return domain.Event{ID: id, TS: at(ts), ContextID: ctx, Source: domain.SourceCLI}
}

func confirmed(e domain.Event) domain.Event { e.Confirmed = true; return e }

type span struct {
	ctx        string
	start, end string
	suspect    bool
	counted    bool
}

func spans(bs []domain.Block) []span {
	out := []span{}
	for _, b := range bs {
		out = append(out, span{b.ContextID, b.Start.Format("01-02 15:04"), b.End.Format("01-02 15:04"), b.IdleSuspect, b.Counted})
	}
	return out
}

func TestBlocks(t *testing.T) {
	mon := "2026-11-16 "
	day := func() (time.Time, time.Time) { return at(mon + "00:00"), at("2026-11-17 00:00") }
	tests := []struct {
		name   string
		events []domain.Event
		from   time.Time
		to     time.Time
		now    time.Time
		want   []span
	}{
		{
			name:   "running block ends at now",
			events: []domain.Event{ev(1, mon+"09:00", "a")},
			now:    at(mon + "10:00"),
			want:   []span{{"a", "11-16 09:00", "11-16 10:00", false, true}},
		},
		{
			name:   "last block ends at day_end on a past day",
			events: []domain.Event{ev(1, mon+"16:00", "a")},
			now:    at("2026-11-18 08:00"),
			want:   []span{{"a", "11-16 16:00", "11-16 17:00", false, true}},
		},
		{
			name:   "switch, switch, off",
			events: []domain.Event{ev(1, mon+"09:00", "a"), ev(2, mon+"10:00", "b"), ev(3, mon+"10:30", domain.ContextOff)},
			now:    at(mon + "18:00"),
			want: []span{
				{"a", "11-16 09:00", "11-16 10:00", false, true},
				{"b", "11-16 10:00", "11-16 10:30", false, true},
			},
		},
		{
			name:   "block longer than idle gap is suspect and not counted",
			events: []domain.Event{ev(1, mon+"09:00", "a"), ev(2, mon+"11:00", "b"), ev(3, mon+"11:30", domain.ContextOff)},
			now:    at(mon + "18:00"),
			want: []span{
				{"a", "11-16 09:00", "11-16 11:00", true, false},
				{"b", "11-16 11:00", "11-16 11:30", false, true},
			},
		},
		{
			name:   "confirmed suspect block counts",
			events: []domain.Event{confirmed(ev(1, mon+"09:00", "a")), ev(2, mon+"11:00", domain.ContextOff)},
			now:    at(mon + "18:00"),
			want:   []span{{"a", "11-16 09:00", "11-16 11:00", true, true}},
		},
		{
			name:   "exactly idle gap is not suspect",
			events: []domain.Event{ev(1, mon+"09:00", "a"), ev(2, mon+"10:30", domain.ContextOff)},
			now:    at(mon + "18:00"),
			want:   []span{{"a", "11-16 09:00", "11-16 10:30", false, true}},
		},
		{
			name:   "running block becomes suspect after idle gap",
			events: []domain.Event{ev(1, mon+"09:00", "a")},
			now:    at(mon + "10:31"),
			want:   []span{{"a", "11-16 09:00", "11-16 10:31", true, false}},
		},
		{
			name:   "block after day_end runs to now",
			events: []domain.Event{ev(1, mon+"18:00", "a")},
			now:    at(mon + "19:00"),
			want:   []span{{"a", "11-16 18:00", "11-16 19:00", false, true}},
		},
		{
			name:   "block after day_end on a past day ends at midnight",
			events: []domain.Event{ev(1, mon+"22:00", "a")},
			now:    at("2026-11-17 12:00"),
			want:   []span{{"a", "11-16 22:00", "11-17 00:00", true, false}},
		},
		{
			name:   "next event before day_end with gap past day_end is not capped",
			events: []domain.Event{ev(1, mon+"16:00", "a"), ev(2, mon+"17:15", domain.ContextOff)},
			now:    at(mon + "20:00"),
			want:   []span{{"a", "11-16 16:00", "11-16 17:15", false, true}},
		},
		{
			name:   "no block crosses midnight",
			events: []domain.Event{ev(1, mon+"23:00", "a"), ev(2, "2026-11-17 01:00", "b")},
			from:   at(mon + "00:00"), to: at("2026-11-18 00:00"),
			now: at("2026-11-17 02:00"),
			want: []span{
				{"a", "11-16 23:00", "11-17 00:00", false, true},
				{"b", "11-17 01:00", "11-17 02:00", false, true},
			},
		},
		{
			name:   "clipped to range keeps flags of the whole block",
			events: []domain.Event{ev(1, mon+"09:00", "a"), ev(2, mon+"13:00", domain.ContextOff)},
			from:   at(mon + "10:00"), to: at(mon + "12:00"),
			now:  at(mon + "18:00"),
			want: []span{{"a", "11-16 10:00", "11-16 12:00", true, false}},
		},
		{
			name:   "event before range start is clipped in",
			events: []domain.Event{ev(1, mon+"08:00", "a"), ev(2, mon+"09:00", "b")},
			from:   at(mon + "08:30"), to: at(mon + "09:30"),
			now: at(mon + "09:20"),
			want: []span{
				{"a", "11-16 08:30", "11-16 09:00", false, true},
				{"b", "11-16 09:00", "11-16 09:20", false, true},
			},
		},
		{
			name:   "only off events",
			events: []domain.Event{ev(1, mon+"09:00", domain.ContextOff)},
			now:    at(mon + "18:00"),
			want:   []span{},
		},
		{
			name:   "same timestamp: later id wins, empty block dropped",
			events: []domain.Event{ev(2, mon+"09:00", "b"), ev(1, mon+"09:00", "a")},
			now:    at(mon + "09:30"),
			want:   []span{{"b", "11-16 09:00", "11-16 09:30", false, true}},
		},
		{
			name:   "future events are ignored",
			events: []domain.Event{ev(1, mon+"10:00", "a")},
			now:    at(mon + "09:00"),
			want:   []span{},
		},
		{
			name:   "unsorted input",
			events: []domain.Event{ev(2, mon+"10:00", domain.ContextOff), ev(1, mon+"09:30", "a")},
			now:    at(mon + "18:00"),
			want:   []span{{"a", "11-16 09:30", "11-16 10:00", false, true}},
		},
		{
			name:   "DST spring forward (Paris, 2026-03-29): 01:30 to 03:30 is 1h",
			events: []domain.Event{ev(1, "2026-03-29 01:30", "a"), ev(2, "2026-03-29 03:30", domain.ContextOff)},
			from:   at("2026-03-29 00:00"), to: at("2026-03-30 00:00"),
			now:  at("2026-03-30 09:00"),
			want: []span{{"a", "03-29 01:30", "03-29 03:30", false, true}},
		},
		{
			name:   "DST fall back (Paris, 2026-10-25): 01:30 to 02:30 is 2h and suspect",
			events: []domain.Event{ev(1, "2026-10-25 01:30", "a"), ev(2, "2026-10-25 02:30", domain.ContextOff)},
			from:   at("2026-10-25 00:00"), to: at("2026-10-26 00:00"),
			now:  at("2026-10-26 09:00"),
			want: []span{{"a", "10-25 01:30", "10-25 02:30", true, false}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to := tt.from, tt.to
			if from.IsZero() {
				from, to = day()
			}
			got := spans(Blocks(tt.events, from, to, tt.now, cfg))
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(span{})); diff != "" {
				t.Errorf("blocks mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestDSTDurations(t *testing.T) {
	// Spring forward: 01:30 -> 03:30 local is one real hour.
	bs := Blocks([]domain.Event{ev(1, "2026-03-29 01:30", "a"), ev(2, "2026-03-29 03:30", domain.ContextOff)},
		at("2026-03-29 00:00"), at("2026-03-30 00:00"), at("2026-03-30 09:00"), cfg)
	if len(bs) != 1 || bs[0].Duration.D() != time.Hour {
		t.Fatalf("spring forward: %+v", bs)
	}
	// Day length is 23h and 25h.
	if d := NextMidnight(at("2026-03-29 12:00"), paris).Sub(Midnight(at("2026-03-29 12:00"), paris)); d != 23*time.Hour {
		t.Errorf("spring day = %v", d)
	}
	if d := NextMidnight(at("2026-10-25 12:00"), paris).Sub(Midnight(at("2026-10-25 12:00"), paris)); d != 25*time.Hour {
		t.Errorf("fall day = %v", d)
	}
	// Fall back: 01:30 -> 02:30 (second occurrence ambiguity aside) spans 2h.
	bs = Blocks([]domain.Event{ev(1, "2026-10-25 01:30", "a"), ev(2, "2026-10-25 02:30", domain.ContextOff)},
		at("2026-10-25 00:00"), at("2026-10-26 00:00"), at("2026-10-26 09:00"), cfg)
	if len(bs) != 1 || bs[0].Duration.D() != 2*time.Hour {
		t.Fatalf("fall back: %+v", bs)
	}
}

func TestGaps(t *testing.T) {
	mon := "2026-11-16 "
	type g struct{ start, end string }
	tests := []struct {
		name   string
		events []domain.Event
		from   time.Time
		to     time.Time
		now    time.Time
		want   []g
	}{
		{
			name:   "before first event and after off",
			events: []domain.Event{ev(1, mon+"10:00", "a"), ev(2, mon+"12:00", domain.ContextOff)},
			now:    at(mon + "18:00"),
			want:   []g{{"09:00", "10:00"}, {"12:00", "17:00"}},
		},
		{
			name:   "between blocks after off then switch",
			events: []domain.Event{ev(1, mon+"09:00", "a"), ev(2, mon+"10:00", domain.ContextOff), ev(3, mon+"11:00", "b")},
			now:    at(mon + "12:00"),
			want:   []g{{"10:00", "11:00"}},
		},
		{
			name:   "not after now",
			events: []domain.Event{ev(1, mon+"10:00", "a"), ev(2, mon+"10:30", domain.ContextOff)},
			now:    at(mon + "11:00"),
			want:   []g{{"09:00", "10:00"}, {"10:30", "11:00"}},
		},
		{
			name:   "idle-suspect blocks cover time",
			events: []domain.Event{ev(1, mon+"09:00", "a")},
			now:    at(mon + "18:00"),
			want:   []g{},
		},
		{
			name:   "weekend has no gaps",
			events: []domain.Event{ev(1, "2026-11-21 10:00", "a"), ev(2, "2026-11-21 11:00", domain.ContextOff)},
			from:   at("2026-11-21 00:00"), to: at("2026-11-22 00:00"),
			now:  at("2026-11-23 00:00"),
			want: []g{},
		},
		{
			name:   "weekday with only off events is not a working day",
			events: []domain.Event{ev(1, mon+"10:00", domain.ContextOff)},
			now:    at(mon + "18:00"),
			want:   []g{},
		},
		{
			name:   "clipped to range",
			events: []domain.Event{ev(1, mon+"12:00", "a"), ev(2, mon+"13:00", domain.ContextOff)},
			from:   at(mon + "11:00"), to: at(mon + "14:00"),
			now:  at(mon + "18:00"),
			want: []g{{"11:00", "12:00"}, {"13:00", "14:00"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to := tt.from, tt.to
			if from.IsZero() {
				from, to = at(mon+"00:00"), at("2026-11-17 00:00")
			}
			bs := Blocks(tt.events, from, to, tt.now, cfg)
			got := []g{}
			for _, x := range Gaps(tt.events, bs, from, to, tt.now, cfg) {
				got = append(got, g{x.Start.Format("15:04"), x.End.Format("15:04")})
				if x.Duration.D() != x.End.Sub(x.Start) {
					t.Errorf("gap duration mismatch %+v", x)
				}
			}
			if diff := cmp.Diff(tt.want, got, cmp.AllowUnexported(g{})); diff != "" {
				t.Errorf("gaps (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWorkingDay(t *testing.T) {
	evs := []domain.Event{ev(1, "2026-11-16 10:00", "a"), ev(2, "2026-11-17 10:00", domain.ContextOff), ev(3, "2026-11-21 10:00", "a")}
	for _, tt := range []struct {
		day  string
		want bool
	}{{"2026-11-16 12:00", true}, {"2026-11-17 12:00", false}, {"2026-11-18 12:00", false}, {"2026-11-21 12:00", false}} {
		if got := WorkingDay(evs, at(tt.day), cfg); got != tt.want {
			t.Errorf("WorkingDay(%s) = %v", tt.day, got)
		}
	}
}

func TestAggregateAndExport(t *testing.T) {
	mon := "2026-11-16 "
	evs := []domain.Event{
		ev(1, mon+"09:00", "a"),            // 1h
		ev(2, mon+"10:00", "break"),        // 30m personal
		ev(3, mon+"10:30", "a"),            // 2h suspect, not counted
		ev(4, mon+"12:30", "sys"),          // 10m system
		ev(5, mon+"12:40", "mtg"),          // 50m mode
		confirmed(ev(6, mon+"13:30", "b")), // 2h suspect but confirmed
		ev(7, mon+"15:30", domain.ContextOff),
	}
	bs := Blocks(evs, at(mon+"00:00"), at("2026-11-17 00:00"), at(mon+"20:00"), cfg)
	agg := Aggregate(bs)
	want := map[string]time.Duration{"a": time.Hour, "break": 30 * time.Minute, "sys": 10 * time.Minute, "mtg": 50 * time.Minute, "b": 2 * time.Hour}
	if diff := cmp.Diff(want, agg); diff != "" {
		t.Errorf("aggregate (-want +got):\n%s", diff)
	}
	if u := Uncounted(bs); u != 2*time.Hour {
		t.Errorf("uncounted = %v", u)
	}
	kinds := map[string]domain.ContextKind{"a": domain.KindProject, "b": domain.KindProject, "break": domain.KindPersonal, "sys": domain.KindSystem, "mtg": domain.KindMode}
	exp := ExportTotals(bs, kinds)
	wantExp := map[string]time.Duration{"a": time.Hour, "mtg": 50 * time.Minute, "b": 2 * time.Hour}
	if diff := cmp.Diff(wantExp, exp); diff != "" {
		t.Errorf("export (-want +got):\n%s", diff)
	}
	// Totals are not rounded: 50m stays 50m until RoundForExport.
	if exp["mtg"] != 50*time.Minute {
		t.Errorf("export totals must not be rounded")
	}
	if delete(kinds, "a"); len(ExportTotals(bs, kinds)) != 2 {
		t.Errorf("unknown kind must be excluded")
	}
}

func TestRoundForExport(t *testing.T) {
	q := 15 * time.Minute
	tests := []struct {
		in, step, want time.Duration
	}{
		{0, q, 0},
		{-time.Minute, q, 0},
		{7 * time.Minute, q, 0},
		{7*time.Minute + 30*time.Second, q, q},
		{8 * time.Minute, q, q},
		{22 * time.Minute, q, q},
		{23 * time.Minute, q, 30 * time.Minute},
		{time.Hour + 52*time.Minute + 30*time.Second, q, 2 * time.Hour},
		{50 * time.Minute, q, 45 * time.Minute},
		{50 * time.Minute, 0, 50 * time.Minute},
	}
	for _, tt := range tests {
		if got := RoundForExport(tt.in, tt.step); got != tt.want {
			t.Errorf("RoundForExport(%v, %v) = %v, want %v", tt.in, tt.step, got, tt.want)
		}
	}
}

func TestParseTimeOfDay(t *testing.T) {
	for _, s := range []string{"9:00", "24:00", "12:60", "ab:cd", "", "12:00x"} {
		if _, err := ParseTimeOfDay(s); err == nil {
			t.Errorf("ParseTimeOfDay(%q) should fail", s)
		}
	}
	got, err := ParseTimeOfDay("08:45")
	if err != nil || got != (TimeOfDay{8, 45}) {
		t.Fatalf("got %v %v", got, err)
	}
	if !IsWeekday(at("2026-11-20 12:00"), paris) || IsWeekday(at("2026-11-22 12:00"), paris) {
		t.Error("IsWeekday")
	}
}

func TestNilLocationUsesLocal(t *testing.T) {
	c := Config{DayStart: TimeOfDay{9, 0}, DayEnd: TimeOfDay{17, 0}, IdleGap: time.Hour}
	start := time.Date(2026, 11, 16, 10, 0, 0, 0, time.Local)
	bs := Blocks([]domain.Event{{ID: 1, TS: start, ContextID: "a"}}, start.Add(-time.Hour), start.Add(time.Hour), start.Add(30*time.Minute), c)
	if len(bs) != 1 || bs[0].Duration.D() != 30*time.Minute {
		t.Fatalf("%+v", bs)
	}
}

// Property-style checks over random event streams.
func TestBlockProperties(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	ctxs := []string{"a", "b", "c", domain.ContextOff}
	base := at("2026-11-16 00:00")
	for iter := 0; iter < 300; iter++ {
		var evs []domain.Event
		n := rng.Intn(25)
		for i := 0; i < n; i++ {
			e := domain.Event{
				ID:        int64(i + 1),
				TS:        base.Add(time.Duration(rng.Intn(4*24*60)) * time.Minute),
				ContextID: ctxs[rng.Intn(len(ctxs))],
				Confirmed: rng.Intn(4) == 0,
			}
			evs = append(evs, e)
		}
		now := base.Add(time.Duration(rng.Intn(5*24*60)) * time.Minute)
		wideFrom, wideTo := base.Add(-24*time.Hour), base.Add(6*24*time.Hour)
		from := base.Add(time.Duration(rng.Intn(3*24*60)) * time.Minute)
		to := from.Add(time.Duration(1+rng.Intn(2*24*60)) * time.Minute)

		bs := Blocks(evs, from, to, now, cfg)
		var counted, total time.Duration
		for i, b := range bs {
			if b.Start.Before(from) || b.End.After(to) || !b.End.After(b.Start) {
				t.Fatalf("iter %d: block outside range or empty: %+v", iter, b)
			}
			if b.End.After(now) {
				t.Fatalf("iter %d: block past now", iter)
			}
			if i > 0 && b.Start.Before(bs[i-1].End) {
				t.Fatalf("iter %d: overlapping blocks", iter)
			}
			if Midnight(b.Start, paris) != Midnight(b.End.Add(-time.Nanosecond), paris) {
				t.Fatalf("iter %d: block crosses midnight: %+v", iter, b)
			}
			total += b.Duration.D()
		}
		for _, d := range Aggregate(bs) {
			counted += d
		}
		if counted > total || total > to.Sub(from) {
			t.Fatalf("iter %d: counted %v total %v range %v", iter, counted, total, to.Sub(from))
		}
		// Clipping is idempotent and computing on a wide range then clipping
		// gives the same result as computing on the range directly.
		if diff := cmp.Diff(bs, Clip(bs, from, to)); diff != "" {
			t.Fatalf("iter %d: clip not idempotent:\n%s", iter, diff)
		}
		if diff := cmp.Diff(spans(bs), spans(Clip(Blocks(evs, wideFrom, wideTo, now, cfg), from, to)), cmp.AllowUnexported(span{})); diff != "" {
			t.Fatalf("iter %d: wide-then-clip differs:\n%s", iter, diff)
		}
		// Adjacent ranges add up to the combined range.
		mid := from.Add(to.Sub(from) / 2)
		left, right := Blocks(evs, from, mid, now, cfg), Blocks(evs, mid, to, now, cfg)
		if sum(left)+sum(right) != total {
			t.Fatalf("iter %d: split ranges %v + %v != %v", iter, sum(left), sum(right), total)
		}
		// Gaps never overlap blocks and lie in working hours.
		for _, g := range Gaps(evs, bs, from, to, now, cfg) {
			for _, b := range bs {
				if g.Start.Before(b.End) && b.Start.Before(g.End) {
					t.Fatalf("iter %d: gap %v-%v overlaps block %v-%v", iter, g.Start, g.End, b.Start, b.End)
				}
			}
			if g.Start.Before(cfg.DayStart.On(g.Start, paris)) || g.End.After(cfg.DayEnd.On(g.Start, paris)) || g.End.After(now) {
				t.Fatalf("iter %d: gap outside working hours: %+v", iter, g)
			}
		}
	}
}

func sum(bs []domain.Block) time.Duration {
	var d time.Duration
	for _, b := range bs {
		d += b.Duration.D()
	}
	return d
}
