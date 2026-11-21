package desktop

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

var (
	primary   = Rect{0, 0, 1920, 1040} // 1080p screen, 40 px taskbar at the bottom
	secondary = Rect{1920, 0, 3840, 1080}
	leftMon   = Rect{-1280, 100, 0, 1024} // a monitor left of the primary one
)

func TestRect(t *testing.T) {
	r := Rect{10, 20, 110, 220}
	if r.W() != 100 || r.H() != 200 {
		t.Fatalf("size %dx%d", r.W(), r.H())
	}
	if r.overlap(Rect{100, 200, 300, 400}) != 10*20 {
		t.Error("overlap of a shared corner")
	}
	if r.overlap(Rect{110, 20, 200, 220}) != 0 {
		t.Error("rectangles that only touch do not overlap")
	}
	if !r.contains(Point{10, 20}) || r.contains(Point{110, 20}) {
		t.Error("contains is inclusive at the start and exclusive at the end")
	}
}

func TestClamp(t *testing.T) {
	tests := []struct {
		name  string
		p     Placement
		areas []Rect
		want  Placement
	}{
		{"inside is unchanged", Placement{X: 100, Y: 100, W: 360, H: 560}, []Rect{primary}, Placement{X: 100, Y: 100, W: 360, H: 560}},
		{"off the right edge", Placement{X: 1800, Y: 100, W: 360, H: 560}, []Rect{primary}, Placement{X: 1560, Y: 100, W: 360, H: 560}},
		{"under the taskbar", Placement{X: 100, Y: 800, W: 360, H: 560}, []Rect{primary}, Placement{X: 100, Y: 480, W: 360, H: 560}},
		{"above and left of the screen", Placement{X: -500, Y: -90, W: 360, H: 560}, []Rect{primary}, Placement{X: 0, Y: 0, W: 360, H: 560}},
		{"larger than the work area shrinks", Placement{X: 0, Y: 0, W: 5000, H: 3000}, []Rect{primary}, Placement{X: 0, Y: 0, W: 1920, H: 1040}},
		{"too small grows to the minimum", Placement{X: 10, Y: 10, W: 50, H: 50}, []Rect{primary}, Placement{X: 10, Y: 10, W: 300, H: 380}},
		{"stays on the monitor it overlaps most", Placement{X: 2000, Y: 50, W: 360, H: 560}, []Rect{primary, secondary}, Placement{X: 2000, Y: 50, W: 360, H: 560}},
		{"straddling, mostly on the primary", Placement{X: 1700, Y: 50, W: 360, H: 560}, []Rect{primary, secondary}, Placement{X: 1560, Y: 50, W: 360, H: 560}},
		{"straddling, mostly on the second", Placement{X: 1900, Y: 50, W: 360, H: 560}, []Rect{primary, secondary}, Placement{X: 1920, Y: 50, W: 360, H: 560}},
		{"monitor unplugged falls back to the primary", Placement{X: 5000, Y: 50, W: 360, H: 560}, []Rect{primary}, Placement{X: 1560, Y: 50, W: 360, H: 560}},
		{"negative coordinates on a left monitor", Placement{X: -1000, Y: 200, W: 360, H: 560}, []Rect{primary, leftMon}, Placement{X: -1000, Y: 200, W: 360, H: 560}},
		{"no monitors keeps the placement", Placement{X: 7, Y: 8, W: 9, H: 10}, nil, Placement{X: 7, Y: 8, W: 9, H: 10}},
		{"maximized flag is kept", Placement{X: 0, Y: 0, W: 800, H: 600, Maximized: true}, []Rect{primary}, Placement{X: 0, Y: 0, W: 800, H: 600, Maximized: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Clamp(tt.p, 300, 380, tt.areas); got != tt.want {
				t.Errorf("Clamp = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestNearAnchor(t *testing.T) {
	tests := []struct {
		name   string
		anchor Point
		areas  []Rect
		want   Placement
	}{
		{"taskbar at the bottom: above the icon", Point{1800, 1060}, []Rect{primary}, Placement{X: 1560, Y: 480, W: 360, H: 560}},
		{"icon at the left edge is clamped", Point{40, 1060}, []Rect{primary}, Placement{X: 0, Y: 480, W: 360, H: 560}},
		{"taskbar at the top: below the icon", Point{1800, 10}, []Rect{primary}, Placement{X: 1560, Y: 18, W: 360, H: 560}},
		{"second monitor", Point{3000, 1050}, []Rect{primary, secondary}, Placement{X: 2820, Y: 482, W: 360, H: 560}},
		{"anchor outside every monitor uses the primary", Point{9000, 9000}, []Rect{primary}, Placement{X: 1560, Y: 480, W: 360, H: 560}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NearAnchor(360, 560, tt.anchor, tt.areas); got != tt.want {
				t.Errorf("NearAnchor = %+v, want %+v", got, tt.want)
			}
		})
	}
	if got := NearAnchor(360, 560, Point{500, 500}, nil); got.W != 360 || got.H != 560 {
		t.Errorf("without monitors: %+v", got)
	}
}

func TestCentered(t *testing.T) {
	if got := Centered(1000, 700, primary); got != (Placement{X: 460, Y: 170, W: 1000, H: 700}) {
		t.Errorf("Centered = %+v", got)
	}
	if got := Centered(5000, 5000, primary); got != (Placement{X: 0, Y: 0, W: 1920, H: 1040}) {
		t.Errorf("oversized Centered = %+v", got)
	}
}

func TestStateRoundTripAndAtomicSave(t *testing.T) {
	home := t.TempDir()
	path := StatePath(home)
	if want := filepath.Join(home, "desktop", "window.json"); path != want {
		t.Fatalf("path = %s", path)
	}
	if st, err := LoadState(path); err != nil || st.Panel != nil || st.App != nil {
		t.Fatalf("missing file: %+v, %v", st, err)
	}
	in := WindowState{Panel: &Placement{X: 10, Y: -20, W: 360, H: 560}, App: &Placement{X: 0, Y: 0, W: 1100, H: 780, Maximized: true}}
	if err := SaveState(path, in); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != WindowStateVersion || *got.Panel != *in.Panel || *got.App != *in.App {
		t.Fatalf("round trip: %+v", got)
	}
	// Saving again replaces the file and leaves no temp file behind.
	in.Panel.X = 99
	if err := SaveState(path, in); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("directory holds %d entries, want only window.json", len(entries))
	}
	if got, _ := LoadState(path); got.Panel.X != 99 {
		t.Errorf("second save lost: %+v", got.Panel)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
		}
	}
}

func TestLoadStateRejectsDamage(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{
		"garbage.json":  "{not json",
		"version.json":  `{"version": 99, "panel": {"x":1,"y":1,"w":10,"h":10}}`,
		"empty.json":    ``,
		"toplevel.json": `[]`,
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		st, err := LoadState(path)
		if err == nil {
			t.Errorf("%s: no error", name)
		}
		if st.Panel != nil || st.App != nil {
			t.Errorf("%s: state not empty: %+v", name, st)
		}
	}
	// A placement with no size is dropped, the other one is kept.
	path := filepath.Join(dir, "zero.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"panel":{"x":1,"y":1,"w":0,"h":0},"app":{"x":1,"y":1,"w":5,"h":5}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := LoadState(path)
	if err != nil || st.Panel != nil || st.App == nil {
		t.Fatalf("zero-size placement: %+v, %v", st, err)
	}
	if _, err := LoadState(dir); err == nil {
		t.Error("reading a directory gave no error")
	}
}
