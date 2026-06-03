//go:build !windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

// processAlive reports whether a process with the given pid is currently alive.
// Signal 0 performs error checking without sending a signal: ESRCH means no such
// process (dead), EPERM means the process exists but we can't signal it (alive).
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return err == syscall.EPERM
}

// errNotLidspeculum signals that the pid recorded in the pidfile does not belong
// to a lidspeculum process, so we refuse to signal it. The pidfile is a
// user-writable JSON file and pids get reused, so we must confirm the target's
// identity before delivering SIGTERM to avoid killing an unrelated same-user
// process.
var errNotLidspeculum = errors.New("recorded pid is not a lidspeculum process")

// procComm returns the executable command name (basename, no path) of pid. On
// Linux it reads /proc/<pid>/comm directly; elsewhere it shells out to
// `ps -p <pid> -o comm=`. The returned name is trimmed of whitespace.
func procComm(pid int) (string, error) {
	if runtime.GOOS == "linux" {
		data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return "", err
	}
	// `ps -o comm=` may print a full path (e.g. on macOS); take the basename.
	return filepath.Base(strings.TrimSpace(string(out))), nil
}

// isLidspeculum reports whether pid's command name is "lidspeculum".
func isLidspeculum(pid int) bool {
	name, err := procComm(pid)
	if err != nil {
		return false
	}
	return filepath.Base(name) == "lidspeculum"
}

// stopHolder asks the holder process to release by sending SIGTERM. The holder's
// signal handler runs its clean release (disengage + remove pidfile). It first
// verifies the target pid is actually a lidspeculum process; if not, it refuses
// to signal and returns errNotLidspeculum so the caller falls through to
// stranded-flag handling instead of killing an unrelated process.
func stopHolder(pid int) error {
	if !isLidspeculum(pid) {
		return errNotLidspeculum
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}
