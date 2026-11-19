//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

// osVersionInfo is RTL_OSVERSIONINFOW.
type osVersionInfo struct {
	size, major, minor, build, platform uint32
	csd                                 [128]uint16
}

// osVersion is Windows's major.minor.build from RtlGetVersion (ntdll), which,
// unlike GetVersionEx, is not subject to the manifest's compatibility shims.
func osVersion() string {
	proc := syscall.NewLazyDLL("ntdll.dll").NewProc("RtlGetVersion")
	if proc.Find() != nil {
		return ""
	}
	var v osVersionInfo
	v.size = uint32(unsafe.Sizeof(v))
	if r, _, _ := proc.Call(uintptr(unsafe.Pointer(&v))); r != 0 {
		return ""
	}
	return cleanOSVersion(fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.build))
}
