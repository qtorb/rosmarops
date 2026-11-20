//go:build !windows

package windows

import (
	"fmt"
	"io/fs"
	"os"
	"syscall"

	"github.com/qtorb/rosmarops/internal/graph"
)

// fileModeOwner is a file's permission bits in octal and its owner relative
// to the user the review runs as, from its metadata.
func fileModeOwner(info fs.FileInfo) (mode, owner string) {
	mode = fmt.Sprintf("%04o", info.Mode().Perm())
	owner = graph.OwnerUnknown
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		if int(st.Uid) == os.Getuid() {
			owner = graph.OwnerCurrentUser
		} else {
			owner = graph.OwnerOtherUser
		}
	}
	return mode, owner
}
