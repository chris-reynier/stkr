//go:build !windows

package desktop

import "github.com/chris-reynier/stkr/internal/domain"

const platformSupported = false

func newPlatformTray(string) (Tray, error) {
	return nil, domain.New(domain.ErrUnavailable, UnsupportedMessage)
}

func platformAcquireInstance(string) (func(), bool, error) { return func() {}, false, nil }

func platformSignalInstance(string) error {
	return domain.New(domain.ErrUnavailable, UnsupportedMessage)
}

func platformOpen(string) error { return domain.New(domain.ErrUnavailable, UnsupportedMessage) }
