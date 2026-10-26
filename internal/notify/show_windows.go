//go:build windows

package notify

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/proc"
)

// timeout bounds the PowerShell call.
const timeout = 30 * time.Second

// Run runs the script with toastXML on its standard input (empty: only
// read the setting) and returns ToastNotifier.Setting.
func Run(ctx context.Context, toastXML string) (string, error) {
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		return "", fmt.Errorf("notify: powershell.exe not found: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", Args()...)
	proc.Hide(cmd)
	cmd.Stdin = strings.NewReader(toastXML)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("notify: %w; stderr: %s", err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// OpenReport is plan B (J9): when Windows does not show the notification,
// it opens the report with explorer.exe and one argument, the report's
// absolute path, with no shell; Windows picks the browser. explorer.exe's
// exit code says nothing about the outcome, so only a failure to start it
// is an error. It gets no hidden window: the browser must show.
func OpenReport(path string) error {
	exe := "explorer.exe"
	if root := os.Getenv("SystemRoot"); root != "" {
		exe = filepath.Join(root, "explorer.exe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, path)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("notify: start explorer.exe: %w", err)
	}
	_ = cmd.Wait() // its exit code is not meaningful
	return nil
}
