package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
	"github.com/chris-reynier/stkr/internal/service"
)

func TestStatusEndpoint(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()

	st := decodeJSON[service.Status](t, f.do("GET", "/api/status"))
	if st.Now != nil || st.CloseDue || st.Update != nil || st.AgentsWaiting != 0 || st.IdeasNew != 0 {
		t.Fatalf("empty status = %+v", st)
	}
	if st.HostedBy != "serve" || st.Version == "" {
		t.Fatalf("hosted_by %q version %q", st.HostedBy, st.Version)
	}
	for _, c := range st.Contexts {
		if c.ID == domain.ContextOff {
			t.Fatal("off must not be in the switch list")
		}
	}

	f.do("POST", "/api/switch/meeting")
	f.clk.Advance(10 * time.Minute)
	f.do("POST", "/api/switch/other")
	f.clk.Advance(72 * time.Minute)
	if _, err := f.svc.AgentEvent(ctx, service.AgentEvent{Instance: "a1", Event: service.AgentEventState, State: domain.AgentActionRequired}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AgentEvent(ctx, service.AgentEvent{Instance: "a2", Event: service.AgentEventState, State: domain.AgentWorking}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.AddIdea(ctx, service.IdeaInput{Text: "an idea"}); err != nil {
		t.Fatal(err)
	}
	f.clk.Set(time.Date(2026, 11, 16, 17, 0, 0, 0, time.UTC)) // after close_reminder 16:45

	st = decodeJSON[service.Status](t, f.do("GET", "/api/status"))
	if st.Now == nil || st.Now.ContextID != "other" || st.Now.Kind != domain.KindProject || st.Now.Label == "" {
		t.Fatalf("now = %+v", st.Now)
	}
	if st.Now.ElapsedS != int64((7*60+50)*60) || !st.Now.IdleSuspect {
		t.Fatalf("elapsed %d idle %v", st.Now.ElapsedS, st.Now.IdleSuspect)
	}
	if len(st.Contexts) < 2 || st.Contexts[0].ID != "other" || st.Contexts[1].ID != "meeting" {
		t.Fatalf("contexts not recent first: %+v", st.Contexts)
	}
	if !st.CloseDue || st.AgentsWaiting != 1 || st.IdeasNew != 1 {
		t.Fatalf("status = %+v", st)
	}
}

func TestStatusGuards(t *testing.T) {
	f := newFixture(t)
	if w := f.do("GET", "/api/status", "Host", "evil.example:7878"); w.Code != 421 {
		t.Fatalf("foreign host = %d", w.Code)
	}
	if w := f.do("POST", "/api/status", "Origin", "https://evil.example"); w.Code != 403 {
		t.Fatalf("foreign origin POST = %d", w.Code)
	}
	if w := f.do("POST", "/api/status"); w.Code != 404 {
		t.Fatalf("POST status = %d, want 404 (GET only)", w.Code)
	}
	if w := f.do("GET", "/api/status", "Origin", "https://evil.example"); w.Code != 200 {
		t.Fatalf("GET with origin = %d", w.Code)
	}
}

func TestStatusHostedBy(t *testing.T) {
	f := newFixture(t)
	srv := New(f.svc, Options{Port: testPort, HostedBy: "desktop"})
	f.h = srv.Handler()
	if st := decodeJSON[service.Status](t, f.do("GET", "/api/status")); st.HostedBy != "desktop" {
		t.Fatalf("hosted_by = %q", st.HostedBy)
	}
}

func TestPerfStatus(t *testing.T) {
	if testing.Short() {
		t.Skip("perf")
	}
	if raceEnabled {
		t.Skip("timing budgets are not meaningful under the race detector")
	}
	f := newFixture(t)
	// A realistic day: many switches.
	ids := []string{"other", "meeting", "planning"}
	for i := 0; i < 60; i++ {
		f.clk.Advance(5 * time.Minute)
		f.do("POST", "/api/switch/"+ids[i%3])
	}
	withinBudget(t, "GET status", 20*time.Millisecond, func(n int) []time.Duration {
		out := make([]time.Duration, n)
		for i := range out {
			start := time.Now()
			w := f.do("GET", "/api/status")
			out[i] = time.Since(start)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body)
			}
		}
		return out
	})
}

func BenchmarkStatus(b *testing.B) {
	f := newFixture(b)
	ids := []string{"other", "meeting", "planning"}
	for i := 0; i < 60; i++ {
		f.clk.Advance(5 * time.Minute)
		f.do("POST", "/api/switch/"+ids[i%3])
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if w := f.do("GET", "/api/status"); w.Code != 200 {
			b.Fatal(w.Code)
		}
	}
}
