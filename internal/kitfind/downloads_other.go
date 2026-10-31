//go:build !windows

package kitfind

import (
	"os"

	"github.com/qtorb/rosmarops/internal/platform"
)

// DownloadsDir is Windows only: the key-from-Downloads flow is the Windows
// double-click onboarding (CTO-12 H3). Elsewhere it does not apply.
func DownloadsDir() (string, error) {
	return "", platform.ErrNotApplicable
}

// special has no extra attributes to check off Windows: os.Lstat's regular-
// file test (in Scan) already excludes links, directories and devices.
func special(os.FileInfo) bool { return false }
