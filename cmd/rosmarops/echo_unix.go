//go:build linux || darwin

package main

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// echoOff turns off the echo of the console f until restore runs (BV-120,
// point 2): the terminal attributes are read with an ioctl, written back
// without ECHO, and restored as they were.
func echoOff(f *os.File) (restore func(), err error) {
	fd := f.Fd()
	var old syscall.Termios
	if err := termios(fd, ioctlGetTermios, &old); err != nil {
		return nil, fmt.Errorf("read terminal attributes: %w", err)
	}
	quiet := old
	quiet.Lflag &^= syscall.ECHO
	if err := termios(fd, ioctlSetTermios, &quiet); err != nil {
		return nil, fmt.Errorf("turn off echo: %w", err)
	}
	return func() { _ = termios(fd, ioctlSetTermios, &old) }, nil
}

func termios(fd, req uintptr, t *syscall.Termios) error {
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(unsafe.Pointer(t))); errno != 0 {
		return errno
	}
	return nil
}
