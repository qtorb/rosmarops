//go:build windows

package winpath

import (
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

// dirFromAPI calls a kernel32 "Get…DirectoryW" function (GetSystemDirectoryW,
// GetWindowsDirectoryW), which writes the path into a UTF-16 buffer and
// returns its length in characters (0 on failure, or the needed size when the
// buffer is too small). MAX_PATH is always enough for these two.
func dirFromAPI(name string) string {
	buf := make([]uint16, syscall.MAX_PATH)
	r, _, _ := kernel32.NewProc(name).Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	n := int(r)
	if n <= 0 || n > len(buf) {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

// SystemDir is the Windows system directory (GetSystemDirectoryW), typically
// C:\Windows\System32.
func SystemDir() string { return dirFromAPI("GetSystemDirectoryW") }

// WindowsDir is the Windows directory (GetWindowsDirectoryW), typically
// C:\Windows.
func WindowsDir() string { return dirFromAPI("GetWindowsDirectoryW") }
