package platform

import (
	"fmt"
	"strings"

	"github.com/chris-reynier/stkr/internal/domain"
)

// AutostartValue is the name of the sign-in entry under the HKCU Run key. The
// Windows setup writes the same value (contract install-update.md).
const AutostartValue = "STKR"

// RunKey is the per-user "start when I sign in" registry key. The real one is
// Windows only; tests use a fake.
type RunKey interface {
	// Get returns the value, ok=false when it does not exist.
	Get(name string) (value string, ok bool, err error)
	Set(name, value string) error
	// Delete removes the value; a missing value is not an error.
	Delete(name string) error
}

// Autostarter switches "start STKR when I sign in" on and off.
type Autostarter interface {
	Enabled() (bool, error)
	Set(on bool) error
}

// Autostart is the Autostarter that writes the Run value STKR.
type Autostart struct {
	key RunKey
	exe string
}

// NewAutostart returns an Autostart for the desktop program exe on the real
// Run key (non-Windows systems report domain.ErrUnavailable).
func NewAutostart(exe string) *Autostart { return &Autostart{key: osRunKey(), exe: exe} }

// NewAutostartWithKey is NewAutostart on a given key (tests).
func NewAutostartWithKey(key RunKey, exe string) *Autostart { return &Autostart{key: key, exe: exe} }

// Command is the Run value: the quoted program path and the --autostart marker.
func (a *Autostart) Command() string { return `"` + a.exe + `" --autostart` }

// Enabled reports whether the sign-in entry exists.
func (a *Autostart) Enabled() (bool, error) {
	_, ok, err := a.key.Get(AutostartValue)
	return ok, err
}

// Set writes (on) or removes (off) the sign-in entry. Turning it on rewrites
// the value, so an entry that points to an old folder is repaired.
func (a *Autostart) Set(on bool) error {
	if !on {
		return a.key.Delete(AutostartValue)
	}
	if strings.TrimSpace(a.exe) == "" {
		return domain.New(domain.ErrUnavailable, "the desktop program (stkrw.exe) was not found next to this program")
	}
	if strings.ContainsAny(a.exe, "\"\r\n") {
		return domain.New(domain.ErrInvalid, "the program path for the sign-in entry is not valid")
	}
	return a.key.Set(AutostartValue, a.Command())
}

// unavailableKey is the Run key on systems that have none.
type unavailableKey struct{}

func (unavailableKey) err() error {
	return fmt.Errorf("%w: starting at sign-in is only supported on Windows", domain.ErrUnavailable)
}
func (k unavailableKey) Get(string) (string, bool, error) { return "", false, k.err() }
func (k unavailableKey) Set(string, string) error         { return k.err() }
func (k unavailableKey) Delete(string) error              { return k.err() }
