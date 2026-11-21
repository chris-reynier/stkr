//go:build !windows

package desktop

import "log/slog"

// platformWindowsFactory: there are no windows outside Windows.
func platformWindowsFactory(string, bool, *slog.Logger) WindowsFactory { return nil }
