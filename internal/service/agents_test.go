package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chris-reynier/stkr/internal/domain"
)

type fakeWin struct {
	mu      sync.Mutex
	present map[string]bool // titles that exist
	err     error           // returned instead of not-found/ok when set
	calls   []string
	block   chan struct{}
}

func (w *fakeWin) Focus(_ context.Context, title string, _ time.Duration) error {
	if w.block != nil {
		<-w.block
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, title)
	if w.err != nil {
		return w.err
	}
	if w.present[title] {
		return nil
	}
	return domain.New(domain.ErrNotFound, "no window")
}

type fakeLauncher struct {
	opened []string
	err    error
}

func (l *fakeLauncher) OpenWorkspace(_ context.Context, p string) error {
	l.opened = append(l.opened, p)
	return l.err
}

type agentFixture struct {
	*fixture
	win *fakeWin
	lau *fakeLauncher
}

func newAgentFixture(t *testing.T) *agentFixture {
	t.Helper()
	f := newFixture(t)
	a := &agentFixture{fixture: f, win: &fakeWin{present: map[string]bool{}}, lau: &fakeLauncher{}}
	f.svc.windows, f.svc.launcher = a.win, a.lau
	return a
}

func (f *agentFixture) ev(t *testing.T, e AgentEvent) AgentEventResult {
	t.Helper()
	r, err := f.svc.AgentEvent(context.Background(), e)
	if err != nil {
		t.Fatalf("event %+v: %v", e, err)
	}
	return r
}

func (f *agentFixture) state(t *testing.T, id string, st domain.AgentState) {
	t.Helper()
	f.ev(t, AgentEvent{Instance: id, Event: AgentEventState, State: st})
}

func (f *agentFixture) get(t *testing.T, id string) domain.AgentInstance {
	t.Helper()
	a, err := f.st.GetAgent(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func (f *agentFixture) setWorkdir(t *testing.T, ctxID, dir string) {
	t.Helper()
	if _, err := f.svc.UpdateContext(context.Background(), ctxID, ContextPatch{Workdir: &dir}); err != nil {
		t.Fatal(err)
	}
}

func TestNormPath(t *testing.T) {
	cases := []struct{ a, b string }{
		{`C:\Work\Proj`, `c:/work/proj/`},
		{`C:\Work\Proj\`, `C:/Work/Proj`},
		{`c:\work\\proj`, `C:/WORK/proj`},
		{`/home/u/proj/`, `/home/u/proj`},
		{`/home/u/a/../proj`, `/home/u/proj`},
	}
	for _, c := range cases {
		if normPath(c.a) != normPath(c.b) {
			t.Errorf("normPath(%q)=%q != normPath(%q)=%q", c.a, normPath(c.a), c.b, normPath(c.b))
		}
	}
	if normPath(`C:\a`) == normPath(`C:\b`) || normPath("") != "" {
		t.Error("distinct or empty paths wrong")
	}
}

func TestAgentValidation(t *testing.T) {
	f := newAgentFixture(t)
	slot7 := 7
	long := strings.Repeat("x", 300)
	bad := []AgentEvent{
		{Event: "state", State: "idle"},
		{Instance: "a", Event: "boom", State: "idle"},
		{Instance: "a", Event: "state"},
		{Instance: "a", Event: "state", State: "sleeping"},
		{Instance: "a", Event: "state", State: "idle", Slot: &slot7},
		{Instance: "a\nb", Event: "state", State: "idle"},
	}
	for _, e := range bad {
		if _, err := f.svc.AgentEvent(context.Background(), e); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%+v: err = %v, want ErrInvalid", e, err)
		}
	}
	f.ev(t, AgentEvent{Instance: "a", Event: "state", State: "working", Summary: long})
	if got := len([]rune(f.get(t, "a").Summary)); got != MaxAgentSummary {
		t.Errorf("summary length = %d", got)
	}
}

func TestAgentPriority(t *testing.T) {
	type inst struct {
		id    string
		state domain.AgentState
		age   int // minutes before now
		queue bool
	}
	cases := []struct {
		name string
		in   []inst
		want string
	}{
		{"error wins", []inst{{"a", "action_required", 5, false}, {"b", "error", 1, false}, {"c", "done", 9, false}}, "b"},
		{"action over done", []inst{{"a", "done", 9, false}, {"b", "action_required", 1, false}, {"c", "working", 30, false}}, "b"},
		{"done over working and idle", []inst{{"a", "working", 50, false}, {"b", "done", 1, false}, {"c", "idle", 60, false}}, "b"},
		{"oldest within state", []inst{{"a", "done", 1, false}, {"b", "done", 20, false}, {"c", "done", 5, false}}, "b"},
		{"oldest of the rest", []inst{{"a", "working", 3, false}, {"b", "idle", 40, false}}, "b"},
		{"queued first within state", []inst{{"a", "done", 30, false}, {"b", "done", 2, true}}, "b"},
		{"queue does not outrank state", []inst{{"a", "error", 1, false}, {"b", "idle", 90, true}}, "a"},
		{"queued idle beats working", []inst{{"a", "working", 90, false}, {"b", "idle", 1, true}}, "b"},
		{"tie breaks by id", []inst{{"b", "done", 5, false}, {"a", "done", 5, false}}, "a"},
	}
	now := at("2026-11-16 12:00")
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var list []domain.AgentInstance
			for _, i := range c.in {
				list = append(list, domain.AgentInstance{ID: i.id, State: i.state, Updated: now.Add(-time.Duration(i.age) * time.Minute), Queued: i.queue})
			}
			got := pickNext(list)
			if got < 0 || list[got].ID != c.want {
				t.Fatalf("picked %v, want %s", got, c.want)
			}
		})
	}
	if pickNext(nil) != -1 {
		t.Error("empty list must give -1")
	}
}

func TestStreamDeckOnePressReachesActionRequired(t *testing.T) {
	f := newAgentFixture(t)
	f.setWorkdir(t, "other", `C:\work\ask`)
	f.state(t, "w1", domain.AgentWorking)
	f.state(t, "d1", domain.AgentDone)
	f.ev(t, AgentEvent{Instance: "ask", Event: "state", State: "action_required", Workspace: `C:\work\ask`, WindowTitle: "ask - Visual Studio Code"})
	f.state(t, "i1", domain.AgentIdle)
	f.win.present["ask - Visual Studio Code"] = true

	res, err := f.svc.GoNext(context.Background(), GoOptions{Source: domain.SourceDeck})
	if err != nil {
		t.Fatal(err)
	}
	if res.Result != GoFocused || res.Instance == nil || res.Instance.ID != "ask" {
		t.Fatalf("go = %+v", res)
	}
	if !res.Switched {
		t.Error("mapped instance must log a switch")
	}
	if got := f.win.calls; len(got) != 1 || got[0] != "ask - Visual Studio Code" {
		t.Errorf("focus calls = %v", got)
	}
}

func TestFocusedSwitchesWithDeckSource(t *testing.T) {
	f := newAgentFixture(t)
	f.setWorkdir(t, "planning", `C:\Work\Plan`)
	f.state(t, "x", domain.AgentDone)
	r := f.ev(t, AgentEvent{Instance: "x", Event: "focused", Workspace: "c:/work/plan/"})
	if !r.Switched || r.Instance == nil || !r.Instance.Mapped || r.Instance.ContextID != "planning" {
		t.Fatalf("result = %+v", r)
	}
	now, err := f.svc.Now(context.Background())
	if err != nil || now.Context == nil || now.Context.ID != "planning" {
		t.Fatalf("now = %+v err=%v", now, err)
	}
	ev, err := f.st.LastEvent(context.Background(), f.clk.Now())
	if err != nil || ev.Source != domain.SourceDeck || ev.ContextID != "planning" {
		t.Fatalf("event = %+v err=%v", ev, err)
	}
	if got := f.get(t, "x"); got.State != domain.AgentIdle || got.Queued {
		t.Errorf("focus must clear done: %+v", got)
	}
}

func TestFocusedClearsQueuedKeepsWorking(t *testing.T) {
	f := newAgentFixture(t)
	f.state(t, "x", domain.AgentWorking)
	if _, err := f.svc.Queue(context.Background(), "x"); err != nil {
		t.Fatal(err)
	}
	r := f.ev(t, AgentEvent{Instance: "x", Event: "focused"})
	if r.Switched {
		t.Error("unmapped focus must not switch")
	}
	if got := f.get(t, "x"); got.State != domain.AgentWorking || got.Queued {
		t.Errorf("got %+v", got)
	}
	if _, err := f.svc.Queue(context.Background(), "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("queue unknown: %v", err)
	}
}

func TestFocusedOnParkedContextDoesNotFail(t *testing.T) {
	f := newAgentFixture(t)
	f.setWorkdir(t, "meeting", "/w/m")
	if err := f.svc.ParkContext(context.Background(), "meeting"); err != nil {
		t.Fatal(err)
	}
	r := f.ev(t, AgentEvent{Instance: "x", Event: "focused", Workspace: "/w/m"})
	if r.Switched {
		t.Error("parked context must not switch")
	}
}

func TestClosedDeletes(t *testing.T) {
	f := newAgentFixture(t)
	f.state(t, "x", domain.AgentWorking)
	r := f.ev(t, AgentEvent{Instance: "x", Event: "closed"})
	if r.Instance != nil {
		t.Error("closed returns no instance")
	}
	if _, err := f.st.GetAgent(context.Background(), "x"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("still there: %v", err)
	}
	f.ev(t, AgentEvent{Instance: "x", Event: "closed"}) // idempotent
}

func TestSyncReplaces(t *testing.T) {
	f := newAgentFixture(t)
	f.state(t, "old", domain.AgentWorking)
	f.state(t, "keep", domain.AgentDone)
	if _, err := f.svc.Queue(context.Background(), "keep"); err != nil {
		t.Fatal(err)
	}
	updated := f.get(t, "keep").Updated
	f.clk.Advance(5 * time.Minute)
	one := 1
	l, err := f.svc.SyncAgents(context.Background(), []AgentEvent{
		{Instance: "keep", State: "done", Slot: &one, Workspace: "/p/k"},
		{Instance: "new", State: "error", Summary: "boom"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Instances) != 2 {
		t.Fatalf("instances = %+v", l.Instances)
	}
	if _, err := f.st.GetAgent(context.Background(), "old"); !errors.Is(err, domain.ErrNotFound) {
		t.Error("missing instance must be deleted")
	}
	k := f.get(t, "keep")
	if !k.Queued || !k.Updated.Equal(updated) || k.Slot != 1 || k.Workspace != "/p/k" {
		t.Errorf("keep = %+v (updated before %v)", k, updated)
	}
	if n := f.get(t, "new"); n.State != domain.AgentError || !n.Updated.Equal(f.clk.Now()) {
		t.Errorf("new = %+v", n)
	}
	// state change bumps updated
	f.clk.Advance(time.Minute)
	if _, err := f.svc.SyncAgents(context.Background(), []AgentEvent{{Instance: "keep", State: "working"}}); err != nil {
		t.Fatal(err)
	}
	if k := f.get(t, "keep"); !k.Updated.Equal(f.clk.Now()) || k.Workspace != "" {
		t.Errorf("keep after state change = %+v", k)
	}
	// empty sync clears everything
	if l, err := f.svc.SyncAgents(context.Background(), nil); err != nil || len(l.Instances) != 0 {
		t.Fatalf("empty sync: %+v %v", l, err)
	}
	// invalid sets are rejected whole
	if _, err := f.svc.SyncAgents(context.Background(), []AgentEvent{{Instance: "a", State: "idle"}, {Instance: "a", State: "idle"}}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("duplicate: %v", err)
	}
	if _, err := f.svc.SyncAgents(context.Background(), []AgentEvent{{Instance: "a"}}); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("no state: %v", err)
	}
}

func TestSweeper(t *testing.T) {
	f := newAgentFixture(t)
	f.state(t, "done-old", domain.AgentDone)
	f.state(t, "work-old", domain.AgentWorking)
	f.clk.Advance(29 * time.Minute)
	f.state(t, "done-new", domain.AgentDone)
	if n, _ := f.svc.SweepAgents(context.Background()); n != 0 {
		t.Fatalf("after 29 min swept %d", n)
	}
	f.clk.Advance(time.Minute) // done-old is 30 min old
	if n, _ := f.svc.SweepAgents(context.Background()); n != 1 {
		t.Fatalf("after 30 min swept %d", n)
	}
	if f.get(t, "done-old").State != domain.AgentIdle || f.get(t, "done-new").State != domain.AgentDone || f.get(t, "work-old").State != domain.AgentWorking {
		t.Error("wrong instances idled at 30 min")
	}
	f.clk.Advance(23*time.Hour + 29*time.Minute) // work-old is 23h59m silent; done-new is long past 30 min
	if n, _ := f.svc.SweepAgents(context.Background()); n != 1 || f.get(t, "work-old").State != domain.AgentWorking {
		t.Fatalf("at 23h59m swept %d (only done-new)", n)
	}
	f.clk.Advance(time.Minute)
	if n, _ := f.svc.SweepAgents(context.Background()); n != 1 {
		t.Fatalf("at 24h swept %d", n)
	}
	if f.get(t, "work-old").State != domain.AgentIdle {
		t.Error("24 h silent instance must be idle")
	}
	if n, _ := f.svc.SweepAgents(context.Background()); n != 0 {
		t.Errorf("idle instances are left alone, swept %d", n)
	}
}

func TestAckDone(t *testing.T) {
	f := newAgentFixture(t)
	f.state(t, "a", domain.AgentDone)
	f.state(t, "b", domain.AgentDone)
	f.state(t, "c", domain.AgentError)
	n, err := f.svc.AckDone(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	if f.get(t, "a").State != domain.AgentIdle || f.get(t, "c").State != domain.AgentError {
		t.Error("ack changed wrong instances")
	}
}

func TestUnknownWorkspacesAndMapping(t *testing.T) {
	f := newAgentFixture(t)
	f.ev(t, AgentEvent{Instance: "a", Event: "state", State: "idle", Workspace: `C:\Code\Alpha`})
	f.ev(t, AgentEvent{Instance: "b", Event: "state", State: "idle", Workspace: `c:/code/alpha`})
	f.ev(t, AgentEvent{Instance: "c", Event: "state", State: "idle"}) // no workspace: not "unknown"
	l, err := f.svc.Agents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Unknown) != 1 || l.Unknown[0].Instances != 2 {
		t.Fatalf("unknown = %+v", l.Unknown)
	}
	n, err := f.svc.MapWorkspace(context.Background(), `C:\Code\Alpha`, "other")
	if err != nil || n != 2 {
		t.Fatalf("map: n=%d err=%v", n, err)
	}
	l, _ = f.svc.Agents(context.Background())
	if len(l.Unknown) != 0 || !l.Instances[0].Mapped && !l.Instances[1].Mapped {
		t.Fatalf("after map: %+v", l)
	}
	if got := f.get(t, "a").ContextID; got != "other" {
		t.Errorf("stored context = %q", got)
	}
	if _, err := f.svc.MapWorkspace(context.Background(), "/x", "nope"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown context: %v", err)
	}
	if _, err := f.svc.MapWorkspace(context.Background(), " ", "other"); !errors.Is(err, domain.ErrInvalid) {
		t.Errorf("empty workspace: %v", err)
	}
}

func TestLabels(t *testing.T) {
	f := newAgentFixture(t)
	f.setWorkdir(t, "planning", "/w/plan")
	f.ev(t, AgentEvent{Instance: "a", Event: "state", State: "idle", Workspace: "/w/plan"})
	f.ev(t, AgentEvent{Instance: "b", Event: "state", State: "idle", Workspace: `C:\x\proj`})
	f.ev(t, AgentEvent{Instance: "c", Event: "state", State: "idle", WindowTitle: "T"})
	f.ev(t, AgentEvent{Instance: "d", Event: "state", State: "idle"})
	l, _ := f.svc.Agents(context.Background())
	got := map[string]string{}
	for _, v := range l.Instances {
		got[v.ID] = v.Label
	}
	want := map[string]string{"a": "Planning", "b": "proj", "c": "T", "d": "d"}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("label %s = %q, want %q", k, got[k], w)
		}
	}
}

func TestGoFallbacksAndResults(t *testing.T) {
	ctx := context.Background()
	t.Run("none", func(t *testing.T) {
		f := newAgentFixture(t)
		r, err := f.svc.GoNext(ctx, GoOptions{})
		if err != nil || r.Result != GoNone || r.Instance != nil {
			t.Fatalf("%+v %v", r, err)
		}
	})
	t.Run("launch when no window", func(t *testing.T) {
		f := newAgentFixture(t)
		f.ev(t, AgentEvent{Instance: "a", Event: "state", State: "error", Workspace: "/w/a"})
		r, _ := f.svc.GoNext(ctx, GoOptions{})
		if r.Result != GoLaunched || len(f.lau.opened) != 1 || f.lau.opened[0] != "/w/a" {
			t.Fatalf("%+v opened=%v", r, f.lau.opened)
		}
		if len(f.win.calls) != 1 || f.win.calls[0] != "a" {
			t.Errorf("focus tried %v, want the folder name", f.win.calls)
		}
	})
	t.Run("launch when focus unavailable", func(t *testing.T) {
		f := newAgentFixture(t)
		f.win.err = domain.New(domain.ErrUnavailable, "no")
		f.ev(t, AgentEvent{Instance: "a", Event: "state", State: "error", Workspace: "/w/a", WindowTitle: "t"})
		r, _ := f.svc.GoNext(ctx, GoOptions{})
		if r.Result != GoLaunched || len(f.win.calls) != 1 {
			t.Fatalf("%+v calls=%v", r, f.win.calls)
		}
	})
	t.Run("title then folder", func(t *testing.T) {
		f := newAgentFixture(t)
		f.win.present["a"] = true
		f.ev(t, AgentEvent{Instance: "a", Event: "state", State: "error", Workspace: "/w/a", WindowTitle: "gone"})
		r, _ := f.svc.GoNext(ctx, GoOptions{})
		if r.Result != GoFocused || strings.Join(f.win.calls, ",") != "gone,a" {
			t.Fatalf("%+v calls=%v", r, f.win.calls)
		}
	})
	t.Run("launch failure", func(t *testing.T) {
		f := newAgentFixture(t)
		f.lau.err = errors.New("no code")
		f.ev(t, AgentEvent{Instance: "a", Event: "state", State: "error", Workspace: "/w/a"})
		r, _ := f.svc.GoNext(ctx, GoOptions{})
		if r.Result != GoFailed || r.Message == "" {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("no workspace no window", func(t *testing.T) {
		f := newAgentFixture(t)
		f.state(t, "a", domain.AgentError)
		r, _ := f.svc.GoNext(ctx, GoOptions{})
		if r.Result != GoFailed {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("pending", func(t *testing.T) {
		f := newAgentFixture(t)
		f.win.block = make(chan struct{})
		f.ev(t, AgentEvent{Instance: "a", Event: "state", State: "error", WindowTitle: "t"})
		r, _ := f.svc.GoNext(ctx, GoOptions{Budget: 20 * time.Millisecond})
		close(f.win.block)
		if r.Result != GoPending {
			t.Fatalf("%+v", r)
		}
	})
	t.Run("go clears done and queue", func(t *testing.T) {
		f := newAgentFixture(t)
		f.state(t, "a", domain.AgentDone)
		_, _ = f.svc.Queue(ctx, "a")
		r, err := f.svc.GoAgent(ctx, "a", GoOptions{Source: domain.SourceUI})
		if err != nil || r.Result != GoFailed {
			t.Fatalf("%+v %v", r, err)
		}
		if a := f.get(t, "a"); a.State != domain.AgentIdle || a.Queued {
			t.Errorf("%+v", a)
		}
		if _, err := f.svc.GoAgent(ctx, "missing", GoOptions{}); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("missing: %v", err)
		}
	})
	t.Run("go switch source", func(t *testing.T) {
		f := newAgentFixture(t)
		f.setWorkdir(t, "meeting", "/w/m")
		f.ev(t, AgentEvent{Instance: "a", Event: "state", State: "error", Workspace: "/w/m"})
		r, _ := f.svc.GoNext(ctx, GoOptions{Source: domain.SourceCLI})
		ev, _ := f.st.LastEvent(ctx, f.clk.Now())
		if !r.Switched || ev.Source != domain.SourceCLI || ev.ContextID != "meeting" {
			t.Fatalf("%+v ev=%+v", r, ev)
		}
	})
}

func TestAgentLogsNoSummary(t *testing.T) {
	f := newAgentFixture(t)
	f.ev(t, AgentEvent{Instance: "a", Event: "state", State: "working", Summary: "SECRET-SUMMARY-TEXT"})
	if strings.Contains(f.logs.String(), "SECRET-SUMMARY-TEXT") {
		t.Error("summary leaked into logs")
	}
}

func (w *fakeWin) PasteInto(ctx context.Context, title string, _ bool, wait time.Duration) error {
	return w.Focus(ctx, title, wait)
}
