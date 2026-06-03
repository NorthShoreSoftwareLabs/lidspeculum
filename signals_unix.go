//go:build !windows

package main

import (
	"os"
	"syscall"
)

// stopSignals are the signals that trigger a clean release. On unix we add
// SIGHUP (terminal hangup) on top of SIGINT/SIGTERM.
var stopSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
