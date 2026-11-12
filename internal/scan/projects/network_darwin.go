//go:build darwin

package projects

import (
	"strings"
	"syscall"
)

// networkFS are macOS network file system type names (statfs f_fstypename).
var networkFS = map[string]bool{"nfs": true, "smbfs": true, "afpfs": true, "webdav": true, "cifs": true, "ftp": true}

// networkPath reports a path on a network or FUSE file system (statfs).
func networkPath(p string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(p, &st); err != nil {
		return false
	}
	var b []byte
	for _, c := range st.Fstypename {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	name := string(b)
	return networkFS[name] || strings.HasPrefix(name, "fuse") || strings.HasPrefix(name, "macfuse") || strings.HasPrefix(name, "osxfuse")
}
