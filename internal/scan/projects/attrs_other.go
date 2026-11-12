//go:build !linux && !darwin && !windows

package projects

import "io/fs"

// classify: only links are known here.
func classify(info, _ fs.FileInfo) Reason {
	if info.Mode()&fs.ModeSymlink != 0 {
		return ReasonLinks
	}
	return ""
}

func networkPath(string) bool { return false }
