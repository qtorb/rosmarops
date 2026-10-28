package windows

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/proc"
	"github.com/qtorb/rosmarops/internal/winpath"
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
	// BV-93, B4: powershell.exe by the absolute path the Windows API gives,
	// never one found on %PATH%. Off Windows the API gives nothing and we fall
	// back to PATH; when PowerShell is not there the feature is unavailable.
	exe := winpath.PowerShellExe()
	if exe == "" {
		p, err := exec.LookPath("powershell.exe")
		if err != nil {
			return nil, nil
		}
		exe = p
	}

	callCtx, cancel := context.WithTimeout(ctx, psTimeout)
	defer cancel()

	cmd := exec.CommandContext(callCtx, exe,
		"-NonInteractive", "-NoProfile",
		"-Command", command)
	proc.Hide(cmd)         // no window of its own (J3)
	cmd.Env = psChildEnv() // BV-93, B4: PSModulePath = system modules only
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

// importModule is a PowerShell prefix that loads the named system module by
// its absolute path (BV-93, B4), so the exact system module loads and not one
// reached through PSModulePath. Off Windows it is empty: there runPS returns
// nil before running anything. A system module path never contains a single
// quote, so single-quoting it is safe.
func importModule(name string) string {
	dir := winpath.Module(name)
	if dir == "" {
		return ""
	}
	return "Import-Module '" + dir + "';"
}

// psChildEnv is the environment of every PowerShell child: the inherited one
// with PSModulePath narrowed to the single system module directory (BV-93,
// B4). Off Windows it is the inherited environment unchanged; PowerShell is
// unavailable there anyway.
func psChildEnv() []string {
	if mods := winpath.SystemModules(); mods == "" {
		return nil // inherit unchanged off Windows
	}
	return winpath.ChildEnv(os.Environ())
}
