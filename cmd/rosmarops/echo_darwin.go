//go:build darwin

package main

import "syscall"

// The terminal attribute requests on macOS.
const (
	ioctlGetTermios = syscall.TIOCGETA
	ioctlSetTermios = syscall.TIOCSETA
)
