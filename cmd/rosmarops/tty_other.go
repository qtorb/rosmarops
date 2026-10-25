//go:build !windows

package main

import "os"

// isTerminal reports a console: a character device that is not /dev/null
// (a run with no input has /dev/null as its standard input). Only then is
// the answer read.
func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(info, null)
}
