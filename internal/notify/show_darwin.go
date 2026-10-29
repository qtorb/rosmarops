//go:build darwin

package notify

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/qtorb/rosmarops/internal/platform"
)

// Run is Windows-only: macOS has no notification to show yet
// (platform.ErrNotApplicable).
func Run(context.Context, string) (string, error) {
	return "", fmt.Errorf("notify: %w", platform.ErrNotApplicable)
}

// OpenReport opens the report in the browser with /usr/bin/open, where
// Windows uses explorer.exe (decision 2026-10-29, macOS vista previa,
// point 1).
func OpenReport(path string) error { return openWith(execRunner, path) }

// execRunner runs the program with exec: no shell, the caller's environment.
func execRunner(ctx context.Context, name string, args ...string) error {
	return exec.CommandContext(ctx, name, args...).Run()
}
