//go:build windows

package main

import (
	"syscall"
	"testing"
	"unsafe"
)

// BV-151, point 5: on real Windows (the verify-windows CI job) SetConsoleTitleW
// sets a title that GetConsoleTitleW reads back — direct proof the syscall
// works. This is more reliable than reading a transient console's
// MainWindowTitle with Get-Process on a headless runner, which may expose no
// visible window; it skips cleanly when the runner has no console title.
func TestSetConsoleTitleRoundTrip(t *testing.T) {
	const want = "RosmarOps · test title"
	setConsoleTitle(want)

	get := syscall.NewLazyDLL("kernel32.dll").NewProc("GetConsoleTitleW")
	if get.Find() != nil {
		t.Skip("GetConsoleTitleW unavailable")
	}
	buf := make([]uint16, 1024)
	r, _, _ := get.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r == 0 {
		t.Skip("no console title on this runner")
	}
	if got := syscall.UTF16ToString(buf); got != want {
		t.Errorf("console title round-trip = %q, want %q", got, want)
	}
}

// enableVTProcessing runs its kernel32 calls without panicking on real Windows.
func TestEnableVTProcessingRuns(t *testing.T) {
	_ = enableVTProcessing()
}
