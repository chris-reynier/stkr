package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/chris-reynier/stkr/internal/config"
)

// Rect is a rectangle in screen pixels (Right and Bottom are exclusive).
type Rect struct{ Left, Top, Right, Bottom int }

// W is the width of r.
func (r Rect) W() int { return r.Right - r.Left }

// H is the height of r.
func (r Rect) H() int { return r.Bottom - r.Top }

// overlap is the area shared by r and o.
func (r Rect) overlap(o Rect) int {
	w := min(r.Right, o.Right) - max(r.Left, o.Left)
	h := min(r.Bottom, o.Bottom) - max(r.Top, o.Top)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

func (r Rect) contains(p Point) bool {
	return p.X >= r.Left && p.X < r.Right && p.Y >= r.Top && p.Y < r.Bottom
}

// Point is a position in screen pixels.
type Point struct{ X, Y int }

// Placement is where a window sits. Maximized is only used by the app
// window; X, Y, W and H are then the restored (normal) rectangle.
type Placement struct {
	X         int  `json:"x"`
	Y         int  `json:"y"`
	W         int  `json:"w"`
	H         int  `json:"h"`
	Maximized bool `json:"maximized,omitempty"`
}

func (p Placement) rect() Rect { return Rect{p.X, p.Y, p.X + p.W, p.Y + p.H} }

// WindowState is the content of <home>/desktop/window.json: the last
// position of the panel and of the app window. It is not configuration.
type WindowState struct {
	Version int        `json:"version"`
	Panel   *Placement `json:"panel,omitempty"`
	App     *Placement `json:"app,omitempty"`
}

// WindowStateVersion is the format version written to window.json.
const WindowStateVersion = 1

// StatePath is where the window state of a data folder is kept.
func StatePath(home string) string { return filepath.Join(home, "desktop", "window.json") }

// LoadState reads the window state. A missing file is an empty state. A
// damaged or unusable file also gives an empty state, together with an error
// the caller may log: the windows then simply open at their default place.
func LoadState(path string) (WindowState, error) {
	data, err := os.ReadFile(path) //nolint:gosec // path is under the data folder
	if errors.Is(err, fs.ErrNotExist) {
		return WindowState{}, nil
	}
	if err != nil {
		return WindowState{}, err
	}
	var st WindowState
	if err := json.Unmarshal(data, &st); err != nil {
		return WindowState{}, fmt.Errorf("window state: %w", err)
	}
	if st.Version != WindowStateVersion {
		return WindowState{}, fmt.Errorf("window state: unknown version %d", st.Version)
	}
	for _, p := range []**Placement{&st.Panel, &st.App} {
		if *p != nil && ((*p).W <= 0 || (*p).H <= 0) {
			*p = nil
		}
	}
	return st, nil
}

// SaveState writes the window state atomically (temp file, fsync, rename).
func SaveState(path string, st WindowState) error {
	st.Version = WindowStateVersion
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return config.WriteFileAtomic(path, append(data, '\n'), 0o600)
}

// Clamp moves and shrinks p so that it lies inside the work area (the screen
// minus the taskbar) it overlaps most, or inside areas[0] (the primary one)
// when it overlaps none, for example after a monitor was unplugged. The size
// is kept unless it does not fit, and never goes below minW x minH unless the
// work area itself is smaller. With no areas p is returned unchanged.
func Clamp(p Placement, minW, minH int, areas []Rect) Placement {
	if len(areas) == 0 {
		return p
	}
	best, bestOverlap := areas[0], -1
	for _, a := range areas {
		if o := p.rect().overlap(a); o > bestOverlap {
			best, bestOverlap = a, o
		}
	}
	p.W = min(max(p.W, minW), best.W())
	p.H = min(max(p.H, minH), best.H())
	p.X = min(max(p.X, best.Left), best.Right-p.W)
	p.Y = min(max(p.Y, best.Top), best.Bottom-p.H)
	return p
}

// anchorGap is the distance kept between a panel and the tray icon.
const anchorGap = 8

// NearAnchor places a w x h window next to anchor (the tray icon or the
// mouse): centred on it horizontally, above it when the anchor is in the
// lower half of its work area and below it otherwise, then kept inside the
// work area that holds the anchor.
func NearAnchor(w, h int, anchor Point, areas []Rect) Placement {
	if len(areas) == 0 {
		return Placement{X: anchor.X - w/2, Y: anchor.Y - h - anchorGap, W: w, H: h}
	}
	area := areas[0]
	for _, a := range areas {
		if a.contains(anchor) {
			area = a
			break
		}
	}
	p := Placement{X: anchor.X - w/2, W: w, H: h}
	if anchor.Y > area.Top+area.H()/2 {
		p.Y = anchor.Y - h - anchorGap
	} else {
		p.Y = anchor.Y + anchorGap
	}
	return Clamp(p, 0, 0, []Rect{area})
}

// Centered places a w x h window in the middle of area, shrunk to fit.
func Centered(w, h int, area Rect) Placement {
	w, h = min(w, area.W()), min(h, area.H())
	return Placement{X: area.Left + (area.W()-w)/2, Y: area.Top + (area.H()-h)/2, W: w, H: h}
}
