//go:build linux || darwin

package projects

import (
	"io/fs"
	"syscall"
)

// classify is the reason a listed folder is not entered, from its details
// alone: a link, or another file system than its root's (st_dev).
func classify(info, root fs.FileInfo) Reason {
	if info.Mode()&fs.ModeSymlink != 0 {
		return ReasonLinks
	}
	a, ok1 := info.Sys().(*syscall.Stat_t)
	b, ok2 := root.Sys().(*syscall.Stat_t)
	if ok1 && ok2 && a.Dev != b.Dev {
		return ReasonOtherFS
	}
	return ""
}
