//go:build !windows && !darwin

package firstrun

import (
	"fmt"

	"github.com/qtorb/rosmarops/internal/platform"
)

// Pick is Windows and macOS only: elsewhere there is no folder picker, as
// if cancelled (platform.ErrNotApplicable).
func Pick(string) (string, error) {
	return "", fmt.Errorf("folder picker: %w", platform.ErrNotApplicable)
}
