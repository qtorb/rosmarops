//go:build windows

package firstrun

import (
	"io"
	"os"
	"os/exec"

	"github.com/qtorb/rosmarops/internal/winpath"
)

// AskKitPhrase shows the phrase window and returns true when the person typed
// a phrase that opens the key (check reports it). «Not now», closing the
// window, five wrong phrases, Constrained Language Mode, or any failure to
// run PowerShell all return false: the review goes on without a kit and the
// phrase is never asked for on the console (CTO-12 H2, fail closed). The
// window is visible, with no time limit.
func AskKitPhrase(cfg KitConfig, check func(phrase string) bool) bool {
	ps := winpath.PowerShellExe()
	if ps == "" {
		p, err := exec.LookPath("powershell.exe")
		if err != nil {
			return false
		}
		ps = p
	}
	cmd := exec.Command(ps, KitWindowArgs()...)
	cmd.Env = winpath.ChildEnv(os.Environ())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false
	}
	if err := cmd.Start(); err != nil {
		return false
	}
	// The texts first, then the verdict protocol over the same pipes.
	if _, err := io.WriteString(stdin, kitConfigInput(cfg)+"\n"); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return false
	}
	ok := driveKitWindow(stdout, stdin, check, MaxKitAttempts)
	_ = stdin.Close()
	_ = cmd.Wait()
	return ok
}
