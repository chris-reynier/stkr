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

func TestPerfBudgets(t *testing.T) {
	if testing.Short() {
		t.Skip("perf")
	}
	if raceEnabled {
		t.Skip("timing budgets are not meaningful under the race detector")
	}
	f := newFixture(t)
	ids := []string{"other", "meeting"}
	post := make([]time.Duration, 200)
	for i := range post {
		f.clk.Advance(10 * time.Second)
		start := time.Now()
		w := f.do("POST", "/api/switch/"+ids[i%2])
		post[i] = time.Since(start)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body)
		}
	}
	if got := p95(post); got > 100*time.Millisecond {
		t.Errorf("POST switch p95 = %v, budget 100ms", got)
	}
	img := make([]time.Duration, 200)
	for i := range img {
		f.clk.Advance(time.Minute) // new spec each time: defeats the cache
		start := time.Now()
		w := f.do("GET", "/img/now.png")
		img[i] = time.Since(start)
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	if got := p95(img); got > 50*time.Millisecond {
		t.Errorf("now.png p95 = %v, budget 50ms", got)
	}
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
