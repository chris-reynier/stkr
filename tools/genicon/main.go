// Command genicon writes the Windows program icon (a multi-size ICO) from the
// same drawing code as the tray icon, so no binary file is a source of truth.
//
//	go run ./tools/genicon packaging/windows/stkr.ico
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image/png"
	"os"

	"github.com/chris-reynier/stkr/internal/desktop"
)

// sizes are the icon sizes stored in the file (256 is the largest ICO allows).
var sizes = []int{16, 24, 32, 48, 64, 128, 256}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: genicon <out.ico>")
		os.Exit(2)
	}
	data, err := buildICO(sizes)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[1], data, 0o644); err != nil { //nolint:gosec // build artefact, not a secret
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// buildICO returns an ICO file with one PNG-compressed image per size.
func buildICO(sizes []int) ([]byte, error) {
	// A running project context: blue disc with the white dot.
	state := desktop.IconFor(&desktop.Status{Now: &desktop.NowStatus{Kind: "project"}})
	pngs := make([][]byte, len(sizes))
	for i, s := range sizes {
		var b bytes.Buffer
		if err := png.Encode(&b, desktop.DrawIcon(state, s)); err != nil {
			return nil, err
		}
		pngs[i] = b.Bytes()
	}
	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		dim := byte(s)
		if s >= 256 {
			dim = 0 // 0 means 256 in an ICO directory
		}
		out.Write([]byte{dim, dim, 0, 0})
		_ = binary.Write(&out, binary.LittleEndian, [2]uint16{1, 32}) // planes, bits per pixel
		_ = binary.Write(&out, binary.LittleEndian, uint32(len(pngs[i])))
		_ = binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(pngs[i])
	}
	for _, p := range pngs {
		out.Write(p)
	}
	return out.Bytes(), nil
}
