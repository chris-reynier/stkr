package desktop

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestInstanceName(t *testing.T) {
	re := regexp.MustCompile(`^stkr-desktop-[0-9a-f]{16}$`)
	base := t.TempDir()
	a := filepath.Join(base, "Home")
	if !re.MatchString(InstanceName(a)) {
		t.Fatalf("bad name %q", InstanceName(a))
	}
	same := []string{a + string(filepath.Separator), filepath.Join(base, "x", "..", "Home")}
	for _, p := range same {
		if instanceName(p, false) != instanceName(a, false) {
			t.Errorf("%q and %q differ", p, a)
		}
	}
	if instanceName(filepath.Join(base, "other"), false) == instanceName(a, false) {
		t.Error("different folders share a name")
	}
	lower := filepath.Join(base, "home")
	if instanceName(lower, true) != instanceName(a, true) {
		t.Error("case folding: names differ")
	}
	if instanceName(lower, false) == instanceName(a, false) {
		t.Error("without case folding: names equal")
	}
	// Slash style does not matter.
	if instanceName(`C:\Users\x\.stkr`, true) != instanceName("C:/Users/x/.stkr", true) && filepath.Separator == '\\' {
		t.Error("slash style changes the name")
	}
	if m := MutexName(a); !strings.HasPrefix(m, `Local\stkr-desktop-`) || !strings.HasSuffix(m, InstanceName(a)) {
		t.Errorf("mutex %q", m)
	}
}
