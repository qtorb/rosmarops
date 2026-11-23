//go:build linux || darwin

package company

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// testOwnerUID replaces root as the trusted owner in test builds only (a
// string for -ldflags -X); testStop, when set, is the folder where the walk
// to the root stops. Both are empty in a release build.
var (
	testOwnerUID string
	testStop     string
)

func trustedUID() uint32 {
	if testOwnerUID != "" {
		if n, err := strconv.ParseUint(testOwnerUID, 10, 32); err == nil {
			return uint32(n)
		}
	}
	return 0
}

// checkTrusted holds when path is a regular file, not a link, and the file
// and every folder up to / belong to root with no write for group or others.
func checkTrusted(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a regular file", ErrUntrusted, path)
	}
	uid := trustedUID()
	for p := path; ; {
		if err := ownedByRoot(p, uid); err != nil {
			return err
		}
		parent := filepath.Dir(p)
		if parent == p || (testStop != "" && p == filepath.Clean(testStop)) {
			return nil
		}
		p = parent
	}
}

func ownedByRoot(p string, uid uint32) error {
	info, err := os.Lstat(p)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("%w: %s: no owner", ErrUntrusted, p)
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return fmt.Errorf("%w: %s is a link", ErrUntrusted, p)
	case st.Uid != uid:
		return fmt.Errorf("%w: %s is not owned by root", ErrUntrusted, p)
	case info.Mode().Perm()&0o022 != 0:
		return fmt.Errorf("%w: %s is writable by group or others", ErrUntrusted, p)
	}
	return nil
}
