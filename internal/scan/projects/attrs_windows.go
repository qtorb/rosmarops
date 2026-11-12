//go:build windows

package projects

import (
	"io/fs"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// Windows file attributes the search reads from a listed entry.
const (
	attrOffline            = 0x1000   // FILE_ATTRIBUTE_OFFLINE
	attrReparsePoint       = 0x400    // FILE_ATTRIBUTE_REPARSE_POINT
	attrRecallOnOpen       = 0x40000  // FILE_ATTRIBUTE_RECALL_ON_OPEN
	attrRecallOnDataAccess = 0x400000 // FILE_ATTRIBUTE_RECALL_ON_DATA_ACCESS
)

// classify is the reason a listed folder is not entered, from its details
// alone (FindNextFile's attributes, no extra read): a junction, link or any
// reparse point, or a cloud placeholder (OneDrive and others).
func classify(info, _ fs.FileInfo) Reason {
	if info.Mode()&fs.ModeSymlink != 0 {
		return ReasonLinks
	}
	if d, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return attrReason(d.FileAttributes)
	}
	return ""
}

// attrReason maps a folder's attributes to a reason, or "".
func attrReason(a uint32) Reason {
	switch {
	case a&(attrRecallOnDataAccess|attrRecallOnOpen|attrOffline) != 0:
		return ReasonCloud
	case a&attrReparsePoint != 0:
		return ReasonLinks
	}
	return ""
}

const driveRemote = 4 // DRIVE_REMOTE

var getDriveType = syscall.NewLazyDLL("kernel32.dll").NewProc("GetDriveTypeW")

// networkPath reports a UNC path or a path on a network drive
// (GetDriveTypeW = DRIVE_REMOTE).
func networkPath(p string) bool {
	if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//") {
		return true
	}
	vol := filepath.VolumeName(p)
	if vol == "" {
		return false
	}
	root, err := syscall.UTF16PtrFromString(vol + `\`)
	if err != nil {
		return false
	}
	t, _, _ := getDriveType.Call(uintptr(unsafe.Pointer(root)))
	return t == driveRemote
}
