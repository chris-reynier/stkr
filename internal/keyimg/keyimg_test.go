package keyimg

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"
)

func decode(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestRenderSizeAndBackground(t *testing.T) {
	bg := color.RGBA{30, 90, 200, 255}
	img := decode(t, Render(KeySpec{Background: bg, Lines: []string{"Acme", "1h25m", "today 3h"}, Badge: "!"}))
	if b := img.Bounds(); b.Dx() != 144 || b.Dy() != 144 {
		t.Fatalf("size = %v", b)
	}
	// Corner pixels away from text and badge carry the background.
	for _, p := range []image.Point{{1, 1}, {1, 142}, {142, 142}, {72, 142}} {
		r, g, b, a := img.At(p.X, p.Y).RGBA()
		if uint8(r>>8) != bg.R || uint8(g>>8) != bg.G || uint8(b>>8) != bg.B || a>>8 != 255 {
			t.Errorf("pixel %v = %d,%d,%d,%d", p, r>>8, g>>8, b>>8, a>>8)
		}
	}
	// Some pixel in the text band is white (text drawn).
	found := false
	for y := 40; y < 100 && !found; y++ {
		for x := 8; x < 136; x++ {
			if r, g, b, _ := img.At(x, y).RGBA(); r>>8 > 200 && g>>8 > 200 && b>>8 > 200 {
				found = true
				break
			}
		}
	}
	if !found {
		t.Error("no text pixels found")
	}
}

func TestRenderAutoFitAndClip(t *testing.T) {
	long := strings.Repeat("W", 60)
	img := decode(t, Render(KeySpec{Background: color.RGBA{0, 0, 0, 255}, Lines: []string{long, long, long, "ignored"}}))
	if img.Bounds().Dx() != 144 {
		t.Fatal("bad size")
	}
	// Margins stay clear of text.
	for y := 0; y < 144; y++ {
		for _, x := range []int{0, 1, 2, 3, 140, 141, 142, 143} {
			if r, _, _, _ := img.At(x, y).RGBA(); r != 0 {
				t.Fatalf("text in margin at %d,%d", x, y)
			}
		}
	}
}

func TestRenderEmptyAndDeterministic(t *testing.T) {
	spec := KeySpec{Background: color.RGBA{1, 2, 3, 255}}
	a, b := Render(spec), Render(spec)
	if !bytes.Equal(a, b) {
		t.Fatal("not deterministic")
	}
	decode(t, a)
	if bytes.Equal(a, Render(KeySpec{Background: color.RGBA{9, 2, 3, 255}})) {
		t.Fatal("different specs share an image")
	}
}

func TestCacheBounded(t *testing.T) {
	for i := 0; i < maxCached+20; i++ {
		Render(KeySpec{Background: color.RGBA{uint8(i), uint8(i >> 8), 7, 255}, Lines: []string{string(rune('a' + i%26)), string(rune(i + 100))}})
	}
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cacheN > maxCached {
		t.Fatalf("cacheN = %d", cacheN)
	}
}

func TestRenderSpeed(t *testing.T) {
	if testing.Short() {
		t.Skip("perf")
	}
	Render(KeySpec{Lines: []string{"warm"}}) // load fonts
	start := time.Now()
	for i := 0; i < 20; i++ {
		draw1(KeySpec{Background: color.RGBA{10, 10, 10, 255}, Lines: []string{"Acme project", "1h25m", "today 3h10m"}, Badge: "!"})
	}
	if per := time.Since(start) / 20; per > 50*time.Millisecond {
		t.Fatalf("uncached render took %v", per)
	}
}

func BenchmarkRender(b *testing.B) {
	for i := 0; i < b.N; i++ {
		draw1(KeySpec{Background: color.RGBA{10, 10, 10, 255}, Lines: []string{"Acme project", "1h25m", "today 3h10m"}})
	}
}
