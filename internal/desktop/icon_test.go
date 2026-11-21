package desktop

import (
	"image/color"
	"testing"
)

func TestIconFor(t *testing.T) {
	st := fixtureStatus()
	tests := []struct {
		name string
		st   *Status
		edit func(*Status)
		want IconState
	}{
		{"offline", nil, nil, IconState{Color: colStopped}},
		{"stopped", &Status{}, nil, IconState{Color: colStopped}},
		{"stopped close due", &Status{CloseDue: true}, nil, IconState{Color: colStopped, Badge: true}},
		{"project", st, nil, IconState{Color: colProject, Running: true}},
		{"mode", st, func(s *Status) { s.Now.Kind = "mode" }, IconState{Color: colMode, Running: true}},
		{"personal", st, func(s *Status) { s.Now.Kind = "personal" }, IconState{Color: colPersonal, Running: true}},
		{"system", st, func(s *Status) { s.Now.Kind = "system" }, IconState{Color: colSystem, Running: true}},
		{"idle", st, func(s *Status) { s.Now.IdleSuspect = true }, IconState{Color: colIdle, Running: true}},
		{"idle close due", st, func(s *Status) { s.Now.IdleSuspect, s.CloseDue = true, true }, IconState{Color: colIdle, Running: true, Badge: true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := tt.st
			if in != nil && in.Now != nil {
				cp := *in
				now := *in.Now
				cp.Now = &now
				in = &cp
			}
			if tt.edit != nil {
				tt.edit(in)
			}
			if got := IconFor(in); got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func near(a, b color.RGBA) bool {
	d := func(x, y uint8) int {
		if x > y {
			return int(x - y)
		}
		return int(y - x)
	}
	return d(a.R, b.R) <= 2 && d(a.G, b.G) <= 2 && d(a.B, b.B) <= 2 && d(a.A, b.A) <= 2
}

func TestDrawIcon(t *testing.T) {
	for _, size := range []int{16, 20, 32} {
		running := DrawIcon(IconState{Color: colProject, Running: true, Badge: true}, size)
		if b := running.Bounds(); b.Dx() != size || b.Dy() != size {
			t.Fatalf("size %d: bounds %v", size, b)
		}
		// Corners are transparent, the middle is the white dot, the left
		// edge of the disc has the state colour, the top right is the badge.
		if c := running.RGBAAt(0, size-1); c.A != 0 {
			t.Errorf("size %d: corner %v", size, c)
		}
		if c := running.RGBAAt(size/2, size/2); !near(c, colDot) {
			t.Errorf("size %d: centre %v", size, c)
		}
		if c := running.RGBAAt(size/8+1, size/2); !near(c, colProject) {
			t.Errorf("size %d: body %v", size, c)
		}
		if c := running.RGBAAt(size*3/4, size/4); !near(c, colBadge) {
			t.Errorf("size %d: badge %v", size, c)
		}
		stopped := DrawIcon(IconState{Color: colStopped}, size)
		if c := stopped.RGBAAt(size/2, size/2); !near(c, colStopped) {
			t.Errorf("size %d: stopped centre %v", size, c)
		}
		if c := stopped.RGBAAt(size*3/4, size/4); near(c, colBadge) {
			t.Errorf("size %d: badge drawn without Badge", size)
		}
	}
}
