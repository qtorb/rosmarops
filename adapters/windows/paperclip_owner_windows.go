//go:build windows

package windows

import (
	"io/fs"

	"github.com/qtorb/rosmarops/internal/graph"
)

// fileModeOwner on Windows: no Unix permission bits, and the owner would need
// the file's security descriptor, which this reader does not open, so it is
// unknown.
func fileModeOwner(fs.FileInfo) (mode, owner string) {
	return "", graph.OwnerUnknown
}
