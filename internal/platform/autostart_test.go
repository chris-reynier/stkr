package platform

import (
	"errors"
	"testing"

	"github.com/chris-reynier/stkr/internal/domain"
)

type fakeRunKey struct {
	vals    map[string]string
	failSet bool
}

func (f *fakeRunKey) Get(n string) (string, bool, error) { v, ok := f.vals[n]; return v, ok, nil }
func (f *fakeRunKey) Set(n, v string) error {
	if f.failSet {
		return errors.New("denied")
	}
	f.vals[n] = v
	return nil
}
func (f *fakeRunKey) Delete(n string) error { delete(f.vals, n); return nil }

func TestAutostartWritesAndRemovesTheRunValue(t *testing.T) {
	key := &fakeRunKey{vals: map[string]string{"Other": "keep"}}
	a := NewAutostartWithKey(key, `C:\Users\me\AppData\Local\Programs\STKR\stkrw.exe`)
	if on, err := a.Enabled(); err != nil || on {
		t.Fatalf("enabled = %v, %v", on, err)
	}
	if err := a.Set(true); err != nil {
		t.Fatal(err)
	}
	want := `"C:\Users\me\AppData\Local\Programs\STKR\stkrw.exe" --autostart`
	if key.vals["STKR"] != want {
		t.Errorf("value = %q, want %q", key.vals["STKR"], want)
	}
	if on, _ := a.Enabled(); !on {
		t.Error("not enabled after Set(true)")
	}
	// An entry that points elsewhere is repaired.
	key.vals["STKR"] = `"D:\old\stkrw.exe" --autostart`
	if err := a.Set(true); err != nil || key.vals["STKR"] != want {
		t.Errorf("repair: %q, %v", key.vals["STKR"], err)
	}
	if err := a.Set(false); err != nil {
		t.Fatal(err)
	}
	if _, ok := key.vals["STKR"]; ok {
		t.Error("value still there")
	}
	if key.vals["Other"] != "keep" {
		t.Error("another value was touched")
	}
	// Turning off twice is fine.
	if err := a.Set(false); err != nil {
		t.Error(err)
	}
}

func TestAutostartRefusesBadPathAndPropagatesErrors(t *testing.T) {
	key := &fakeRunKey{vals: map[string]string{}}
	for _, exe := range []string{`C:\a"b\stkrw.exe`, "x\ny"} {
		if err := NewAutostartWithKey(key, exe).Set(true); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%q: err = %v", exe, err)
		}
	}
	key.failSet = true
	if err := NewAutostartWithKey(key, `C:\x\stkrw.exe`).Set(true); err == nil {
		t.Error("registry error swallowed")
	}
}

func TestUnavailableKey(t *testing.T) {
	a := NewAutostartWithKey(unavailableKey{}, "x")
	if _, err := a.Enabled(); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("enabled: %v", err)
	}
	if err := a.Set(false); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("set: %v", err)
	}
}

func TestAutostartWithoutDesktopProgram(t *testing.T) {
	key := &fakeRunKey{vals: map[string]string{}}
	if err := NewAutostartWithKey(key, "").Set(true); !errors.Is(err, domain.ErrUnavailable) {
		t.Errorf("err = %v", err)
	}
	// Turning it off needs no program.
	key.vals["STKR"] = "x"
	if err := NewAutostartWithKey(key, "").Set(false); err != nil || len(key.vals) != 0 {
		t.Errorf("off: %v %v", err, key.vals)
	}
}
