package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mitchellh/go-homedir"
)

const (
	DirMode  = 0755
	FileMode = 0644
)

// expandTilde replaces a leading "~" with the user's home directory. The
// argument is returned unchanged when it doesn't start with a tilde or when the
// home directory can't be resolved.
func expandTilde(s string) string {
	if s != "~" && !strings.HasPrefix(s, "~/") {
		return s
	}
	expanded, err := homedir.Expand(s)
	if err != nil {
		errorLog(err, "Unable to expand '%s'", s)
		return s
	}
	return expanded
}

// expandExecutable resolves a bare command name against PATH. The candidate
// file names and the test for executability are platform specific: Windows
// resolves through PATHEXT, while Unix looks at the executable mode bits.
func expandExecutable(exe string) string {
	for _, base := range PATH {
		for _, name := range executableNames(exe) {
			cmd := filepath.Join(base, name)
			s, e := os.Stat(cmd)
			if e != nil || s.IsDir() {
				continue
			}
			if isExecutable(s) {
				return cmd
			}
		}
	}
	return exe
}

// executes a command.
func executeCommand(cmd string) error {
	args := SPACES.Split(cmd, -1)
	for i, arg := range args {
		args[i] = expandTilde(arg)
	}
	exe := expandExecutable(args[0])

	command := exec.Command(exe, args[1:]...)
	if err := command.Start(); err != nil {
		errorLogF("failed to execute '%s %s'", exe, args[1:])
		return err
	}
	pid := command.Process.Pid
	if err := containChildProcess(pid); err != nil {
		errorLog(err, "Unable to contain child process %d", pid)
	}
	return command.Process.Release()
}
