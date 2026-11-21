package hotkey

import "testing"

func TestParse(t *testing.T) {
	tests := []struct {
		in   string
		want Key
		str  string
	}{
		{"", Key{}, ""},
		{"   ", Key{}, ""},
		{"Ctrl+Alt+Space", Key{ModCtrl | ModAlt, 0x20}, "Ctrl+Alt+Space"},
		{"ctrl + alt + space", Key{ModCtrl | ModAlt, 0x20}, "Ctrl+Alt+Space"},
		{"CONTROL+SHIFT+k", Key{ModCtrl | ModShift, 'K'}, "Ctrl+Shift+K"},
		{"Win+Shift+9", Key{ModWin | ModShift, '9'}, "Shift+Win+9"},
		{"Ctrl+Alt+Shift+Win+F12", Key{ModCtrl | ModAlt | ModShift | ModWin, 0x7B}, "Ctrl+Alt+Shift+Win+F12"},
		{"Alt+F1", Key{ModAlt, 0x70}, "Alt+F1"},
		{"Alt+f24", Key{ModAlt, 0x87}, "Alt+F24"},
		{"Ctrl+PgUp", Key{ModCtrl, 0x21}, "Ctrl+PageUp"},
		{"Super+Enter", Key{ModWin, 0x0D}, "Win+Enter"},
		{"Ctrl+Esc", Key{ModCtrl, 0x1B}, "Ctrl+Esc"},
		{"Ctrl+Down", Key{ModCtrl, 0x28}, "Ctrl+Down"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := Parse(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("Parse(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
			if got.String() != tt.str {
				t.Errorf("String() = %q, want %q", got.String(), tt.str)
			}
			// The canonical text parses back to the same key.
			if back, err := Parse(got.String()); err != nil || back != got {
				t.Errorf("round trip of %q: %+v, %v", got.String(), back, err)
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	for _, in := range []string{
		"Space",           // no modifier
		"F5",              // no modifier
		"Ctrl+",           // empty part
		"Ctrl",            // no key
		"Ctrl+Alt",        // no key
		"Ctrl+Ctrl+A",     // repeated modifier
		"Ctrl+A+B",        // two keys
		"A+Ctrl",          // key before modifier
		"Ctrl+Banana",     // unknown key
		"Ctrl+F25",        // out of range
		"Ctrl+F0",         // out of range
		"Ctrl+Fx",         // not a function key
		"Ctrl++A",         // empty part
		"Ctrl+Alt+Space+", // trailing +
	} {
		if k, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %+v, want an error", in, k)
		}
	}
}

func TestZero(t *testing.T) {
	if !(Key{}).IsZero() || (Key{ModAlt, 'A'}).IsZero() {
		t.Fatal("IsZero")
	}
	if (Key{ModAlt, 0xFF}).String() != "Alt+0xFF" {
		t.Fatalf("unknown vk string = %q", (Key{ModAlt, 0xFF}).String())
	}
}
