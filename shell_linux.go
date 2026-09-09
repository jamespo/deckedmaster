//go:build linux

package main

import "os/exec"

// shellCommand builds the command a command widget's script is run through.
func shellCommand(command string) *exec.Cmd {
	return exec.Command("sh", "-c", command)
}
