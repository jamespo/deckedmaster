//go:build windows

package main

import (
	_ "embed"
	"flag"

	"fyne.io/systray"
)

//go:embed assets/tray/icon.ico
var trayIcon []byte

var trayConfig = flag.Bool("tray", false, "show a system tray icon with an Exit option (Windows only)")

// startTray shows a system tray icon when -tray was passed. systray.Run
// blocks until Quit is called, so it gets its own goroutine alongside
// eventLoop; stopTray is what unblocks it on any other shutdown path.
func startTray() {
	if !*trayConfig {
		return
	}
	go systray.Run(onTrayReady, func() {})
}

// stopTray removes the tray icon on shutdown. Calling Quit when the tray was
// never started is a safe no-op.
func stopTray() {
	if !*trayConfig {
		return
	}
	systray.Quit()
}

func onTrayReady() {
	systray.SetIcon(trayIcon)
	systray.SetTooltip("deckedmaster")

	mExit := systray.AddMenuItem("Exit", "Quit deckedmaster")
	go func() {
		<-mExit.ClickedCh
		fatal(nil)
	}()
}
