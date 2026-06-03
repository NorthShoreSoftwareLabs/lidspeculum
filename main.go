// Command lidspeculum keeps a computer awake while the lid is closed.
//
// Lid-close sleep ("clamshell sleep") is a separate mechanism from idle
// sleep on every major OS, so the usual keep-awake tricks (caffeinate,
// power assertions) do NOT prevent it. lidspeculum drives the one setting
// per platform that actually holds the lid open:
//
//	macOS    pmset disablesleep
//	Linux    systemd-logind HandleLidSwitch (+ systemd-inhibit for `run`)
//	Windows  powercfg lid-close action
//
// See the per-platform awake_*.go files for the details.
package main

import (
	"fmt"
	"os"
)

// version is overridden at release time via -ldflags "-X main.version=...".
var version = "dev"

func usage() {
	fmt.Print(`lidspeculum ` + version + ` — keep this computer awake with the lid shut

Usage:
  lidspeculum on             Stop the lid close from sleeping the machine
  lidspeculum off            Restore normal lid-close behavior
  lidspeculum status         Show the current setting
  lidspeculum run <cmd...>   Turn on, run cmd, restore automatically on exit
  lidspeculum version        Print the version
  lidspeculum help           Show this help

Examples:
  lidspeculum on
  lidspeculum run go test ./...
  lidspeculum off

Notes:
  - on/off change a system power setting and need admin rights
    (sudo on macOS/Linux, an elevated shell on Windows).
  - On Linux, "run" uses systemd-inhibit and needs no root.
  - Closing the lid with no external display restricts airflow; watch
    temperatures on long jobs.
`)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "lidspeculum:", err)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}

	switch os.Args[1] {
	case "on":
		if err := enable(); err != nil {
			fail(err)
		}
		fmt.Println("lidspeculum: ON — closing the lid will not sleep this machine.")

	case "off":
		if err := disable(); err != nil {
			fail(err)
		}
		fmt.Println("lidspeculum: OFF — normal lid-close behavior restored.")

	case "status":
		active, detail, err := status()
		if err != nil {
			fail(err)
		}
		state := "normal sleep"
		if active {
			state = "lid-close sleep OFF"
		}
		fmt.Printf("lidspeculum: %s (%s)\n", state, detail)

	case "run":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "lidspeculum run: need a command to run")
			os.Exit(1)
		}
		os.Exit(runHeld(os.Args[2:]))

	case "version", "-v", "--version":
		fmt.Println("lidspeculum " + version)

	case "help", "-h", "--help":
		usage()

	default:
		fmt.Fprintf(os.Stderr, "lidspeculum: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}
