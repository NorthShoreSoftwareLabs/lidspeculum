// Command lidspeculum keeps a computer awake while the lid is closed.
//
// Lid-close sleep ("clamshell sleep") is a separate mechanism from idle sleep on
// every major OS, so the usual keep-awake tricks (caffeinate, power assertions)
// do NOT prevent it. lidspeculum drives the one lever per platform that actually
// holds the lid open, behind an opinionated, caffeinate-style resident model:
// you start a hold and it stays in effect until you stop it (or a deadline).
//
//	macOS    pmset disablesleep        (needs sudo)
//	Linux    systemd-inhibit lock      (no root needed)
//	Windows  powercfg lid-close action (needs an elevated shell)
//
// See the per-platform keeper_*.go files for the details.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

// version is overridden at release time via -ldflags "-X main.version=...".
var version = "dev"

func usage() {
	fmt.Print(`lidspeculum — keep your computer awake with the lid closed.

USAGE
  lidspeculum hold                 keep awake until you press Ctrl-C
  lidspeculum hold --for 2h        keep awake for a while, then auto-release
  lidspeculum run make build       keep awake only while a command runs

COMMANDS
  hold      Hold the machine awake until Ctrl-C (or a timeout).
            Flags: --for <duration>, --until <HH:MM>, --display, -q/--quiet
  run       Hold awake only while the wrapped command runs; exits with its code.
            A leading -- is optional: lidspeculum run -- rsync -q ...
            Flags: --display, -q/--quiet

By default a hold keeps the SYSTEM awake but lets the screen sleep. Add --display
to keep the screen on too.
  status    Report whether a hold is active and how to stop it (--json).
  stop      Make the machine sleepable again (ends any active hold).
  version   Print the version.
  help      Show this help.

Starting a hold changes a system power setting, so it needs elevation:
  macOS    prompts for your password (sudo)
  Windows  must be run from an elevated (Administrator) terminal — no prompt
  Linux    needs no elevation
`)
}

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 0
	}

	switch args[0] {
	case "hold":
		return runHoldCmd(args[1:])
	case "run":
		return runRunCmd(args[1:])
	case "status":
		return runStatusCmd(args[1:])
	case "stop":
		return cmdStop()
	case "version", "-v", "--version":
		fmt.Println("lidspeculum " + version)
		return 0
	case "help", "-h", "--help":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "lidspeculum: unknown command %q\n\n", args[0])
		usage()
		return 2
	}
}

// parseResult maps a FlagSet.Parse error to an exit code: flag.ErrHelp (from
// -h/--help) is a clean exit 0; any other parse error is a usage error (2).
// ok is false when the caller should return code immediately.
func parseResult(err error) (code int, ok bool) {
	if err == nil {
		return 0, true
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0, false
	}
	return 2, false
}

// quietFlags registers both -q and --quiet on a FlagSet, returning a pointer to
// the shared bool.
func quietFlags(fs *flag.FlagSet) *bool {
	q := fs.Bool("quiet", false, "suppress the start/release lines")
	fs.BoolVar(q, "q", false, "suppress the start/release lines (shorthand)")
	return q
}

func runHoldCmd(args []string) int {
	fs := flag.NewFlagSet("hold", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	forStr := fs.String("for", "", "hold for a duration (e.g. 90m, 1h30m, 45s, or seconds)")
	untilStr := fs.String("until", "", "hold until a 24-hour clock time today (HH:MM)")
	display := fs.Bool("display", false, "also keep the screen awake, not just the system")
	quiet := quietFlags(fs)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `lidspeculum hold — keep the machine awake until you stop it.

USAGE
  lidspeculum hold [--for <duration> | --until <HH:MM>] [--display] [-q]

FLAGS
  --for <duration>   hold for 90m, 1h30m, 45s (a bare integer means seconds)
  --until <HH:MM>    hold until a 24-hour clock time today (errors if past)
  --display          also keep the screen on (by default only the system is held)
  -q, --quiet        suppress the start/release lines

EXAMPLES
  lidspeculum hold
  lidspeculum hold --for 2h
  lidspeculum hold --until 17:00
  lidspeculum hold --display
`)
	}
	if code, ok := parseResult(fs.Parse(args)); !ok {
		return code
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "error: hold takes no positional arguments (got %q)\n", fs.Arg(0))
		return 2
	}
	return cmdHold(*forStr, *untilStr, *quiet, *display)
}

func runRunCmd(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	display := fs.Bool("display", false, "also keep the screen awake, not just the system")
	quiet := quietFlags(fs)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `lidspeculum run — keep the machine awake only while a command runs.

USAGE
  lidspeculum run [--display] [-q] <cmd> [args...]
  lidspeculum run [--display] [-q] -- <cmd> [args...]

The optional -- separates lidspeculum's flags from the command. lidspeculum
exits with the wrapped command's exit code.

FLAGS
  --display    also keep the screen on (by default only the system is held)
  -q, --quiet  suppress the start/release lines

EXAMPLES
  lidspeculum run make build
  lidspeculum run -- rsync -q src/ dst/
  lidspeculum run --display make build
`)
	}
	if code, ok := parseResult(fs.Parse(args)); !ok {
		return code
	}
	return cmdRun(fs.Args(), *quiet, *display)
}

func runStatusCmd(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "print a one-shot JSON object, then exit")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `lidspeculum status — report whether a hold is active.

USAGE
  lidspeculum status [--json]

EXAMPLES
  lidspeculum status
  lidspeculum status --json
`)
	}
	if code, ok := parseResult(fs.Parse(args)); !ok {
		return code
	}
	return cmdStatus(*asJSON)
}
