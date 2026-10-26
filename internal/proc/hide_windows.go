//go:build windows

package proc

import (
	"os/exec"
	"syscall"
)

// createNoWindow is CREATE_NO_WINDOW: the console child gets no console
// window (https://learn.microsoft.com/windows/win32/procthread/process-creation-flags).
const createNoWindow = 0x08000000

// Hide makes cmd start without a window.
func Hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
