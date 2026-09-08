//go:build windows

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// defaultPathExt is used when PATHEXT is unset, and matches the value Windows
// ships with minus the scripting-host entries.
const defaultPathExt = ".COM;.EXE;.BAT;.CMD"

// containChildProcess is a no-op. The Windows equivalent of the cgroup this
// port's counterpart uses is a Job Object created with
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE, which would have to be created once and
// have each child assigned to it. Until then children outlive the daemon,
// which is how command actions behaved before cgroups were introduced.
//
//nolint:unparam,revive // signature is fixed by the Unix port
func containChildProcess(pid int) error {
	return nil
}

// executableNames returns the candidate file names for a bare command, trying
// each PATHEXT extension in turn. A name that already carries one of those
// extensions is used as-is.
func executableNames(exe string) []string {
	pathExt := os.Getenv("PATHEXT")
	if pathExt == "" {
		pathExt = defaultPathExt
	}
	exts := filepath.SplitList(pathExt)

	if ext := filepath.Ext(exe); ext != "" {
		for _, e := range exts {
			if strings.EqualFold(ext, e) {
				return []string{exe}
			}
		}
	}

	names := make([]string, 0, len(exts)+1)
	names = append(names, exe)
	for _, e := range exts {
		names = append(names, exe+strings.ToLower(e))
	}
	return names
}

// isExecutable reports true for any regular file. Windows has no executable
// mode bit; executableNames has already restricted candidates to PATHEXT.
func isExecutable(s fs.FileInfo) bool {
	return s.Mode().IsRegular()
}
