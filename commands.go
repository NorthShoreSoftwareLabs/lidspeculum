package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"time"
)

// acquire takes the single hold slot by atomically creating the pidfile. If a
// pidfile already exists it inspects the recorded pid: a live holder blocks us
// (returns errHoldActive); a dead/corrupt one is reclaimed (its pidfile and any
// stranded OS flag are cleared, then we take over).
func acquire(h *hold) error {
	err := writeHoldExclusive(h)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrExist) {
		return err
	}

	// Pidfile exists. Decide whether the holder is alive.
	existing, rerr := readHold()
	if rerr != nil && !errors.Is(rerr, errCorruptPidfile) {
		return rerr
	}
	if rerr == nil && existing != nil && processAlive(existing.PID) {
		detail := fmt.Sprintf("pid %d", existing.PID)
		if existing.ExpiresAt > 0 {
			detail += ", expires " + time.Unix(existing.ExpiresAt, 0).Format("15:04")
		}
		return fmt.Errorf("%w (%s); run `lidspeculum stop` to end it", errHoldActive, detail)
	}

	// Stale or corrupt: reclaim. Clear any stranded OS flag, drop the pidfile,
	// then retake the slot.
	if rawFlagActive() {
		_ = disengage()
	}
	if err := removePidfile(); err != nil {
		return err
	}
	// If two starts race here, the loser's re-create hits os.ErrExist because the
	// winner already took the slot. Map that to the friendly errHoldActive so the
	// loser prints "a hold is already active" rather than a raw "file exists".
	if err := writeHoldExclusive(h); err != nil {
		if errors.Is(err, os.ErrExist) {
			return errHoldActive
		}
		return err
	}
	return nil
}

var errHoldActive = errors.New("a hold is already active")

// releaseOnce guards release so disengage()+removePidfile() can never run twice,
// defending the single-release invariant even if a future call site is added.
var releaseOnce sync.Once

// release disengages the OS lever and removes the pidfile. It is safe to call
// more than once (subsequent calls are no-ops via releaseOnce).
//
// Ownership guard: before touching anything, we check that THIS process still
// owns the hold. A superseded holder (its pidfile was removed by `stop` and a
// newer holder B then engaged and wrote B's pidfile) must NOT disengage the
// lever B owns nor delete B's pidfile. So:
//   - no pidfile, or pidfile.PID == our pid: this is our clean release —
//     disengage(), and on success remove the pidfile (if absent, the disengage
//     is just idempotent).
//   - pidfile.PID != our pid: we've been superseded — leave the lever and the
//     pidfile alone and return.
//
// Invariant: the pidfile is present whenever the OS flag might still be set. So
// in the owned-release branch, if disengage() fails (the flag may still be set),
// we deliberately KEEP the pidfile, leaving a later `stop`/`status` able to find
// and recover the holder. Only once the flag is confirmed restored do we remove
// the pidfile.
func release(quiet bool) {
	releaseOnce.Do(func() {
		// A corrupt pidfile is treated as "not clearly ours"; the conservative
		// read below (cur == nil only on a clean absent file) keeps us from
		// stomping a record we can't parse.
		cur, _ := readHold()
		if cur != nil && cur.PID != os.Getpid() {
			// Superseded by another holder: do not disengage its lever or remove
			// its pidfile.
			return
		}

		if err := disengage(); err != nil {
			fmt.Fprintln(os.Stderr, "lidspeculum: restore failed:", err)
			// Keep the pidfile so the stranded flag stays recoverable.
			return
		}
		_ = removePidfile()
		if !quiet {
			fmt.Fprintln(os.Stderr, "lidspeculum: released. Your machine can sleep normally now.")
		}
	})
}

// deadline computes the absolute expiry time from the mutually exclusive --for
// and --until flags. A zero time means "no deadline".
func deadline(forStr, untilStr string, now time.Time) (time.Time, error) {
	switch {
	case forStr != "" && untilStr != "":
		return time.Time{}, errors.New("--for and --until are mutually exclusive; use only one")
	case forStr != "":
		d, err := ParseFor(forStr)
		if err != nil {
			return time.Time{}, err
		}
		return now.Add(d), nil
	case untilStr != "":
		return ParseUntil(untilStr, now)
	default:
		return time.Time{}, nil
	}
}

// holdStartLine builds the human start message for a hold. byUntil selects the
// wall-clock phrasing (--until) over the relative-span phrasing (--for).
func holdStartLine(now, expires time.Time, byUntil bool) string {
	if expires.IsZero() {
		return "lidspeculum: holding awake until you press Ctrl-C (screen can still sleep)."
	}
	left := shortDur(expires.Sub(now))
	if byUntil {
		return fmt.Sprintf("lidspeculum: holding awake until %s (%s). Press Ctrl-C to stop.",
			expires.Format("15:04"), left)
	}
	return fmt.Sprintf("lidspeculum: holding awake for %s, until %s. Press Ctrl-C to stop.",
		left, expires.Format("15:04"))
}

// cmdHold runs a resident hold until Ctrl-C, a deadline, or a stop/term signal.
func cmdHold(forStr, untilStr string, quiet bool) int {
	now := time.Now()
	expires, err := deadline(forStr, untilStr, now)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 2
	}

	// On Linux the first pass re-execs under systemd-inhibit and never returns.
	if _, err := maybeReexec(); err != nil {
		fmt.Fprintln(os.Stderr, "lidspeculum:", err)
		return 1
	}

	h := &hold{
		PID:       os.Getpid(),
		StartedAt: now.Unix(),
		Kind:      "hold",
	}
	if !expires.IsZero() {
		h.ExpiresAt = expires.Unix()
	}
	if err := acquire(h); err != nil {
		return acquireExit(err)
	}

	announceElevation(quiet)
	if err := engage(); err != nil {
		// A partial flag flip may remain; disengage (idempotent) before dropping
		// the pidfile so we never strand the flag with no pidfile to recover it.
		_ = disengage()
		_ = removePidfile()
		fmt.Fprintln(os.Stderr, "lidspeculum:", err)
		return 1
	}

	if !quiet {
		fmt.Fprintln(os.Stderr, holdStartLine(now, expires, untilStr != ""))
	}

	// Best-effort confirmation that the keeper actually took hold (Linux only;
	// no-op elsewhere). Never fails the hold.
	confirmKeeper(quiet)

	return residentWait(expires, quiet)
}

// cmdRun holds awake only while cmd runs, then exits with the command's code.
func cmdRun(cmd []string, quiet bool) int {
	if len(cmd) == 0 {
		fmt.Fprintln(os.Stderr, "error: run needs a command\n       e.g. lidspeculum run make build")
		return 2
	}

	// Fail fast (before re-exec, pidfile, or any elevation) if the command isn't
	// found.
	if _, err := exec.LookPath(cmd[0]); err != nil {
		fmt.Fprintf(os.Stderr, "lidspeculum: %s: %v\n", cmd[0], err)
		return 1
	}

	if _, err := maybeReexec(); err != nil {
		fmt.Fprintln(os.Stderr, "lidspeculum:", err)
		return 1
	}

	now := time.Now()
	h := &hold{
		PID:       os.Getpid(),
		StartedAt: now.Unix(),
		Kind:      "run",
		Command:   strings.Join(cmd, " "),
	}
	if err := acquire(h); err != nil {
		return acquireExit(err)
	}

	announceElevation(quiet)
	if err := engage(); err != nil {
		// A partial flag flip may remain; disengage (idempotent) before dropping
		// the pidfile so we never strand the flag with no pidfile to recover it.
		_ = disengage()
		_ = removePidfile()
		fmt.Fprintln(os.Stderr, "lidspeculum:", err)
		return 1
	}

	if !quiet {
		fmt.Fprintf(os.Stderr, "lidspeculum: holding awake while %q runs.\n", h.Command)
	}

	// Best-effort confirmation that the keeper actually took hold (Linux only;
	// no-op elsewhere). Never fails the hold.
	confirmKeeper(quiet)

	return runWrapped(cmd, quiet)
}

// residentWait blocks until a deadline timer fires or a stop signal arrives,
// then releases cleanly. Signals skip deferred cleanup, so release is wired
// through the signal path explicitly.
func residentWait(expires time.Time, quiet bool) int {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, stopSignals...)
	defer signal.Stop(sig)

	var timer *time.Timer
	var timerC <-chan time.Time
	if !expires.IsZero() {
		timer = time.NewTimer(time.Until(expires))
		defer timer.Stop()
		timerC = timer.C
	}

	select {
	case <-sig:
	case <-timerC:
	}
	release(quiet)
	return 0
}

// runWrapped starts the wrapped command, forwards stdio, and releases on the
// command's exit or on a stop signal. It returns the command's exit code.
func runWrapped(cmd []string, quiet bool) int {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, stopSignals...)
	defer signal.Stop(sig)

	c := exec.Command(cmd[0], cmd[1:]...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := c.Start(); err != nil {
		release(quiet)
		fmt.Fprintln(os.Stderr, "lidspeculum:", err)
		return 1
	}

	done := make(chan error, 1)
	go func() { done <- c.Wait() }()

	var waitErr error
	select {
	case <-sig:
		// The child shares our terminal and likely got the same signal. Give it a
		// bounded grace period to exit; if it ignores the signal and keeps running,
		// kill it rather than blocking forever on <-done while still holding the
		// flag (a second Ctrl-C would otherwise be dropped).
		select {
		case waitErr = <-done:
		case <-time.After(5 * time.Second):
			_ = c.Process.Kill()
			waitErr = <-done
		}
	case waitErr = <-done:
	}
	release(quiet)

	if waitErr != nil {
		var ee *exec.ExitError
		if errors.As(waitErr, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "lidspeculum:", waitErr)
		return 1
	}
	return 0
}

// acquireExit maps an acquire error to a message and exit code. Both the
// "hold already active" case and any other acquire failure print the error and
// exit 1, so there is a single path here.
func acquireExit(err error) int {
	fmt.Fprintln(os.Stderr, "lidspeculum:", err)
	return 1
}

// cmdStatus reports whether a hold is active and, when applicable, that a
// stranded OS flag is set with no live holder.
func cmdStatus(asJSON bool) int {
	h, err := readHold()
	corrupt := errors.Is(err, errCorruptPidfile)
	if err != nil && !corrupt {
		fmt.Fprintln(os.Stderr, "lidspeculum:", err)
		return 1
	}

	active := h != nil && !corrupt && processAlive(h.PID)
	stranded := !active && rawFlagActive()

	if asJSON {
		return statusJSON(active, stranded, h)
	}

	if active {
		started := time.Unix(h.StartedAt, 0).Format("15:04")
		if h.ExpiresAt > 0 {
			exp := time.Unix(h.ExpiresAt, 0)
			left := shortDur(time.Until(exp))
			fmt.Printf("lidspeculum: holding awake (pid %d), started %s, expires %s (%s left). Stop it with: lidspeculum stop\n",
				h.PID, started, exp.Format("15:04"), left)
		} else {
			fmt.Printf("lidspeculum: holding awake (pid %d), started %s, no deadline. Stop it with: lidspeculum stop\n",
				h.PID, started)
		}
		return 0
	}

	if stranded {
		fmt.Println("lidspeculum: no active hold, but the lid-close setting is still overridden (a previous hold didn't clean up). Run: lidspeculum stop")
		return 0
	}

	fmt.Println("lidspeculum: no active hold. Your machine sleeps normally.")
	return 0
}

func statusJSON(active, stranded bool, h *hold) int {
	obj := map[string]any{"active": active}
	if active {
		obj["pid"] = h.PID
		obj["kind"] = h.Kind
		obj["started_at"] = h.StartedAt
		obj["expires_at"] = h.ExpiresAt
		if h.Kind == "run" && h.Command != "" {
			obj["command"] = h.Command
		}
	}
	if stranded {
		obj["stranded"] = true
	}
	out, err := json.Marshal(obj)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lidspeculum:", err)
		return 1
	}
	fmt.Println(string(out))
	return 0
}

// cmdStop makes the machine sleepable again. It ends a live hold (by signaling
// its process), or clears a stranded OS flag when no live holder exists.
func cmdStop() int {
	h, err := readHold()
	corrupt := errors.Is(err, errCorruptPidfile)
	if err != nil && !corrupt {
		fmt.Fprintln(os.Stderr, "lidspeculum:", err)
		return 1
	}

	if h != nil && !corrupt && processAlive(h.PID) {
		code, handled := stopLiveHold(h)
		if handled {
			return code
		}
		// stopLiveHold declined to signal (e.g. the pid is not a lidspeculum
		// process, so it would be unsafe to SIGTERM it). Fall through to
		// stranded-flag handling below.
	}

	return clearStranded()
}

// clearStranded clears a stranded OS flag and/or a stale pidfile when there is
// no live holder to signal.
func clearStranded() int {
	if rawFlagActive() {
		if err := disengage(); err != nil {
			fmt.Fprintln(os.Stderr, "lidspeculum:", err)
			return 1
		}
		_ = removePidfile()
		fmt.Println("lidspeculum: cleared a leftover lid-close override. Your machine sleeps normally.")
		return 0
	}

	// A stale/corrupt pidfile with no flag set: tidy it up but report nothing
	// was really running.
	_ = removePidfile()
	fmt.Println("lidspeculum: nothing to stop; your machine already sleeps normally.")
	return 0
}

// stopLiveHold ends a live hold. The per-OS stopHolder does the platform-specific
// work: on unix it signals the holder (which runs its own clean release), on
// Windows it disengages the flag and removes the pidfile directly. It then waits
// briefly for the pidfile to clear.
//
// It returns (code, handled). handled is false only when stopHolder declined to
// act (errNotLidspeculum: the recorded pid is not a lidspeculum process, so
// signaling it would be unsafe); the caller then falls through to stranded-flag
// handling. In all other cases handled is true and code is the exit code.
func stopLiveHold(h *hold) (int, bool) {
	if err := stopHolder(h.PID); err != nil {
		if errors.Is(err, errNotLidspeculum) {
			return 0, false // let the caller try stranded-flag recovery
		}
		fmt.Fprintln(os.Stderr, "lidspeculum:", err)
		return 1, true
	}

	// Poll up to ~3s for the holder's pidfile to disappear.
	for i := 0; i < 30; i++ {
		cur, _ := readHold()
		if cur == nil || cur.PID != h.PID || !processAlive(h.PID) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	// The poll can finish while the holder is wedged and the flag is still set
	// (e.g. its release stalled). Don't claim success on a lie: if the OS flag is
	// still active (mac/win), fall back to a direct disengage before reporting.
	// On Linux there is no persistent flag (rawFlagActive is best-effort), so this
	// is a no-op there.
	if rawFlagActive() {
		if err := disengage(); err != nil {
			fmt.Fprintln(os.Stderr, "lidspeculum: the hold did not release cleanly and the override is still set:", err)
			return 1, true
		}
		_ = removePidfile()
	}

	fmt.Println("lidspeculum: stopped the active hold. Your machine sleeps normally.")
	return 0, true
}
