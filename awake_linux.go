//go:build linux

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// errNoSystemd is returned when on/off can't find systemd-logind to drive.
var errNoSystemd = errors.New("lidspeculum on/off requires systemd-logind (systemctl not found); on non-systemd systems, use `lidspeculum run <cmd>` instead")

// Linux (systemd): lid behavior is owned by systemd-logind.
//
//   - on/off install or remove a logind drop-in that sets HandleLidSwitch=ignore,
//     then reload logind. This is persistent and needs root, so we re-exec under
//     sudo when necessary.
//   - run uses `systemd-inhibit`, which takes a lid-switch lock for the duration
//     of the wrapped command and needs no root at all.

const dropinDir = "/etc/systemd/logind.conf.d"
const dropinPath = dropinDir + "/10-lidspeculum.conf"

const dropin = `# Managed by lidspeculum. Remove with: lidspeculum off
[Login]
HandleLidSwitch=ignore
HandleLidSwitchExternalPower=ignore
HandleLidSwitchDocked=ignore
`

// reexecSudo re-runs this exact command (same args) under sudo.
func reexecSudo() error {
	self, err := os.Executable()
	if err != nil {
		return err
	}
	args := append([]string{self}, os.Args[1:]...)
	c := exec.Command("sudo", args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

func reloadLogind() error {
	c := exec.Command("systemctl", "kill", "-s", "HUP", "systemd-logind")
	c.Stdout, c.Stderr = os.Stdout, os.Stderr
	return c.Run()
}

func enable() error {
	// Check before any sudo prompt or file write, so non-systemd systems fail
	// fast instead of leaving a no-op drop-in that status would report as active.
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errNoSystemd
	}
	if os.Geteuid() != 0 {
		return reexecSudo()
	}
	if err := os.MkdirAll(dropinDir, 0o755); err != nil {
		return err
	}
	// O_NOFOLLOW: refuse to follow a symlink planted at the drop-in path.
	f, err := os.OpenFile(dropinPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(dropin); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return reloadLogind()
}

func disable() error {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return errNoSystemd
	}
	if os.Geteuid() != 0 {
		return reexecSudo()
	}
	if err := os.Remove(dropinPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return reloadLogind()
}

func status() (bool, string, error) {
	if _, err := os.Stat(dropinPath); err == nil {
		return true, "logind drop-in active: " + dropinPath, nil
	} else if os.IsNotExist(err) {
		return false, "no logind drop-in", nil
	} else {
		return false, "", err
	}
}

func runHeld(cmd []string) int {
	if _, err := exec.LookPath("systemd-inhibit"); err != nil {
		fmt.Fprintln(os.Stderr, "lidspeculum: systemd-inhibit not found (is this a systemd system?)")
		return 1
	}
	args := append([]string{
		"--what=handle-lid-switch:sleep:idle",
		"--who=lidspeculum",
		"--why=keep awake during command",
		"--mode=block",
	}, cmd...)
	c := exec.Command("systemd-inhibit", args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "lidspeculum:", err)
		return 1
	}
	return 0
}
