//go:build !windows

package notify

import (
	"context"
	"fmt"

	"github.com/qtorb/rosmarops/internal/platform"
)

// Run is Windows-only: elsewhere there is no notification to show
// (platform.ErrNotApplicable; macOS's is M1b).
func Run(context.Context, string) (string, error) {
	return "", fmt.Errorf("notify: %w", platform.ErrNotApplicable)
}

// OpenReport is Windows-only: elsewhere no report is opened
// (platform.ErrNotApplicable; macOS's is M1b).
func OpenReport(string) error {
	return fmt.Errorf("notify: open report: %w", platform.ErrNotApplicable)
}
