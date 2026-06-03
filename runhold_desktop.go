//go:build darwin || windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
)

// runHeld enables the keep-awake setting, runs cmd, and always restores the
// setting afterward — including when this process is interrupted. A plain
// `defer disable()` is skipped when the process is killed by a signal, which
// on macOS/Windows could leave the machine unable to sleep. Catching SIGINT
// and SIGTERM keeps the restore reliable.
//
// (Linux has its own runHeld using systemd-inhibit, where the lock is released
// automatically when the process dies.)
func runHeld(cmd []string) int {
	// Fail before flipping any system state (and before any sudo prompt) if the
	// command doesn't exist.
	if _, err := exec.LookPath(cmd[0]); err != nil {
		fmt.Fprintf(os.Stderr, "lidspeculum: %s: %v\n", cmd[0], err)
		return 1
	}

	if err := enable(); err != nil {
		fmt.Fprintln(os.Stderr, "lidspeculum:", err)
		return 1
	}
	restore := func() {
		if err := disable(); err != nil {
			fmt.Fprintln(os.Stderr, "lidspeculum: restore failed:", err)
		}
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)

	c := exec.Command(cmd[0], cmd[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Start(); err != nil {
		restore()
		fmt.Fprintln(os.Stderr, "lidspeculum:", err)
		return 1
	}

	done := make(chan error, 1)
	go func() { done <- c.Wait() }()

	var waitErr error
	select {
	case <-sig:
		// The child shares our terminal and got the same signal; wait for it to
		// exit, then restore no matter what.
		waitErr = <-done
	case waitErr = <-done:
	}
	restore()

	if waitErr != nil {
		if ee, ok := waitErr.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "lidspeculum:", waitErr)
		return 1
	}
	return 0
}
