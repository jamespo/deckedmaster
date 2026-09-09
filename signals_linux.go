//go:build linux

package main

import (
	"os"
	"os/signal"
	"syscall"
)

// notifyReload returns the channel that carries configuration reload requests,
// which are signalled with SIGHUP.
func notifyReload() <-chan os.Signal {
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	return hup
}

// reapChildProcesses waits on children spawned by command actions so they do
// not linger as zombies. Widgets start processes and release them immediately,
// which leaves us as their parent and therefore responsible for reaping them.
func reapChildProcesses() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGCHLD)

	for range sigs {
		for {
			var status syscall.WaitStatus
			pid, err := syscall.Wait4(-1, &status, syscall.WNOHANG, nil)
			if pid <= 0 || err != nil {
				break
			}
			verboseLog("reaped pid=%d status=%d", pid, status.ExitStatus())
		}
	}
}
