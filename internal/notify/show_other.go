//go:build !windows && !darwin

package notify

import (
	"context"
	"fmt"

	"github.com/qtorb/rosmarops/internal/platform"
)

// Run is Windows-only: elsewhere there is no notification to show
// (platform.ErrNotApplicable).
func Run(context.Context, string) (string, error) {
	return "", fmt.Errorf("notify: %w", platform.ErrNotApplicable)
}

// OpenReport is Windows and macOS only: elsewhere no report is opened
// (platform.ErrNotApplicable).
func OpenReport(string) error {
	return fmt.Errorf("notify: open report: %w", platform.ErrNotApplicable)
}
