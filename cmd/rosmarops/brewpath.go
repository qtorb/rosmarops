package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

// brewFS is what brewStableExe reads: a link's own entry and where a path
// resolves. Only reads; os in the program, a fake in the tests.
type brewFS interface {
	Lstat(name string) (fs.FileInfo, error)
	EvalSymlinks(name string) (string, error)
}

type osBrewFS struct{}

func (osBrewFS) Lstat(name string) (fs.FileInfo, error)   { return os.Lstat(name) }
func (osBrewFS) EvalSymlinks(name string) (string, error) { return filepath.EvalSymlinks(name) }

// brewCellar matches a Homebrew keg's binary: <prefix>/Cellar/rosmarops/<version>/bin/rosmarops.
var brewCellar = regexp.MustCompile(`^(/.+)/Cellar/rosmarops/[^/]+/bin/rosmarops$`)

// brewStableExe (BV-243): from a Homebrew install, os.Executable() names the
// versioned keg in the Cellar, which `brew upgrade` removes; the weekly-review
// command the report gives must name Homebrew's stable link instead,
// <prefix>/bin/rosmarops, where <prefix> is the part before /Cellar/. Only
// when that link exists and resolves to the same file as real; otherwise, on
// Windows, or outside a Cellar, real is returned unchanged. The integrity
// check still hashes the real running binary (hash.Self).
func brewStableExe(real, goos string, fsys brewFS) string {
	if real == "" || goos == "windows" {
		return real
	}
	m := brewCellar.FindStringSubmatch(real)
	if m == nil {
		return real
	}
	stable := m[1] + "/bin/rosmarops"
	info, err := fsys.Lstat(stable)
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		return real
	}
	got, err := fsys.EvalSymlinks(stable)
	if err != nil {
		return real
	}
	want, err := fsys.EvalSymlinks(real)
	if err != nil || got != want {
		return real
	}
	return stable
}
