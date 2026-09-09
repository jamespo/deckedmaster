//go:build !windows

package main

// checkHIDSupport is a no-op on this platform. It exists on Windows because
// CGO_ENABLED=0 there silently produces a binary that can never find a
// device; the primary Linux build has no such trap.
func checkHIDSupport() {}
