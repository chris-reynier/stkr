package main

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"testing"
)

func TestBuildICO(t *testing.T) {
	data, err := buildICO(sizes)
	if err != nil {
		t.Fatal(err)
	}
	if binary.LittleEndian.Uint16(data[2:]) != 1 || int(binary.LittleEndian.Uint16(data[4:])) != len(sizes) {
		t.Fatalf("bad header % x", data[:6])
	}
	for i, want := range sizes {
		e := data[6+16*i:]
		n := binary.LittleEndian.Uint32(e[8:])
		off := binary.LittleEndian.Uint32(e[12:])
		img, err := png.Decode(bytes.NewReader(data[off : off+n]))
		if err != nil {
			t.Fatalf("size %d: %v", want, err)
		}
		if img.Bounds().Dx() != want {
			t.Fatalf("entry %d is %d px, want %d", i, img.Bounds().Dx(), want)
		}
	}
}
