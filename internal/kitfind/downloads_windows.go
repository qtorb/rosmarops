//go:build windows

package kitfind

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"github.com/qtorb/rosmarops/internal/winpath"
)

// folderidDownloads is FOLDERID_Downloads, {374DE290-123F-4565-9164-39C4925E467B}.
var folderidDownloads = knownFolderID{
	0x374DE290, 0x123F, 0x4565,
	[8]byte{0x91, 0x64, 0x39, 0xC4, 0x92, 0x5E, 0x46, 0x7B},
}

type knownFolderID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

// Windows file attributes that mark a file as not a plain, present file: a
// reparse point (a link or a junction) or a cloud placeholder whose data is
// not downloaded (OneDrive and similar).
const (
	attrReparsePoint       = 0x00000400
	attrOffline            = 0x00001000
	attrRecallOnOpen       = 0x00040000
	attrRecallOnDataAccess = 0x00400000
)

// DownloadsDir is the user's Downloads folder from the Windows API
// (SHGetKnownFolderPath, shell32.dll loaded by its absolute system path), or
// %USERPROFILE%\Downloads when the API call fails (CTO-12 H3).
func DownloadsDir() (string, error) {
	if p, err := knownFolderDownloads(); err == nil && p != "" {
		return p, nil
	}
	if up := os.Getenv("USERPROFILE"); up != "" {
		return filepath.Join(up, "Downloads"), nil
	}
	return "", errors.New("kitfind: no Downloads folder")
}

// knownFolderDownloads calls SHGetKnownFolderPath for FOLDERID_Downloads.
// shell32.dll is loaded by its absolute path in the system directory, never
// by a bare name, which Windows would look up first in the program's folder.
func knownFolderDownloads() (string, error) {
	sys := winpath.SystemDir()
	if sys == "" {
		return "", errors.New("no system directory")
	}
	dll, err := syscall.LoadDLL(sys + `\shell32.dll`)
	if err != nil {
		return "", fmt.Errorf("load shell32.dll: %w", err)
	}
	defer dll.Release()
	proc, err := dll.FindProc("SHGetKnownFolderPath")
	if err != nil {
		return "", fmt.Errorf("find SHGetKnownFolderPath: %w", err)
	}
	var out *uint16
	r, _, _ := proc.Call(
		uintptr(unsafe.Pointer(&folderidDownloads)),
		0, 0,
		uintptr(unsafe.Pointer(&out)),
	)
	if r != 0 || out == nil { // S_OK is 0
		return "", fmt.Errorf("SHGetKnownFolderPath: hresult 0x%x", r)
	}
	defer coTaskMemFree(unsafe.Pointer(out))
	return utf16PtrToString(out), nil
}

// coTaskMemFree frees the path SHGetKnownFolderPath allocated, with
// ole32.dll's CoTaskMemFree (also loaded by its absolute system path).
func coTaskMemFree(p unsafe.Pointer) {
	sys := winpath.SystemDir()
	if sys == "" {
		return
	}
	dll, err := syscall.LoadDLL(sys + `\ole32.dll`)
	if err != nil {
		return
	}
	defer dll.Release()
	if proc, err := dll.FindProc("CoTaskMemFree"); err == nil {
		_, _, _ = proc.Call(uintptr(p))
	}
}

// utf16PtrToString reads a NUL-terminated UTF-16 string from p.
func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	var s []uint16
	for ptr := unsafe.Pointer(p); ; ptr = unsafe.Add(ptr, 2) {
		c := *(*uint16)(ptr)
		if c == 0 {
			break
		}
		s = append(s, c)
	}
	return syscall.UTF16ToString(s)
}

// special reports a reparse point or a not-yet-downloaded cloud placeholder,
// which the scan skips (CTO-12 H3).
func special(info os.FileInfo) bool {
	d, ok := info.Sys().(*syscall.Win32FileAttributeData)
	if !ok {
		return false
	}
	a := d.FileAttributes
	return a&attrReparsePoint != 0 || a&attrOffline != 0 ||
		a&attrRecallOnOpen != 0 || a&attrRecallOnDataAccess != 0
}
