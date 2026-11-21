//go:build !windows

package platform

func osRunKey() RunKey { return unavailableKey{} }
