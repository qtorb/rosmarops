//go:build windows

package main

import (
	"os"
	"syscall"
	"unsafe"
)

// freeConsole detaches the process from its console window (decision
// 2026-10-26, integridad y revisión semanal, J3): a scheduled review shows
// no window. https://learn.microsoft.com/windows/console/freeconsole
func freeConsole() {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("FreeConsole")
	if proc.Find() == nil {
		_, _, _ = proc.Call()
	}
}

// setConsoleTitle sets the console window's title (BV-151), which Windows
// shows in the taskbar thumbnail. kernel32 only, no network.
// https://learn.microsoft.com/windows/console/setconsoletitle
func setConsoleTitle(s string) {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleTitleW")
	if proc.Find() != nil {
		return
	}
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		return
	}
	_, _, _ = proc.Call(uintptr(unsafe.Pointer(p)))
}

// enableVTProcessing turns on ENABLE_VIRTUAL_TERMINAL_PROCESSING on standard
// output so ANSI colour and OSC sequences are interpreted (BV-151). It
// returns whether virtual-terminal processing is on afterwards; on failure
// the caller omits colour and the taskbar sequences. kernel32 only, no
// network.
func enableVTProcessing() bool {
	const enableVTProcessing = 0x0004
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getMode := kernel32.NewProc("GetConsoleMode")
	setMode := kernel32.NewProc("SetConsoleMode")
	if getMode.Find() != nil || setMode.Find() != nil {
		return false
	}
	h := syscall.Handle(os.Stdout.Fd())
	var mode uint32
	if r, _, _ := getMode.Call(uintptr(h), uintptr(unsafe.Pointer(&mode))); r == 0 {
		return false
	}
	if mode&enableVTProcessing != 0 {
		return true // already on
	}
	if r, _, _ := setMode.Call(uintptr(h), uintptr(mode|enableVTProcessing)); r == 0 {
		return false
	}
	return true
}
