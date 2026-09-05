package main

import (
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mitchellh/go-homedir"
)

const (
	DirMode           = 0755
	FileMode          = 0644
	cgroupProcsFile   = "cgroup.procs"
	processCGroupFile = "/proc/self/cgroup"
	baseCGroupPath    = "/sys/fs/cgroup"
)

// childProcessCGroup resolves the control group child processes are spawned in.
// It is resolved on first use rather than at startup, and returns an empty path
// when this system does not let us create one (no cgroup v2, insufficient
// permissions, containers, ...), in which case children simply stay in our own
// control group.
var childProcessCGroup = sync.OnceValue(func() string {
	return createNewCGroup("deckedmaster.scope")
})

func runningCGroup() (string, error) {
	s, err := os.ReadFile(processCGroupFile)
	if err != nil {
		return "", err
	}
	split := strings.Split(string(s), ":")
	cgroup := split[len(split)-1]
	return strings.TrimSpace(cgroup), nil
}

func createNewCGroup(name string) string {
	current, err := runningCGroup()
	if err != nil {
		errorLog(err, "Unable to read the control group for the current process")
		return ""
	}
	cgroupPath := path.Join(baseCGroupPath, filepath.Dir(current), name)
	if err := os.MkdirAll(cgroupPath, DirMode); err != nil {
		errorLog(err, "Unable to create control group %s for child processes", cgroupPath)
		return ""
	}
	verboseLog("Using control group to spawn child processes\n\t%s", cgroupPath)
	return cgroupPath
}

func moveProcessToCGroup(pid int, cgroup string) error {
	if cgroup == "" {
		return nil
	}
	cgroupFile := path.Join(cgroup, cgroupProcsFile)
	fileContents := fmt.Sprintf("%d\n", pid)
	return os.WriteFile(cgroupFile, []byte(fileContents), FileMode)
}

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

func expandExecutable(exe string) string {
	for _, base := range PATH {
		cmd := filepath.Join(base, exe)
		s, e := os.Stat(cmd)
		if e != nil || s.IsDir() {
			continue
		}
		fileMode := s.Mode()
		if fileMode&0111 != 0 {
			return cmd
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
	if err := moveProcessToCGroup(pid, childProcessCGroup()); err != nil {
		errorLog(err, "Unable to move child process %d to cgroup", pid)
	}
	return command.Process.Release()
}
