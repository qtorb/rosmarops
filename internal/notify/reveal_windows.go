//go:build windows

package notify

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"

	"github.com/qtorb/rosmarops/internal/winpath"
)

// RevealInExplorer opens Explorer with the file already selected (BV-131,
// CTO-12 H4): explorer.exe by its absolute system path, with the literal
// command line `explorer.exe /select,"<path>"` set as SysProcAttr.CmdLine so
// the quoting is ours. It is not opened for a path with a double quote or a
// UNC path, nor when the absolute Explorer path cannot be resolved — it never
// falls back to a bare "explorer.exe" that %PATH% could redirect (BV-135,
// CTO-12 H4). Explorer's exit code is not meaningful, so only a failure to
// start it is an error.
func RevealInExplorer(path string) error {
	if err := revealPathOK(path); err != nil {
		return err
	}
	exe, err := revealExe(winpath.ExplorerExe())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe)
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: revealCmdLine(exe, path)}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("reveal: start explorer.exe: %w", err)
	}
	_ = cmd.Wait()
	return nil
}
