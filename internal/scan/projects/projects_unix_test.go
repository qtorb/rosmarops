//go:build linux || darwin

package projects

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
)

// otherDev is a FileInfo on another device (P12).
type otherDev struct{ fs.FileInfo }

func (o otherDev) Sys() any {
	st, _ := o.FileInfo.Sys().(*syscall.Stat_t)
	if st == nil {
		return nil
	}
	c := *st
	c.Dev++
	return &c
}

// P12: a folder on another file system is not walked (other_fs); a network
// root is not walked (network); on Windows the cloud and network attributes
// map to their reasons (attrs_windows_test.go).
func TestProjectsOtherFilesystems(t *testing.T) {
	home := t.TempDir()
	mount := filepath.Join(home, "mnt-disk")
	repo(t, filepath.Join(mount, "r"))
	repo(t, filepath.Join(home, "code", "ok"))
	cfs := newCountFS()
	fi, err := os.Lstat(mount)
	if err != nil {
		t.Fatal(err)
	}
	cfs.info[mount] = otherDev{fi}
	res := Search(Options{Home: home, SearchHome: true, GOOS: runtime.GOOS, FS: cfs})
	if got := paths(res.Items); len(got) != 1 || filepath.Base(got[0]) != "ok" {
		t.Fatalf("found %v", got)
	}
	if n := skipped(res, ReasonOtherFS); n == nil || n.Paths[0] != mount {
		t.Errorf("other_fs %+v", n)
	}
	if len(cfs.readUnder(mount)) != 0 {
		t.Error("read inside another file system")
	}
	// A network root is not walked.
	netRoot := t.TempDir()
	repo(t, filepath.Join(netRoot, "x"))
	cfs.network[netRoot] = true
	res = Search(Options{Roots: []string{netRoot}, GOOS: runtime.GOOS, FS: cfs})
	if len(cfs.readUnder(netRoot)) != 0 || skipped(res, ReasonNetwork) == nil {
		t.Errorf("network root walked: %+v", res)
	}
	// A UNC root (Windows rules) is not walked either.
	res = Search(Options{Roots: []string{`\\server\share\code`}, GOOS: "windows", FS: cfs})
	if n := skipped(res, ReasonNetwork); n == nil {
		t.Errorf("UNC root: %+v", res.Skipped)
	}
}
