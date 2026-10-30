//go:build windows

package main

import (
	"fmt"
	"os"
	"syscall"
)

// enableEchoInput is ENABLE_ECHO_INPUT, a console input mode flag.
const enableEchoInput = 0x0004

var setConsoleMode = syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode")

// echoOff turns off the echo of the console f until restore runs (BV-120,
// point 2): its input mode without ENABLE_ECHO_INPUT, then the old mode.
func echoOff(f *os.File) (restore func(), err error) {
	h := syscall.Handle(f.Fd())
	var old uint32
	if err := syscall.GetConsoleMode(h, &old); err != nil {
		return nil, fmt.Errorf("read console mode: %w", err)
	}
	if r, _, err := setConsoleMode.Call(uintptr(h), uintptr(old&^enableEchoInput)); r == 0 {
		return nil, fmt.Errorf("turn off echo: %w", err)
	}
	return func() { _, _, _ = setConsoleMode.Call(uintptr(h), uintptr(old)) }, nil
}
