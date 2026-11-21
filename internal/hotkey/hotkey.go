// Package hotkey parses global hotkey descriptions such as
// "Ctrl+Alt+Space" into the modifier mask and virtual-key code that the
// Windows RegisterHotKey call needs. It is pure Go so config validation and
// tests run on every platform.
package hotkey

import (
	"fmt"
	"strings"
)

// Modifier bits, equal to the MOD_* values of RegisterHotKey.
const (
	ModAlt   uint32 = 0x1
	ModCtrl  uint32 = 0x2
	ModShift uint32 = 0x4
	ModWin   uint32 = 0x8
)

// Key is a parsed hotkey. The zero Key means "no hotkey".
type Key struct {
	Mods uint32
	VK   uint32
}

// IsZero reports whether k is the empty (disabled) hotkey.
func (k Key) IsZero() bool { return k == Key{} }

var modNames = map[string]uint32{
	"ctrl": ModCtrl, "control": ModCtrl,
	"alt":   ModAlt,
	"shift": ModShift,
	"win":   ModWin, "windows": ModWin, "super": ModWin, "meta": ModWin,
}

// named keys (virtual-key codes) besides A-Z, 0-9 and F1-F24.
var keyNames = map[string]uint32{
	"space": 0x20, "enter": 0x0D, "return": 0x0D, "tab": 0x09,
	"esc": 0x1B, "escape": 0x1B, "backspace": 0x08,
	"delete": 0x2E, "del": 0x2E, "insert": 0x2D, "ins": 0x2D,
	"home": 0x24, "end": 0x23,
	"pageup": 0x21, "pgup": 0x21, "pagedown": 0x22, "pgdn": 0x22,
	"left": 0x25, "up": 0x26, "right": 0x27, "down": 0x28,
}

// Parse reads "Ctrl+Alt+Shift+Win+<Key>". Names are case-insensitive and
// spaces around "+" are ignored. At least one modifier is required, so a
// hotkey can never steal an ordinary key. Empty text gives the zero Key.
func Parse(s string) (Key, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Key{}, nil
	}
	parts := strings.Split(s, "+")
	var k Key
	haveKey := false
	for _, raw := range parts {
		p := strings.ToLower(strings.TrimSpace(raw))
		if p == "" {
			return Key{}, fmt.Errorf("hotkey %q has an empty part", s)
		}
		if m, ok := modNames[p]; ok {
			if k.Mods&m != 0 {
				return Key{}, fmt.Errorf("hotkey %q repeats %s", s, raw)
			}
			if haveKey {
				return Key{}, fmt.Errorf("hotkey %q: the key must come last", s)
			}
			k.Mods |= m
			continue
		}
		if haveKey {
			return Key{}, fmt.Errorf("hotkey %q has more than one key", s)
		}
		vk, ok := keyCode(p)
		if !ok {
			return Key{}, fmt.Errorf("hotkey %q: unknown key %q", s, strings.TrimSpace(raw))
		}
		k.VK, haveKey = vk, true
	}
	if !haveKey {
		return Key{}, fmt.Errorf("hotkey %q has no key (example: Ctrl+Alt+Space)", s)
	}
	if k.Mods == 0 {
		return Key{}, fmt.Errorf("hotkey %q needs at least one of Ctrl, Alt, Shift or Win", s)
	}
	return k, nil
}

func keyCode(p string) (uint32, bool) {
	if len(p) == 1 {
		c := p[0]
		switch {
		case c >= 'a' && c <= 'z':
			return uint32(c-'a') + 0x41, true
		case c >= '0' && c <= '9':
			return uint32(c-'0') + 0x30, true
		}
	}
	if vk, ok := keyNames[p]; ok {
		return vk, true
	}
	if p[0] == 'f' && len(p) <= 3 {
		n := 0
		for _, c := range p[1:] {
			if c < '0' || c > '9' {
				return 0, false
			}
			n = n*10 + int(c-'0')
		}
		if n >= 1 && n <= 24 {
			return 0x70 + uint32(n-1), true
		}
	}
	return 0, false
}

var vkNames = func() map[uint32]string {
	m := map[uint32]string{
		0x20: "Space", 0x0D: "Enter", 0x09: "Tab", 0x1B: "Esc", 0x08: "Backspace",
		0x2E: "Delete", 0x2D: "Insert", 0x24: "Home", 0x23: "End",
		0x21: "PageUp", 0x22: "PageDown", 0x25: "Left", 0x26: "Up", 0x27: "Right", 0x28: "Down",
	}
	return m
}()

// String is the canonical text of k, for example "Ctrl+Alt+Space". The zero
// Key is "".
func (k Key) String() string {
	if k.IsZero() {
		return ""
	}
	var parts []string
	for _, m := range []struct {
		bit  uint32
		name string
	}{{ModCtrl, "Ctrl"}, {ModAlt, "Alt"}, {ModShift, "Shift"}, {ModWin, "Win"}} {
		if k.Mods&m.bit != 0 {
			parts = append(parts, m.name)
		}
	}
	switch {
	case k.VK >= 0x41 && k.VK <= 0x5A, k.VK >= 0x30 && k.VK <= 0x39:
		parts = append(parts, string(rune(k.VK)))
	case k.VK >= 0x70 && k.VK <= 0x87:
		parts = append(parts, fmt.Sprintf("F%d", k.VK-0x70+1))
	default:
		if n, ok := vkNames[k.VK]; ok {
			parts = append(parts, n)
		} else {
			parts = append(parts, fmt.Sprintf("0x%02X", k.VK))
		}
	}
	return strings.Join(parts, "+")
}
