package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// fakeBrewFS is a fake file system: links maps a symbolic link to its
// target, files are regular files; EvalSymlinks follows links to the end.
type fakeBrewFS struct {
	links map[string]string
	files map[string]bool
}

type fakeInfo struct {
	name string
	mode fs.FileMode
}

func (i fakeInfo) Name() string       { return i.name }
func (i fakeInfo) Size() int64        { return 0 }
func (i fakeInfo) Mode() fs.FileMode  { return i.mode }
func (i fakeInfo) ModTime() time.Time { return time.Time{} }
func (i fakeInfo) IsDir() bool        { return false }
func (i fakeInfo) Sys() any           { return nil }

func (f fakeBrewFS) Lstat(name string) (fs.FileInfo, error) {
	if _, ok := f.links[name]; ok {
		return fakeInfo{name, fs.ModeSymlink | 0o755}, nil
	}
	if f.files[name] {
		return fakeInfo{name, 0o755}, nil
	}
	return nil, fs.ErrNotExist
}

func (f fakeBrewFS) EvalSymlinks(name string) (string, error) {
	for i := 0; i < 8; i++ {
		t, ok := f.links[name]
		if !ok {
			break
		}
		name = t
	}
	if !f.files[name] {
		return "", fs.ErrNotExist
	}
	return name, nil
}

func TestBrewStableExe(t *testing.T) {
	const (
		linuxKeg  = "/home/linuxbrew/.linuxbrew/Cellar/rosmarops/0.12.0/bin/rosmarops"
		linuxLink = "/home/linuxbrew/.linuxbrew/bin/rosmarops"
		macKeg    = "/opt/homebrew/Cellar/rosmarops/0.12.0/bin/rosmarops"
		macLink   = "/opt/homebrew/bin/rosmarops"
		oldKeg    = "/opt/homebrew/Cellar/rosmarops/0.11.7/bin/rosmarops"
	)
	cases := []struct {
		name string
		real string
		goos string
		fsys fakeBrewFS
		want string
	}{
		{
			name: "Linux Cellar with the right link: stable path",
			real: linuxKeg, goos: "linux",
			fsys: fakeBrewFS{links: map[string]string{linuxLink: linuxKeg}, files: map[string]bool{linuxKeg: true}},
			want: linuxLink,
		},
		{
			name: "macOS Cellar with the right link: stable path",
			real: macKeg, goos: "darwin",
			fsys: fakeBrewFS{links: map[string]string{macLink: macKeg}, files: map[string]bool{macKeg: true}},
			want: macLink,
		},
		{
			name: "Cellar without the link: unchanged",
			real: macKeg, goos: "darwin",
			fsys: fakeBrewFS{files: map[string]bool{macKeg: true}},
			want: macKeg,
		},
		{
			name: "Cellar with a link to another version: unchanged",
			real: macKeg, goos: "darwin",
			fsys: fakeBrewFS{links: map[string]string{macLink: oldKeg}, files: map[string]bool{macKeg: true, oldKeg: true}},
			want: macKeg,
		},
		{
			name: "Cellar with a link to something else: unchanged",
			real: macKeg, goos: "darwin",
			fsys: fakeBrewFS{links: map[string]string{macLink: "/usr/local/bin/other"}, files: map[string]bool{macKeg: true, "/usr/local/bin/other": true}},
			want: macKeg,
		},
		{
			name: "Cellar with a dangling link: unchanged",
			real: macKeg, goos: "darwin",
			fsys: fakeBrewFS{links: map[string]string{macLink: "/nowhere"}, files: map[string]bool{macKeg: true}},
			want: macKeg,
		},
		{
			name: "a regular file at the stable path, not a link: unchanged",
			real: macKeg, goos: "darwin",
			fsys: fakeBrewFS{files: map[string]bool{macKeg: true, macLink: true}},
			want: macKeg,
		},
		{
			name: "outside the Cellar: unchanged",
			real: "/home/albert/bin/rosmarops", goos: "linux",
			fsys: fakeBrewFS{links: map[string]string{"/home/albert/bin/rosmarops": "/x"}, files: map[string]bool{"/x": true}},
			want: "/home/albert/bin/rosmarops",
		},
		{
			name: "another formula's Cellar: unchanged",
			real: "/opt/homebrew/Cellar/other/1.0/bin/rosmarops", goos: "darwin",
			fsys: fakeBrewFS{links: map[string]string{macLink: "/opt/homebrew/Cellar/other/1.0/bin/rosmarops"}, files: map[string]bool{"/opt/homebrew/Cellar/other/1.0/bin/rosmarops": true}},
			want: "/opt/homebrew/Cellar/other/1.0/bin/rosmarops",
		},
		{
			name: "Windows: unchanged",
			real: macKeg, goos: "windows",
			fsys: fakeBrewFS{links: map[string]string{macLink: macKeg}, files: map[string]bool{macKeg: true}},
			want: macKeg,
		},
		{
			name: "Windows path: unchanged",
			real: `C:\Users\albert\Downloads\rosmarops.exe`, goos: "windows",
			fsys: fakeBrewFS{},
			want: `C:\Users\albert\Downloads\rosmarops.exe`,
		},
		{
			name: "empty: unchanged",
			real: "", goos: "linux", fsys: fakeBrewFS{}, want: "",
		},
	}
	for _, c := range cases {
		if got := brewStableExe(c.real, c.goos, c.fsys); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// TestBrewStableExeRealFS runs brewStableExe on a real Homebrew-shaped
// layout under a temporary folder, with the relative link Homebrew creates.
func TestBrewStableExeRealFS(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Homebrew layout: macOS and Linux")
	}
	prefix := t.TempDir()
	keg := filepath.Join(prefix, "Cellar", "rosmarops", "0.12.0", "bin")
	if err := os.MkdirAll(keg, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(keg, "rosmarops")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(prefix, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(prefix, "bin", "rosmarops")
	if err := os.Symlink("../Cellar/rosmarops/0.12.0/bin/rosmarops", link); err != nil {
		t.Fatal(err)
	}
	// os.Executable() on Linux resolves links: the real path, with the temp
	// folder's own links resolved too.
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		t.Fatal(err)
	}
	stable := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(real))))), "bin", "rosmarops")
	if got := brewStableExe(real, runtime.GOOS, osBrewFS{}); got != stable {
		t.Errorf("got %q, want %q", got, stable)
	}
	// After an upgrade the link names another keg: unchanged.
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../Cellar/rosmarops/0.12.1/bin/rosmarops", link); err != nil {
		t.Fatal(err)
	}
	if got := brewStableExe(real, runtime.GOOS, osBrewFS{}); got != real {
		t.Errorf("dangling link: got %q, want %q", got, real)
	}
}
