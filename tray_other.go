//go:build !windows

package main

// startTray is a no-op on this platform: the system tray icon is currently
// Windows-only, so -tray isn't even a recognized flag here.
func startTray() {}

// stopTray is a no-op on this platform, matching startTray.
func stopTray() {}
