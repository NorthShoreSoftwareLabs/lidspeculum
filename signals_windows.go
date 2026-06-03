//go:build windows

package main

import (
	"os"
	"syscall"
)

// stopSignals are the signals that trigger a clean release. Windows has no
// SIGHUP, so we listen for SIGINT (Ctrl-C) and SIGTERM only.
var stopSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
