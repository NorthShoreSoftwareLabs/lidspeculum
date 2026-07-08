//go:build darwin

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
)

const (
	// pmsetPath is pinned so the command lidspeculum runs matches the sudoers rule
	// installed by `authorize` byte-for-byte (see cmdAuthorize).
	pmsetPath   = "/usr/bin/pmset"
	visudoPath  = "/usr/sbin/visudo"
	sudoersPath = "/etc/sudoers.d/lidspeculum"
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
	args := []string{pmsetPath, "-a", "disablesleep", value}
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

// targetUser returns the login user the sudoers rule should name. When invoked
// under sudo it prefers SUDO_USER (the real user, not root). It rejects root and
// any username with characters outside a conservative set, so a hostile or odd
// username can never inject extra sudoers syntax.
func targetUser() (string, error) {
	name := os.Getenv("SUDO_USER")
	if name == "" {
		u, err := user.Current()
		if err != nil {
			return "", fmt.Errorf("can't determine your username: %w", err)
		}
		name = u.Username
	}
	if name == "root" {
		return "", fmt.Errorf("run this as your normal user (not root) so the rule names the right account")
	}
	if !validUsername(name) {
		return "", fmt.Errorf("refusing to build a sudoers rule for an unusual username %q", name)
	}
	return name, nil
}

// validUsername allows only the characters a normal macOS account name uses,
// keeping the generated sudoers line to a known-safe shape.
func validUsername(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if !ok {
			return false
		}
	}
	return true
}

// confirmPrompt asks a yes/no question on stdout and reads the answer from stdin.
// Anything other than y/yes (including EOF or a non-interactive stdin) is "no".
func confirmPrompt(msg string) bool {
	fmt.Print(msg)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

// runSudo runs `sudo <args>` with the terminal wired through so any password
// prompt is visible and answerable.
func runSudo(args ...string) error {
	c := exec.Command("sudo", args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// cmdAuthorize installs a narrowly-scoped sudoers rule so holds no longer prompt
// for a password. The rule grants passwordless sudo for ONLY the two exact pmset
// commands lidspeculum runs to toggle the lid-close setting — nothing else.
//
// Lockout safety: a malformed file under /etc/sudoers.d breaks all of sudo, so we
// validate the generated file with `visudo -cf` (unprivileged, touching only our
// temp file) BEFORE it is ever placed under /etc. Only after it validates do we
// install it, then re-validate the whole set and roll back if anything is wrong.
func cmdAuthorize(assumeYes bool) int {
	target, err := targetUser()
	if err != nil {
		fmt.Fprintln(os.Stderr, "lidspeculum:", err)
		return 1
	}

	rule := fmt.Sprintf("%s ALL=(root) NOPASSWD: %s -a disablesleep 1, %s -a disablesleep 0",
		target, pmsetPath, pmsetPath)
	content := "# Installed by `lidspeculum authorize`. Lets lidspeculum toggle the\n" +
		"# lid-close sleep setting without prompting for a password. Remove it\n" +
		"# with `lidspeculum revoke` (or just delete this file).\n" +
		rule + "\n"

	fmt.Println("This lets holds start and stop without asking for your password, by adding")
	fmt.Println("a passwordless-sudo rule scoped to exactly the two commands lidspeculum runs:")
	fmt.Println()
	fmt.Printf("  file: %s\n", sudoersPath)
	fmt.Printf("  rule: %s\n", rule)
	fmt.Println()
	fmt.Println("Nothing else gets passwordless sudo. Installing it asks for your password once.")
	if !assumeYes && !confirmPrompt("Proceed? [y/N]: ") {
		fmt.Println("lidspeculum: cancelled; nothing changed.")
		return 1
	}

	tmp, err := os.CreateTemp("", "lidspeculum-sudoers-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "lidspeculum:", err)
		return 1
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		fmt.Fprintln(os.Stderr, "lidspeculum:", err)
		return 1
	}
	tmp.Close()

	// Validate BEFORE the file goes anywhere near /etc/sudoers.d.
	if err := exec.Command(visudoPath, "-cf", tmpName).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "lidspeculum: the generated rule failed sudoers validation; not installing.")
		return 1
	}

	if err := runSudo("install", "-m", "0440", "-o", "root", "-g", "wheel", tmpName, sudoersPath); err != nil {
		fmt.Fprintln(os.Stderr, "lidspeculum: couldn't install the rule:", err)
		return 1
	}

	// Now that it is live, re-validate the whole sudoers set; roll back on any
	// problem so we never leave sudo in a broken state.
	if err := runSudo(visudoPath, "-c"); err != nil {
		_ = runSudo("rm", "-f", sudoersPath)
		fmt.Fprintln(os.Stderr, "lidspeculum: sudoers validation failed after install; rolled it back. Nothing changed.")
		return 1
	}

	fmt.Println("lidspeculum: authorized. Holds no longer prompt for your password.")
	fmt.Println("Undo any time with: lidspeculum revoke")
	return 0
}

// cmdRevoke removes the sudoers rule installed by authorize. `rm -f` is
// idempotent, so revoking when nothing is installed is harmless.
func cmdRevoke(assumeYes bool) int {
	if !assumeYes && !confirmPrompt(fmt.Sprintf("Remove %s so holds require your password again? [y/N]: ", sudoersPath)) {
		fmt.Println("lidspeculum: cancelled; nothing changed.")
		return 1
	}
	if err := runSudo("rm", "-f", sudoersPath); err != nil {
		fmt.Fprintln(os.Stderr, "lidspeculum: couldn't remove the rule:", err)
		return 1
	}
	fmt.Println("lidspeculum: revoked. Holds will prompt for your password again.")
	return 0
}

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
