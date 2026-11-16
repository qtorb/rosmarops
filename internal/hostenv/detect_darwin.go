//go:build darwin

package hostenv

import "syscall"

// Detect is this computer's environment: on macOS only a virtual machine,
// from kern.hv_vmm_present (a sysctl value, not a program). macOS has no
// containers of its own.
func Detect() *Env {
	if v, err := syscall.SysctlUint32("kern.hv_vmm_present"); err == nil && v == 1 {
		return &Env{Kind: KindVirtualMachine, Signal: SignalHVVMMPresent, VMType: Unidentified}
	}
	return nil
}
