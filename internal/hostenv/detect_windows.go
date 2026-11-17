//go:build windows

package hostenv

import (
	"os"
	"syscall"
	"unsafe"
)

// Detect is this computer's environment on Windows: Windows Sandbox first
// (its WDAGUtilityAccount user), then a virtual machine from the BIOS
// registry values, read with the standard library (no PowerShell).
func Detect() *Env {
	if _, err := os.Stat(`C:\Users\WDAGUtilityAccount`); err == nil {
		return &Env{Kind: KindWindowsSandbox, Signal: SignalWDAGAccount}
	}
	vendor := biosValue("SystemManufacturer")
	product := biosValue("SystemProductName")
	if t := VMTypeOf(vendor, product); t != "" {
		return &Env{Kind: KindVirtualMachine, Signal: SignalBIOSRegistry, VMType: t}
	}
	return nil
}

// biosValue reads a REG_SZ value of HKLM\HARDWARE\DESCRIPTION\System\BIOS;
// "" when it cannot.
func biosValue(name string) string {
	path, err := syscall.UTF16PtrFromString(`HARDWARE\DESCRIPTION\System\BIOS`)
	if err != nil {
		return ""
	}
	var key syscall.Handle
	if syscall.RegOpenKeyEx(syscall.HKEY_LOCAL_MACHINE, path, 0, syscall.KEY_READ, &key) != nil {
		return ""
	}
	defer syscall.RegCloseKey(key)
	vname, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return ""
	}
	buf := make([]uint16, 256)
	n := uint32(len(buf) * 2)
	var typ uint32
	if syscall.RegQueryValueEx(key, vname, nil, &typ, (*byte)(unsafe.Pointer(&buf[0])), &n) != nil || typ != syscall.REG_SZ {
		return ""
	}
	return syscall.UTF16ToString(buf[:n/2])
}
