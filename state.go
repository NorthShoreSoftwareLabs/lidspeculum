package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// hold is the on-disk record of the single active hold. At most one of these
// exists at a time, enforced by atomic (O_CREATE|O_EXCL) creation of the
// pidfile.
type hold struct {
	PID       int    `json:"pid"`
	StartedAt int64  `json:"started_at"`        // unix seconds
	ExpiresAt int64  `json:"expires_at"`        // unix seconds, 0 if no deadline
	Kind      string `json:"kind"`              // "hold" or "run"
	Command   string `json:"command,omitempty"` // for kind=="run"
}

// pidfileName is the file holding the single active hold record.
const pidfileName = "hold.json"

// stateDir returns the per-user state directory, creating it (0700) if needed.
//
//   - Linux:   $XDG_STATE_HOME/lidspeculum or ~/.local/state/lidspeculum
//   - macOS:   os.UserConfigDir()/lidspeculum (~/Library/Application Support)
//   - Windows: %LOCALAPPDATA%\lidspeculum
func stateDir() (string, error) {
	dir, err := stateBaseDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "lidspeculum")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func pidfilePath() (string, error) {
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, pidfileName), nil
}

// readHold reads the pidfile. It returns (nil, nil) when no pidfile exists.
func readHold() (*hold, error) {
	p, err := pidfilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var h hold
	if err := json.Unmarshal(data, &h); err != nil {
		// A corrupt pidfile is treated as a stale/garbage holder: report it so
		// callers can reclaim it.
		return nil, errCorruptPidfile
	}
	// A pid <= 1 is never a legitimate holder (0 = nil/unset, 1 = init). Treat
	// such a record as corrupt so callers reclaim it instead of trusting—and
	// later signaling—a bogus pid read from a user-writable file.
	if h.PID <= 1 {
		return nil, errCorruptPidfile
	}
	return &h, nil
}

var errCorruptPidfile = errors.New("pidfile is corrupt")

// writeHoldExclusive atomically creates the pidfile. It returns os.ErrExist if a
// pidfile already exists, which the caller uses to detect a competing holder.
func writeHoldExclusive(h *hold) error {
	p, err := pidfilePath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(h)
	if err != nil {
		return err
	}
	// O_NOFOLLOW (unix) rejects a symlink planted at hold.json, closing a
	// tampering vector; it is 0 on Windows, where the flag does not exist.
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY|oNoFollow, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(p)
		return err
	}
	return f.Close()
}

// removePidfile deletes the pidfile if present.
func removePidfile() error {
	p, err := pidfilePath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
