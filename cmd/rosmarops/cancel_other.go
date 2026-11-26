//go:build !windows

package main

import (
	"os"
	"syscall"
)

// cancelSignals on macOS and Linux: Ctrl+C (SIGINT) and closing the terminal
// (SIGHUP).
var cancelSignals = []os.Signal{os.Interrupt, syscall.SIGHUP}
