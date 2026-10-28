//go:build !windows

package firstrun

import (
	"fmt"

	"github.com/qtorb/rosmarops/internal/platform"
)

// Pick is Windows-only: elsewhere there is no folder picker, as if
// cancelled (platform.ErrNotApplicable; macOS's is M1b).
func Pick(string) (string, error) {
	return "", fmt.Errorf("folder picker: %w", platform.ErrNotApplicable)
}
