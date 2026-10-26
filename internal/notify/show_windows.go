//go:build windows

package notify

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
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
