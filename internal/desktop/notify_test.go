package desktop

import (
	"strings"
	"testing"
)

func titles(ns []Notice) string {
	var out []string
	for _, n := range ns {
		out = append(out, n.Title)
	}
	return strings.Join(out, ",")
}

func TestWatcherTransitions(t *testing.T) {
	upd := func(v string, avail bool) *UpdateStatus { return &UpdateStatus{Available: avail, Version: v} }
	steps := []struct {
		name string
		st   Status
		want string
	}{
		{"quiet start", Status{}, ""},
		{"close due", Status{CloseDue: true}, "Close the day"},
		{"close still due: no repeat", Status{CloseDue: true}, ""},
		{"update found", Status{CloseDue: true, Update: upd("v0.3.0", true)}, "Update available"},
		{"same update: no repeat", Status{Update: upd("v0.3.0", true)}, ""},
		{"close saved then due again next day", Status{CloseDue: true, Update: upd("v0.3.0", true)}, "Close the day"},
		{"unavailable update ignored", Status{Update: upd("v0.4.0", false)}, ""},
		{"agent waits", Status{AgentsWaiting: 1}, "Agent waiting"},
		{"still one: no repeat", Status{AgentsWaiting: 1}, ""},
		{"second agent", Status{AgentsWaiting: 2, Update: upd("v0.4.0", true)}, "Update available,Agent waiting"},
		{"fewer: nothing", Status{AgentsWaiting: 1}, ""},
	}
	var w Watcher
	for _, s := range steps {
		got := w.Observe(s.st)
		if titles(got) != s.want {
			t.Fatalf("%s: got %q, want %q", s.name, titles(got), s.want)
		}
		for _, n := range got {
			if n.Text == "" || n.Error {
				t.Fatalf("%s: bad notice %+v", s.name, n)
			}
		}
	}
}

func TestWatcherAgentText(t *testing.T) {
	var w Watcher
	if n := w.Observe(Status{AgentsWaiting: 3}); len(n) != 1 || !strings.Contains(n[0].Text, "3 agents") {
		t.Fatalf("got %+v", n)
	}
}
