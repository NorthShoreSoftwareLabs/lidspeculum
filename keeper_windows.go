//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Windows: the lid-close action lives in the active power scheme under the
// "Power buttons and lid" subgroup. Setting it to 0 ("Do nothing") keeps the
// machine awake when the lid shuts. Changing the active scheme needs an elevated
// (Administrator) shell.
//
// Action index values: 0 = do nothing, 1 = sleep, 2 = hibernate, 3 = shut down.
//
// The single-hold model guarantees only one process ever saves the prior
// AC/DC indices, so the saved "prev" file can't be corrupted by a competing
// writer.

const (
	subButtons = "4f971e89-eebd-4455-a8de-9e59040e7347" // SUB_BUTTONS
	lidAction  = "5ca83367-6e45-459f-a27b-476b1d01c936" // lid-close action
)

// stateBaseDir returns the base directory for lidspeculum state on Windows:
// %LOCALAPPDATA%. We deliberately do NOT fall back to a shared temp dir: the
// "prev" file holds the privileged powercfg state we restore on disengage, and
// writing it to a world-writable location is a tampering vector.
func stateBaseDir() (string, error) {
	if dir := os.Getenv("LOCALAPPDATA"); dir != "" {
		return dir, nil
	}
	return "", errors.New("LOCALAPPDATA not set")
}

// preflightKeeper checks the keeper's preconditions. powercfg ships with Windows.
func preflightKeeper() error { return nil }

// maybeReexec is a no-op on Windows; there is no inhibitor process to re-exec
// under. keepDisplay is handled at runtime by engageDisplay, not here.
func maybeReexec(keepDisplay bool) (bool, error) { return false, nil }

// SetThreadExecutionState flags. ES_CONTINUOUS makes the state persist until the
// next call (rather than resetting after one idle-timer cycle); ES_DISPLAY_REQUIRED
// keeps the display on. The state is scoped to the calling OS thread and is
// cleared when that thread exits, so engageDisplay pins a dedicated thread.
const (
	esContinuous      = 0x80000000
	esDisplayRequired = 0x00000002
)

// kernel32 is declared in proc_windows.go; reuse it here for the display flag.
var procSetThreadExecState = kernel32.NewProc("SetThreadExecutionState")

// engageDisplay keeps the display awake for the hold's lifetime. Unlike the lid
// setting (powercfg, which needs an elevated shell), the display request is a
// per-thread flag that needs no elevation. Because the flag is cleared when the
// setting thread exits, we hold it on a dedicated goroutine locked to its OS
// thread; the returned stop function releases the flag and lets that thread go.
func engageDisplay() (func(), error) {
	done := make(chan struct{})
	ready := make(chan error, 1)
	go func() {
		// Pin this goroutine to one OS thread for its whole life so the execution
		// state we set is the state that stays in force (and is cleared) here.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		r, _, err := procSetThreadExecState.Call(uintptr(esContinuous | esDisplayRequired))
		if r == 0 {
			ready <- fmt.Errorf("SetThreadExecutionState failed: %w", err)
			return
		}
		ready <- nil

		<-done
		// Clear the display requirement (ES_CONTINUOUS with no other flags).
		procSetThreadExecState.Call(uintptr(esContinuous))
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return func() { close(done) }, nil
}

// announceElevation warns the user that engaging needs an elevated shell.
// Windows does not prompt; it requires an already-elevated (Administrator)
// terminal, so engage() fails with a clear error if not elevated.
func announceElevation(quiet bool) {
	if !quiet {
		fmt.Fprintln(os.Stderr, "lidspeculum: changing the lid-close sleep setting needs an elevated (Administrator) terminal.")
	}
}

// confirmKeeper is a no-op on Windows: the lid-close action is a directly-read
// powercfg index, so there is nothing to confirm after the fact.
func confirmKeeper(quiet bool) {}

// cmdAuthorize / cmdRevoke are macOS conveniences. Windows gates the lid setting
// behind an elevated (Administrator) terminal / UAC rather than a password, so
// there is no passwordless rule to install or remove here.
func cmdAuthorize(assumeYes bool) int {
	fmt.Fprintln(os.Stderr, "lidspeculum: authorize is a macOS-only convenience. On Windows, run lidspeculum from an elevated (Administrator) terminal; there is no passwordless equivalent to install.")
	return 0
}

func cmdRevoke(assumeYes bool) int {
	fmt.Fprintln(os.Stderr, "lidspeculum: nothing to revoke on Windows.")
	return 0
}

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

func prevPath() (string, error) {
	dir, err := stateBaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "lidspeculum", "prev"), nil
}

// savePrev records the lid indices in effect before we changed them so
// disengage can put them back exactly.
func savePrev(ac, dc int) error {
	p, err := prevPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	return os.WriteFile(p, []byte(fmt.Sprintf("%d %d\n", ac, dc)), 0o600)
}

// validIndex reports whether v is a real lid-action value.
func validIndex(v int) bool { return v >= 0 && v <= 3 }

func loadPrev() (ac, dc int, ok bool) {
	p, err := prevPath()
	if err != nil {
		return 0, 0, false
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d %d", &ac, &dc); err != nil {
		return 0, 0, false
	}
	// Don't feed an out-of-range value from a corrupt file into a privileged
	// powercfg write; let disengage fall back to the default.
	if !validIndex(ac) || !validIndex(dc) {
		return 0, 0, false
	}
	return ac, dc, true
}

// engage saves the prior lid indices then sets the lid-close action to 0.
//
// Saving the prior state is a PRECONDITION of engaging: if we cannot read the
// current indices (or they come back invalid), we must NOT flip the flag,
// because a later disengage with no saved "prev" falls back to a hardcoded
// default and would silently destroy a user whose real lid setting was
// hibernate(2) or shutdown(3). So query+save first, and only setLid on success.
func engage() error {
	ac, dc, err := queryLidIndices()
	if err != nil {
		return fmt.Errorf("can't read current lid-close setting (run from an elevated terminal?): %w", err)
	}
	if ac < 0 || dc < 0 {
		return fmt.Errorf("can't read current lid-close setting: powercfg returned no AC/DC index")
	}
	if err := savePrev(ac, dc); err != nil {
		return fmt.Errorf("can't save current lid-close setting before changing it: %w", err)
	}
	return setLid("0")
}

// disengage restores the saved prior indices, or falls back to the Windows
// default (1 = sleep) if no valid saved state exists.
func disengage() error {
	if ac, dc, ok := loadPrev(); ok {
		if err := powercfg("/setacvalueindex", "SCHEME_CURRENT", subButtons, lidAction, strconv.Itoa(ac)); err != nil {
			return err
		}
		if err := powercfg("/setdcvalueindex", "SCHEME_CURRENT", subButtons, lidAction, strconv.Itoa(dc)); err != nil {
			return err
		}
		if p, perr := prevPath(); perr == nil {
			_ = os.Remove(p)
		}
		return powercfg("/setactive", "SCHEME_CURRENT")
	}
	return setLid("1")
}

// rawFlagActive reads the OS flag directly, used by status/stop to detect a
// stranded override.
func rawFlagActive() bool {
	ac, _, err := queryLidIndices()
	if err != nil {
		return false
	}
	return ac == 0
}
