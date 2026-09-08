//go:build windows

package main

import "os/exec"

// shellCommand builds the command a command widget's script is run through.
// Note that deck configs written for the Unix ports will mostly need their
// command strings rewritten: cmd.exe is not a POSIX shell.
func shellCommand(command string) *exec.Cmd {
	return exec.Command("cmd", "/c", command)
}
