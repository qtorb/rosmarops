//go:build linux

package main

import "syscall"

// The terminal attribute requests on Linux.
const (
	ioctlGetTermios = syscall.TCGETS
	ioctlSetTermios = syscall.TCSETS
)
