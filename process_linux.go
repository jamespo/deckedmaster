//go:build linux

package main

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

const (
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

// containChildProcess moves a spawned process into our own control group, so
// that it is cleaned up with us rather than outliving the daemon.
func containChildProcess(pid int) error {
	cgroup := childProcessCGroup()
	if cgroup == "" {
		return nil
	}
	cgroupFile := path.Join(cgroup, cgroupProcsFile)
	fileContents := fmt.Sprintf("%d\n", pid)
	return os.WriteFile(cgroupFile, []byte(fileContents), FileMode)
}

// executableNames returns the single name to look for: Unix does not decorate
// executables with an extension.
func executableNames(exe string) []string {
	return []string{exe}
}

// isExecutable reports whether any of the executable mode bits are set.
func isExecutable(s fs.FileInfo) bool {
	return s.Mode()&0111 != 0
}
