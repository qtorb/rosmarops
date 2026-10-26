//go:build windows

package main

import "syscall"

// freeConsole detaches the process from its console window (decision
// 2026-10-26, integridad y revisión semanal, J3): a scheduled review shows
// no window. https://learn.microsoft.com/windows/console/freeconsole
func freeConsole() {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("FreeConsole")
	if proc.Find() == nil {
		_, _, _ = proc.Call()
	}
}
