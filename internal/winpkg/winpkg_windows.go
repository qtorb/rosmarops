//go:build windows

package winpkg

import (
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

// appModelErrorNoPackage is APPMODEL_ERROR_NO_PACKAGE: the process has no
// package identity (RosmarOps runs unpackaged). It is not a failure.
const appModelErrorNoPackage = 15700

// query calls one of kernel32's GetCurrentPackage* functions, which take
// (UINT32 *length, PWSTR buffer): a first call with a null buffer gives the
// length needed, the second fills it. "" without a package or on any error.
func query(name string) string {
	proc := kernel32.NewProc(name)
	if proc.Find() != nil {
		return ""
	}
	var length uint32
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(&length)), 0)
	if r == appModelErrorNoPackage || length == 0 {
		return ""
	}
	buf := make([]uint16, length)
	r, _, _ = proc.Call(uintptr(unsafe.Pointer(&length)), uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 { // anything but ERROR_SUCCESS
		return ""
	}
	return syscall.UTF16ToString(buf[:length])
}

// FamilyName is GetCurrentPackageFamilyName, "" when unpackaged.
func FamilyName() string { return query("GetCurrentPackageFamilyName") }

// FullName is GetCurrentPackageFullName, "" when unpackaged.
func FullName() string { return query("GetCurrentPackageFullName") }

// Path is GetCurrentPackagePath, the package's installed folder, "" when
// unpackaged.
func Path() string { return query("GetCurrentPackagePath") }
