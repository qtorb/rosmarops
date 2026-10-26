package windows

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/proc"
)

// psTimeout is the default wall-clock limit for any single PowerShell call.
// Kept short: if PS hangs, the caller logs a warning and returns nil.
const psTimeout = 30 * time.Second

// subprocessWaitDelay bounds how long a process killed by its context may
// keep its output pipes open (a child it started can hold them): after it,
// the pipes are closed and the call returns.
// https://pkg.go.dev/os/exec#Cmd.WaitDelay
const subprocessWaitDelay = 2 * time.Second

// runPS executes a PowerShell command and returns its stdout as raw bytes.
//
// Invokes powershell.exe with -NonInteractive and -NoProfile to:
//   - avoid blocking on user prompts,
//   - avoid recursively executing the very profile we may be auditing.
//
// On non-Windows (or when powershell.exe is not in PATH) returns nil, nil
// so callers treat missing PowerShell as "feature unavailable" rather than
// an error — this keeps the scan functional on CI and cross-platform builds.
//
// The call ends at psTimeout or at ctx's deadline (the global one),
// whichever comes first; callers may wrap with a tighter deadline if needed.
func runPS(ctx context.Context, command string) ([]byte, error) {
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		// PowerShell not available (non-Windows CI, WSL without interop, etc.)
		return nil, nil
	}

	callCtx, cancel := context.WithTimeout(ctx, psTimeout)
	defer cancel()

	cmd := exec.CommandContext(callCtx, "powershell.exe",
		"-NonInteractive", "-NoProfile",
		"-Command", command)
	proc.Hide(cmd) // no window of its own (J3)
	cmd.WaitDelay = subprocessWaitDelay

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			return nil, fmt.Errorf("%w; stderr: %s", err, stderrStr)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}
