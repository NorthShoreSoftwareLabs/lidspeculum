//go:build darwin

package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// macOS: the one setting that defeats clamshell (lid-closed) sleep without an
// external display is `pmset disablesleep`. It needs root, so we shell out
// through sudo when we are not already root. The single-hold model means only
// one process ever flips this flag at a time.

// stateBaseDir returns the base directory for lidspeculum state on macOS:
// os.UserConfigDir() (~/Library/Application Support), unless $XDG_STATE_HOME is
// set (honored for users who prefer it).
func stateBaseDir() (string, error) {
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return x, nil
	}
	return os.UserConfigDir()
}

// preflightKeeper checks whether the keeper can run at all. macOS has no
// preconditions beyond pmset, which ships with the OS.
func preflightKeeper() error { return nil }

// maybeReexec is a no-op on macOS; there is no inhibitor process to re-exec
// under. It returns (false, nil) meaning "did not re-exec; continue here".
// keepDisplay is handled at runtime by engageDisplay, not here.
func maybeReexec(keepDisplay bool) (bool, error) { return false, nil }

// announceElevation warns the user that engaging will prompt for a password.
func announceElevation(quiet bool) {
	if !quiet {
		os.Stderr.WriteString("lidspeculum: changing the lid-close sleep setting needs admin rights; you may be prompted for your password.\n")
	}
}

func pmsetSet(value string) error {
	args := []string{"pmset", "-a", "disablesleep", value}
	if os.Geteuid() != 0 {
		args = append([]string{"sudo"}, args...)
	}
	c := exec.Command(args[0], args[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// confirmKeeper is a no-op on macOS: pmset disablesleep is a synchronous,
// directly-read flag, so there is nothing to confirm after the fact.
func confirmKeeper(quiet bool) {}

// engageDisplay keeps the display awake for the hold's lifetime, in addition to
// the lid lever. It shells out to `caffeinate -d`, the OS's own display-sleep
// inhibitor; unlike the lid setting this needs no elevation. `-w <our pid>` makes
// caffeinate exit if we die without cleaning up, so a display assertion is never
// stranded. It returns a stop function that ends the assertion promptly on a
// clean release.
func engageDisplay() (func(), error) {
	c := exec.Command("caffeinate", "-d", "-w", strconv.Itoa(os.Getpid()))
	if err := c.Start(); err != nil {
		return nil, err
	}
	return func() {
		_ = c.Process.Kill()
		_ = c.Wait()
	}, nil
}

// engage flips the lid-close sleep setting off (machine stays awake).
func engage() error { return pmsetSet("1") }

// disengage restores normal lid-close sleep.
func disengage() error { return pmsetSet("0") }

// rawFlagActive reads the OS flag directly (independent of any pidfile), used by
// status/stop to detect a stranded override left by a crashed hold.
func rawFlagActive() bool {
	out, err := exec.Command("pmset", "-g").Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "SleepDisabled") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == "1" {
				return true
			}
			return false
		}
	}
	// pmset omits the line when the value is the default (0).
	return false
}
