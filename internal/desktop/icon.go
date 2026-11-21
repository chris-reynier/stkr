package desktop

import (
	"image"
	"image/color"
	"math"
)

// Icon colours. The context colours match the Stream Deck key images.
var (
	colProject  = color.RGBA{37, 99, 235, 255}
	colMode     = color.RGBA{109, 40, 217, 255}
	colPersonal = color.RGBA{4, 120, 87, 255}
	colSystem   = color.RGBA{75, 85, 99, 255}
	colIdle     = color.RGBA{217, 119, 6, 255}
	colStopped  = color.RGBA{156, 163, 175, 255}
	colBadge    = color.RGBA{220, 38, 38, 255}
	colDot      = color.RGBA{255, 255, 255, 255}
)

// IconState is what the tray icon shows.
type IconState struct {
	Color   color.RGBA
	Running bool // white dot in the middle
	Badge   bool // red badge: the close is due
}

// IconFor derives the icon from the last status (nil = not reachable).
func IconFor(st *Status) IconState {
	if st == nil {
		return IconState{Color: colStopped}
	}
	is := IconState{Color: colStopped, Badge: st.CloseDue}
	if st.Now != nil {
		is.Running = true
		is.Color = kindColor(st.Now.Kind)
		if st.Now.IdleSuspect {
			is.Color = colIdle
		}
	}
	return is
}

func kindColor(kind string) color.RGBA {
	switch kind {
	case "mode":
		return colMode
	case "personal":
		return colPersonal
	case "system":
		return colSystem
	default:
		return colProject
	}
}

// DrawIcon renders the icon as a size x size RGBA image (16 and 32 are the
// usual tray sizes): a filled circle in the state colour, a white dot while
// running and a red badge at the top right when the close is due.
// Edges are anti-aliased with 4x4 supersampling.
func DrawIcon(is IconState, size int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	type disc struct {
		cx, cy, r float64
		c         color.RGBA
	}
	discs := []disc{{s / 2, s / 2, s/2 - s/32, is.Color}}
	if is.Running {
		discs = append(discs, disc{s / 2, s / 2, s * 0.18, colDot})
	}
	if is.Badge {
		discs = append(discs,
			disc{s * 0.76, s * 0.24, s * 0.26, color.RGBA{255, 255, 255, 255}},
			disc{s * 0.76, s * 0.24, s * 0.20, colBadge})
	}
	const ss = 4
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px := float64(x) + (float64(sx)+0.5)/ss
					py := float64(y) + (float64(sy)+0.5)/ss
					// The topmost disc that covers the sample wins.
					for i := len(discs) - 1; i >= 0; i-- {
						d := discs[i]
						if math.Hypot(px-d.cx, py-d.cy) <= d.r {
							r += float64(d.c.R)
							g += float64(d.c.G)
							b += float64(d.c.B)
							a += 255
							break
						}
					}
				}
			}
			n := float64(ss * ss)
			if a == 0 {
				continue
			}
			// Store non-premultiplied colour averaged over covered samples,
			// with coverage as alpha; image.RGBA is premultiplied.
			cov := a / 255
			alpha := a / n
			img.SetRGBA(x, y, color.RGBA{
				R: uint8(r / cov * alpha / 255),
				G: uint8(g / cov * alpha / 255),
				B: uint8(b / cov * alpha / 255),
				A: uint8(alpha),
			})
		}
	}
	return img
}
