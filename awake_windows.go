//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Windows: the lid-close action lives in the active power scheme under the
// "Power buttons and lid" subgroup. Setting it to 0 ("Do nothing") keeps the
// machine awake when the lid shuts. Changing the active scheme needs an
// elevated (Administrator) shell.
//
// Action index values: 0 = do nothing, 1 = sleep, 2 = hibernate, 3 = shut down.

const (
	subButtons = "4f971e89-eebd-4455-a8de-9e59040e7347" // SUB_BUTTONS
	lidAction  = "5ca83367-6e45-459f-a27b-476b1d01c936" // lid-close action
)

func powercfg(args ...string) error {
	c := exec.Command("powercfg", args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// queryLidIndices returns the current AC and DC lid-close action indices.
func queryLidIndices() (ac, dc int, err error) {
	out, err := exec.Command("powercfg", "/query", "SCHEME_CURRENT", subButtons, lidAction).Output()
	if err != nil {
		return 0, 0, err
	}
	ac, dc = -1, -1
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Current AC Power Setting Index:"):
			ac = parseHexIndex(line)
		case strings.HasPrefix(line, "Current DC Power Setting Index:"):
			dc = parseHexIndex(line)
		}
	}
	return ac, dc, nil
}

func parseHexIndex(line string) int {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return -1
	}
	raw := fields[len(fields)-1] // e.g. 0x00000001
	v, err := strconv.ParseInt(strings.TrimPrefix(raw, "0x"), 16, 64)
	if err != nil {
		return -1
	}
	return int(v)
}

func setLid(value string) error {
	if err := powercfg("/setacvalueindex", "SCHEME_CURRENT", subButtons, lidAction, value); err != nil {
		return err
	}
	if err := powercfg("/setdcvalueindex", "SCHEME_CURRENT", subButtons, lidAction, value); err != nil {
		return err
	}
	return powercfg("/setactive", "SCHEME_CURRENT")
}

func statePath() string {
	dir := os.Getenv("LOCALAPPDATA")
	if dir == "" {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "lidspeculum", "prev")
}

// savePrev records the lid indices that were in effect before we changed them,
// so disable() can put them back exactly.
func savePrev(ac, dc int) error {
	p := statePath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(fmt.Sprintf("%d %d\n", ac, dc)), 0o644)
}

// validIndex reports whether v is a real lid-action value:
// 0 = do nothing, 1 = sleep, 2 = hibernate, 3 = shut down.
func validIndex(v int) bool { return v >= 0 && v <= 3 }

func loadPrev() (ac, dc int, ok bool) {
	data, err := os.ReadFile(statePath())
	if err != nil {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d %d", &ac, &dc); err != nil {
		return 0, 0, false
	}
	// Don't feed an out-of-range value from a tampered/corrupt file into a
	// privileged powercfg write; let disable() fall back to the default.
	if !validIndex(ac) || !validIndex(dc) {
		return 0, 0, false
	}
	return ac, dc, true
}

func enable() error {
	ac, dc, err := queryLidIndices()
	if err == nil && (ac >= 0 || dc >= 0) {
		_ = savePrev(ac, dc) // best effort; not fatal if it fails
	}
	return setLid("0")
}

func disable() error {
	if ac, dc, ok := loadPrev(); ok {
		if err := powercfg("/setacvalueindex", "SCHEME_CURRENT", subButtons, lidAction, strconv.Itoa(ac)); err != nil {
			return err
		}
		if err := powercfg("/setdcvalueindex", "SCHEME_CURRENT", subButtons, lidAction, strconv.Itoa(dc)); err != nil {
			return err
		}
		_ = os.Remove(statePath())
		return powercfg("/setactive", "SCHEME_CURRENT")
	}
	// No saved state: fall back to the Windows default (1 = sleep).
	return setLid("1")
}

func status() (bool, string, error) {
	ac, dc, err := queryLidIndices()
	if err != nil {
		return false, "", err
	}
	return ac == 0, fmt.Sprintf("lid action AC=%d DC=%d", ac, dc), nil
}
