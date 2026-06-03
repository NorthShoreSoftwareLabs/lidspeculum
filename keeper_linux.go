//go:build linux

package main

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Linux (systemd-logind): there is no persistent OS flag to flip. Instead, a
// hold is kept alive by an inhibitor lock taken with `systemd-inhibit
// --what=handle-lid-switch:sleep`. The lock lives exactly as long as the
// systemd-inhibit child process, and needs no root.
//
// We get the resident behavior by RE-EXECING ourselves under systemd-inhibit:
// the first invocation (env LIDSPECULUM_INHIBITED unset) replaces itself with
// `systemd-inhibit ... <self> <same args>` carrying LIDSPECULUM_INHIBITED=1.
// The inner process sees the env, skips re-exec, writes the pidfile, and blocks
// (hold) or runs the command (run). When the inner process exits, systemd-inhibit
// releases the lock automatically. engage/disengage are therefore no-ops on
// Linux.

// inhibitedEnv marks the re-exec'd inner process. NOTE: LIDSPECULUM_INHIBITED is
// a RESERVED internal variable. If it is set to "1" in the ambient environment
// before launch, the first pass will skip its re-exec under systemd-inhibit and
// run without taking the lock. This is a deliberately accepted edge (the var is
// documented as reserved); callers must not set it themselves.
const inhibitedEnv = "LIDSPECULUM_INHIBITED"

var errNoInhibit = errors.New("lidspeculum needs systemd-inhibit (systemd-logind); not found on this system.")

// stateBaseDir returns the base directory for lidspeculum state on Linux:
// $XDG_STATE_HOME, falling back to ~/.local/state.
func stateBaseDir() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return x, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home + "/.local/state", nil
}

// preflightKeeper ensures systemd-inhibit exists before we promise anything.
func preflightKeeper() error {
	if _, err := exec.LookPath("systemd-inhibit"); err != nil {
		return errNoInhibit
	}
	return nil
}

// maybeReexec replaces this process with one running under systemd-inhibit,
// unless we are already the inhibited inner process. It returns (true, nil) only
// on an error path that can't reach Exec; on success syscall.Exec never returns.
func maybeReexec() (bool, error) {
	if os.Getenv(inhibitedEnv) == "1" {
		return false, nil // already inside the inhibitor; continue here
	}
	if err := preflightKeeper(); err != nil {
		return false, err
	}
	self, err := os.Executable()
	if err != nil {
		return false, err
	}
	inhibit, err := exec.LookPath("systemd-inhibit")
	if err != nil {
		return false, errNoInhibit
	}

	// Build: systemd-inhibit <opts> <self> <original args after program name>
	argv := []string{
		"systemd-inhibit",
		"--what=handle-lid-switch:sleep",
		"--who=lidspeculum",
		"--why=lidspeculum hold",
		"--mode=block",
		self,
	}
	argv = append(argv, os.Args[1:]...)

	env := append(os.Environ(), inhibitedEnv+"=1")
	// syscall.Exec replaces this image; on success it does not return.
	if err := syscall.Exec(inhibit, argv, env); err != nil {
		return false, err
	}
	return true, nil // unreachable on success
}

// announceElevation is a no-op on Linux: systemd-inhibit needs no elevation.
func announceElevation(quiet bool) {}

// engage / disengage are no-ops on Linux. The inhibitor lock is held by the
// outer systemd-inhibit process for the inner process's whole lifetime.
func engage() error    { return nil }
func disengage() error { return nil }

// rawFlagActive best-effort checks `systemd-inhibit --list` for a lidspeculum
// lock by matching the WHO column EXACTLY (the first whitespace-separated field
// of a row), rather than substring-matching the whole output, which would
// misclassify a foreign inhibitor whose WHY/COMM merely mentions "lidspeculum".
//
// Parsing is defensive: format varies across systemd versions (header rows, a
// trailing "N inhibitors listed." summary line, column alignment), so any
// unexpected shape simply doesn't match, and any error yields false rather than
// a panic. Used only by status'/stop's stranded-detection (mostly a macOS/Windows
// concern; on Linux a dead holder takes its inhibitor lock with it).
func rawFlagActive() bool {
	out, err := exec.Command("systemd-inhibit", "--list").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "lidspeculum" {
			return true
		}
	}
	return false
}
