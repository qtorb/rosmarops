//go:build !windows

package proc

import "os/exec"

// Hide makes cmd start without a window: nothing to do outside Windows.
func Hide(*exec.Cmd) {}
