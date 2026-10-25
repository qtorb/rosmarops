//go:build windows

package main

import (
	"os"
	"syscall"
)

// isTerminal reports a console: GetConsoleMode succeeds only on a console
// handle (a scheduled run has none; NUL or a pipe fails). Only then is the
// answer read.
func isTerminal(f *os.File) bool {
	var mode uint32
	return syscall.GetConsoleMode(syscall.Handle(f.Fd()), &mode) == nil
}
