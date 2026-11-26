//go:build windows

package main

import (
	"os"
	"syscall"
)

// cancelSignals on Windows: Ctrl+C (CTRL_C_EVENT, os.Interrupt) and closing
// the console (CTRL_CLOSE_EVENT, which Go delivers as SIGTERM once notified).
var cancelSignals = []os.Signal{os.Interrupt, syscall.SIGTERM}
