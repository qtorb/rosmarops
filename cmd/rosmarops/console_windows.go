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

// ownConsole reports whether this process owns its console — the double-click
// case, where Windows allocated a fresh console for us alone. It asks how many
// processes share the console (GetConsoleProcessList): exactly one means ours.
// When it inherits a PowerShell or cmd console, the count is higher and we must
// not touch its mode. kernel32 only, no network.
// https://learn.microsoft.com/windows/console/getconsoleprocesslist
func ownConsole() bool {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleProcessList")
	if proc.Find() != nil {
		return false
	}
	var list [4]uint32
	n, _, _ := proc.Call(uintptr(unsafe.Pointer(&list[0])), uintptr(len(list)))
	return n == 1
}

// disableQuickEdit turns off QuickEdit on the console input so a click does not
// put the window in «Select» mode and freeze the program, restoring the mode on
// exit (BV-211 §2). It touches nothing unless this process owns its console (a
// double click): a console inherited from PowerShell or cmd is left as it was.
// The decision logic is in disableQuickEditWith (console.go); here are the real
// kernel32 seams. No network.
func disableQuickEdit() func() {
	return disableQuickEditWith(ownConsole, stdinConsoleMode, setStdinConsoleMode)
}

// stdinConsoleMode reads the console input mode, or false if it cannot.
func stdinConsoleMode() (uint32, bool) {
	getMode := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleMode")
	if getMode.Find() != nil {
		return 0, false
	}
	h := syscall.Handle(os.Stdin.Fd()) // the console input buffer
	var mode uint32
	if r, _, _ := getMode.Call(uintptr(h), uintptr(unsafe.Pointer(&mode))); r == 0 {
		return 0, false
	}
	return mode, true
}

// setStdinConsoleMode writes the console input mode, reporting success.
func setStdinConsoleMode(mode uint32) bool {
	setMode := syscall.NewLazyDLL("kernel32.dll").NewProc("SetConsoleMode")
	if setMode.Find() != nil {
		return false
	}
	h := syscall.Handle(os.Stdin.Fd())
	r, _, _ := setMode.Call(uintptr(h), uintptr(mode))
	return r != 0
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
