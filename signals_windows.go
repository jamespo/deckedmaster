//go:build windows

package main

import "os"

// notifyReload returns a nil channel, which blocks forever in a select.
// Windows has no SIGHUP, so configuration is reloaded through the REST API or
// by restarting. Deck files are also watched for changes at runtime.
func notifyReload() <-chan os.Signal {
	return nil
}

// reapChildProcesses does nothing. Windows has no zombie processes: a child
// is destroyed once every handle to it is closed, and os/exec closes ours.
func reapChildProcesses() {}
