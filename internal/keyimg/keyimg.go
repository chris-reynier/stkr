// Package keyimg renders the 144x144 PNG images shown on Stream Deck keys.
// Rendering is local and deterministic; results are cached by spec.
package keyimg

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"
	"sync"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// Size is the width and height of a key image in pixels.
const Size = 144

// MaxLines is the number of text lines a key can show.
const MaxLines = 3

const (
	margin    = 8
	maxCached = 256
)

// KeySpec describes one key image.
type KeySpec struct {
	Background color.RGBA
	// Foreground is the text color. Zero value means white.
	Foreground color.RGBA
	// Lines holds up to MaxLines lines of text. The first line is the largest.
	Lines []string
	// Badge is optional short text (a few characters) drawn in the top-right corner.
	Badge string
}

var (
	cache   sync.Map // key string -> []byte
	cacheN  int
	cacheMu sync.Mutex

	fontsOnce sync.Once
	bold      *opentype.Font
	regular   *opentype.Font
	faceMu    sync.Mutex
	faces     = map[faceKey]font.Face{}
)

type faceKey struct {
	bold bool
	size int
}

func loadFonts() {
	fontsOnce.Do(func() {
		var err error
		if bold, err = opentype.Parse(gobold.TTF); err != nil {
			panic(err)
		}
		if regular, err = opentype.Parse(goregular.TTF); err != nil {
			panic(err)
		}
	})
}

// face returns a cached face. opentype faces are not safe for concurrent
// use, so callers hold faceMu while drawing.
func face(isBold bool, size int) font.Face {
	k := faceKey{isBold, size}
	if f, ok := faces[k]; ok {
		return f
	}
	loadFonts()
	src := regular
	if isBold {
		src = bold
	}
	f, err := opentype.NewFace(src, &opentype.FaceOptions{Size: float64(size), DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		panic(err)
	}
	faces[k] = f
	return f
}

func (s KeySpec) key() string {
	fg := s.Foreground
	if fg == (color.RGBA{}) {
		fg = color.RGBA{255, 255, 255, 255}
	}
	var b strings.Builder
	b.Grow(64)
	for _, c := range []uint8{s.Background.R, s.Background.G, s.Background.B, s.Background.A, fg.R, fg.G, fg.B, fg.A} {
		b.WriteByte(c)
	}
	n := len(s.Lines)
	if n > MaxLines {
		n = MaxLines
	}
	for _, l := range s.Lines[:n] {
		b.WriteString("\x00" + l)
	}
	b.WriteString("\x01" + s.Badge)
	return b.String()
}

// Render returns a PNG for spec. The returned slice is shared with the cache
// and must not be modified.
func Render(spec KeySpec) []byte {
	k := spec.key()
	if v, ok := cache.Load(k); ok {
		return v.([]byte)
	}
	out := draw1(spec)
	cacheMu.Lock()
	if cacheN >= maxCached {
		cache.Range(func(key, _ any) bool { cache.Delete(key); return true })
		cacheN = 0
	}
	cache.Store(k, out)
	cacheN++
	cacheMu.Unlock()
	return out
}

func draw1(spec KeySpec) []byte {
	bg := spec.Background
	if bg.A == 0 {
		bg.A = 255
	}
	fg := spec.Foreground
	if fg == (color.RGBA{}) {
		fg = color.RGBA{255, 255, 255, 255}
	}
	img := image.NewRGBA(image.Rect(0, 0, Size, Size))
	draw.Draw(img, img.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)

	lines := spec.Lines
	if len(lines) > MaxLines {
		lines = lines[:MaxLines]
	}
	faceMu.Lock()
	defer faceMu.Unlock()

	// Line heights: first line big, others smaller.
	maxSize := []int{34, 22, 18}
	faceList := make([]font.Face, len(lines))
	for i, l := range lines {
		faceList[i] = fit(l, i == 0, maxSize[i], Size-2*margin)
	}
	total := 0
	for _, f := range faceList {
		total += f.Metrics().Height.Ceil() + 2
	}
	y := (Size-total)/2 + 2
	for i, l := range lines {
		f := faceList[i]
		m := f.Metrics()
		w := font.MeasureString(f, l).Ceil()
		x := (Size - w) / 2
		if x < margin {
			x = margin
		}
		y += m.Ascent.Ceil()
		d := font.Drawer{Dst: img, Src: image.NewUniform(fg), Face: f, Dot: fixed.P(x, y)}
		d.DrawString(clip(f, l, Size-2*margin))
		y += m.Descent.Ceil() + 2
	}
	if spec.Badge != "" {
		f := face(true, 16)
		w := font.MeasureString(f, spec.Badge).Ceil()
		bw, bh := w+10, 22
		r := image.Rect(Size-bw-4, 4, Size-4, 4+bh)
		draw.Draw(img, r, image.NewUniform(fg), image.Point{}, draw.Src)
		d := font.Drawer{Dst: img, Src: image.NewUniform(bg), Face: f, Dot: fixed.P(r.Min.X+5, r.Min.Y+17)}
		d.DrawString(spec.Badge)
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		panic(err) // cannot fail writing to a bytes.Buffer
	}
	return buf.Bytes()
}

// fit returns the largest face (down to 10 px) in which s fits width px.
func fit(s string, isBold bool, maxSize, width int) font.Face {
	for size := maxSize; size > 10; size-- {
		f := face(isBold, size)
		if font.MeasureString(f, s).Ceil() <= width {
			return f
		}
	}
	return face(isBold, 10)
}

// clip shortens s with an ellipsis until it fits width.
func clip(f font.Face, s string, width int) string {
	if font.MeasureString(f, s).Ceil() <= width {
		return s
	}
	r := []rune(s)
	for len(r) > 1 {
		r = r[:len(r)-1]
		t := string(r) + "…"
		if font.MeasureString(f, t).Ceil() <= width {
			return t
		}
	}
	return string(r)
}
