package projects

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
)

// goos is the running system; tests pass another in Options.GOOS.
var goos = runtime.GOOS

// FS is what the search may do with the file system: list a folder, look up
// one entry (Lstat), read a listed entry's details, resolve a root's link
// once, and ask whether a path is on a network file system. There is no Open:
// the search never opens a file (CTO-38 §1.2, P11).
type FS interface {
	ReadDir(dir string) ([]fs.DirEntry, error)
	Lstat(path string) (fs.FileInfo, error)
	// Info is the details of an entry ReadDir returned (no extra read on
	// Windows; an lstat elsewhere).
	Info(dir string, e fs.DirEntry) (fs.FileInfo, error)
	EvalSymlinks(path string) (string, error)
	// Network reports a path on a network file system or drive.
	Network(path string) bool
}

// OS is the real file system.
func OS() FS { return osFS{} }

type osFS struct{}

func (osFS) ReadDir(dir string) ([]fs.DirEntry, error) { return os.ReadDir(dir) }
func (osFS) Lstat(p string) (fs.FileInfo, error)       { return os.Lstat(p) }
func (osFS) Info(_ string, e fs.DirEntry) (fs.FileInfo, error) {
	return e.Info()
}
func (osFS) EvalSymlinks(p string) (string, error) { return filepath.EvalSymlinks(p) }
func (osFS) Network(p string) bool                 { return networkPath(p) }
