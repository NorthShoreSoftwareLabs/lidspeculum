//go:build windows

package main

import (
	"errors"
	"syscall"
	"unsafe"
)

// errNotLidspeculum exists for parity with the unix build (commands.go compares
// against it). On Windows stopHolder disengages the flag directly and never
// signals an arbitrary pid, so it never returns this error.
var errNotLidspeculum = errors.New("recorded pid is not a lidspeculum process")

// Windows process liveness via kernel32, using syscall.NewLazyDLL so we keep a
// zero-dependency build (no golang.org/x/sys).
var (
	kernel32          = syscall.NewLazyDLL("kernel32.dll")
	procOpenProcess   = kernel32.NewProc("OpenProcess")
	procGetExitCode   = kernel32.NewProc("GetExitCodeProcess")
	procCloseHandle   = kernel32.NewProc("CloseHandle")
	procWaitForSingle = kernel32.NewProc("WaitForSingleObject")
)

const (
	synchronize             = 0x00100000 // SYNCHRONIZE: required by WaitForSingleObject
	processQueryLimitedInfo = 0x1000     // PROCESS_QUERY_LIMITED_INFORMATION
	stillActive             = 259        // STILL_ACTIVE
	waitObject0             = 0          // WAIT_OBJECT_0: handle signaled (process exited)
	waitTimeout             = 0x00000102 // WAIT_TIMEOUT: still running
)

// processAlive reports whether a process with the given pid is currently alive.
//
// GetExitCodeProcess returns STILL_ACTIVE (259) for a live process, but ALSO
// for a process that genuinely exited with code 259, so it can't be trusted
// alone. We first wait on the process handle with a zero timeout, which is why
// the handle is opened with SYNCHRONIZE (without it WaitForSingleObject returns
// WAIT_FAILED and we would misreport a live process as dead):
//
//   - WAIT_OBJECT_0 (0): the process object is signaled (already exited)
//     regardless of its exit code => dead.
//   - WAIT_TIMEOUT (0x102) + GetExitCodeProcess == STILL_ACTIVE => alive.
//   - WAIT_FAILED (or any other result): the wait is unreliable, so fall back
//     conservatively to GetExitCodeProcess alone (alive only if STILL_ACTIVE,
//     else dead) rather than misreporting.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, _, _ := procOpenProcess.Call(uintptr(synchronize|processQueryLimitedInfo), 0, uintptr(pid))
	if h == 0 {
		return false
	}
	defer procCloseHandle.Call(h)

	w, _, _ := procWaitForSingle.Call(h, 0)
	if w == waitObject0 {
		// The process has exited regardless of its exit code.
		return false
	}

	var code uint32
	r, _, _ := procGetExitCode.Call(h, uintptr(unsafe.Pointer(&code)))
	if r == 0 {
		return false
	}
	if w == waitTimeout {
		return code == stillActive
	}
	// WAIT_FAILED (0xFFFFFFFF) or any unexpected result: the wait can't be
	// trusted. Fall back to the exit code alone, treating the process as alive
	// only if it reports STILL_ACTIVE.
	return code == stillActive
}

// stopHolder makes the machine sleepable again on Windows.
//
// Windows nuance: there is no portable way to deliver SIGTERM to an unrelated
// process the way unix does, so we can't ask the holder to run its own clean
// release. Because lidspeculum is single-hold, stop instead disengages the OS
// flag (restoring the saved prior powercfg indices) and removes the pidfile
// directly. The orphaned holder process keeps running until its own deadline or
// until the user closes it; when it next exits it will try to disengage again,
// which is harmless (the flag is already restored). The net effect the user
// wants -- the machine sleeps normally -- is achieved immediately.
func stopHolder(pid int) error {
	if err := disengage(); err != nil {
		return err
	}
	return removePidfile()
}
