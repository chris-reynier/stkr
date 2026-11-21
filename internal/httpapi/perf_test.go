package httpapi

import (
	"sort"
	"testing"
	"time"
)

func p95(d []time.Duration) time.Duration {
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return d[len(d)*95/100]
}

// withinBudget runs measure (after a warm-up pass) and requires its p95 to be
// inside the budget in at least one of three attempts. A single slow attempt
// on a busy machine (parallel test packages, GC pause) is noise; a real
// regression is slow every time.
func withinBudget(t *testing.T, name string, budget time.Duration, measure func(n int) []time.Duration) {
	t.Helper()
	measure(30) // warm-up: template/font caches, allocator, scheduler
	var got time.Duration
	for attempt := 1; attempt <= 3; attempt++ {
		got = p95(measure(200))
		t.Logf("%s p95 = %v (attempt %d, budget %v)", name, got, attempt, budget)
		if got <= budget {
			return
		}
	}
	t.Errorf("%s p95 = %v in three attempts, budget %v", name, got, budget)
}

func TestPerfBudgets(t *testing.T) {
	if testing.Short() {
		t.Skip("perf")
	}
	if raceEnabled {
		t.Skip("timing budgets are not meaningful under the race detector")
	}
	f := newFixture(t)
	ids := []string{"other", "meeting"}
	withinBudget(t, "POST switch", 100*time.Millisecond, func(n int) []time.Duration {
		post := make([]time.Duration, n)
		for i := range post {
			f.clk.Advance(10 * time.Second)
			start := time.Now()
			w := f.do("POST", "/api/switch/"+ids[i%2])
			post[i] = time.Since(start)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body)
			}
		}
		return post
	})
	withinBudget(t, "GET now.png", 50*time.Millisecond, func(n int) []time.Duration {
		img := make([]time.Duration, n)
		for i := range img {
			f.clk.Advance(time.Minute) // new spec each time: defeats the cache
			start := time.Now()
			w := f.do("GET", "/img/now.png")
			img[i] = time.Since(start)
			if w.Code != 200 {
				t.Fatal(w.Code)
			}
		}
		return img
	})
}

func BenchmarkSwitch(b *testing.B) {
	f := newFixture(b)
	ids := []string{"other", "meeting"}
	for i := 0; i < b.N; i++ {
		f.clk.Advance(10 * time.Second)
		if w := f.do("POST", "/api/switch/"+ids[i%2]); w.Code != 200 {
			b.Fatal(w.Code)
		}
	}
}

func BenchmarkNowPNG(b *testing.B) {
	f := newFixture(b)
	f.do("POST", "/api/switch/other")
	for i := 0; i < b.N; i++ {
		f.clk.Advance(time.Minute)
		if w := f.do("GET", "/img/now.png"); w.Code != 200 {
			b.Fatal(w.Code)
		}
	}
}
