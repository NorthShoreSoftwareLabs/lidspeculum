//go:build darwin

package main

import (
	"os"
	"os/exec"
	"strings"
)

// macOS: the only setting that defeats clamshell (lid-closed) sleep without
// an external display is `pmset disablesleep`. It needs root, so we shell out
// through sudo when we are not already root. `disablesleep 1` also blocks idle
// sleep, so the machine stays fully awake.

func pmsetSet(value string) error {
	args := []string{"pmset", "-a", "disablesleep", value}
	if os.Geteuid() != 0 {
		args = append([]string{"sudo"}, args...)
	}
	c := exec.Command(args[0], args[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

func enable() error  { return pmsetSet("1") }
func disable() error { return pmsetSet("0") }

func status() (bool, string, error) {
	out, err := exec.Command("pmset", "-g").Output()
	if err != nil {
		return false, "", err
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "SleepDisabled") {
			fields := strings.Fields(line)
			if len(fields) >= 2 && fields[1] == "1" {
				return true, "SleepDisabled=1", nil
			}
			return false, "SleepDisabled=0", nil
		}
	}
	// pmset omits the line entirely when the value is the default (0).
	return false, "SleepDisabled=0", nil
}
