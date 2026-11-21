package desktop

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite golden files")

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update)", err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		t.Errorf("%s mismatch:\n--- got\n%s--- want\n%s", name, got, want)
	}
}

func fixtureStatus() *Status {
	return &Status{
		Now: &NowStatus{ContextID: "acme", Label: "Acme & Co portal", Kind: "project",
			Since: time.Date(2026, 11, 16, 8, 0, 0, 0, time.UTC), ElapsedS: 72 * 60},
		Contexts: []StatusContext{
			{ID: "acme", Label: "Acme & Co portal", Kind: "project"},
			{ID: "meeting", Label: "Meeting", Kind: "mode"},
			{ID: "break", Label: "", Kind: "personal"},
		},
		Version: "0.2.0", HostedBy: "desktop",
	}
}

func TestMenuGolden(t *testing.T) {
	busy := fixtureStatus()
	busy.Now.IdleSuspect = true
	busy.Now.ElapsedS = 3*3600 + 5*60 + 59
	busy.CloseDue = true
	busy.AgentsWaiting = 2
	busy.IdeasNew = 3
	withUpdate := fixtureStatus()
	withUpdate.Update = &UpdateStatus{Available: true, Version: "v0.3.0"}
	tests := []struct {
		name string
		st   *Status
	}{
		{"menu_offline", nil},
		{"menu_stopped", &Status{Contexts: []StatusContext{}, Version: "0.2.0"}},
		{"menu_running", fixtureStatus()},
		{"menu_busy", busy},
		{"menu_update", withUpdate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			golden(t, tt.name, RenderMenu(BuildMenu(tt.st, MenuOptions{Version: "0.2.0", PanelOnTop: true})))
		})
	}
}

func TestMenuEveryActionIsKnown(t *testing.T) {
	var ids []string
	var walk func([]MenuItem)
	walk = func(items []MenuItem) {
		for _, it := range items {
			if it.ID != "" {
				ids = append(ids, it.ID)
			}
			walk(it.Children)
		}
	}
	walk(BuildMenu(fixtureStatus(), MenuOptions{Version: "0.2.0"}))
	a := NewApp(Options{Tray: &fakeTray{}})
	for _, id := range ids {
		if id == ActQuit {
			continue
		}
		// Without a server every API action fails with errOffline, never
		// with "unknown action".
		if _, err := a.do(id); err != nil && strings.Contains(err.Error(), "unknown") {
			t.Errorf("%s: %v", id, err)
		}
	}
}

func TestFormatElapsed(t *testing.T) {
	for in, want := range map[int64]string{-5: "0h00", 0: "0h00", 59: "0h00", 300: "0h05", 4320: "1h12", 36000: "10h00"} {
		if got := FormatElapsed(in); got != want {
			t.Errorf("FormatElapsed(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestTooltip(t *testing.T) {
	st := fixtureStatus()
	if got := Tooltip(st); got != "STKR - Acme & Co portal 1h12" {
		t.Fatalf("tooltip = %q", got)
	}
	st.Now.IdleSuspect, st.CloseDue = true, true
	if got := Tooltip(st); got != "STKR - Acme & Co portal 1h12 (idle?) - close due" {
		t.Fatalf("tooltip = %q", got)
	}
	if got := Tooltip(&Status{}); got != "STKR - Nothing running" {
		t.Fatalf("tooltip = %q", got)
	}
	if got := Tooltip(nil); got != "STKR - server not reachable" {
		t.Fatalf("tooltip = %q", got)
	}
	st.Now.Label = strings.Repeat("é", 300)
	if got := Tooltip(st); len([]rune(got)) != 127 || !strings.HasSuffix(got, "...") {
		t.Fatalf("long tooltip has %d runes", len([]rune(got)))
	}
	st.Now.Label = "  "
	if got := Tooltip(&Status{Now: &NowStatus{ContextID: "x1"}}); got != "STKR - x1 0h00" {
		t.Fatalf("tooltip without label = %q", got)
	}
}
