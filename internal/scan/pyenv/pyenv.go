// Package pyenv lists the Python packages installed in a set of site-packages
// directories, reading only the directory entries (BV-100, F0a). It opens no
// file: a package's name and version come from its "<name>-<version>.dist-info"
// directory name. It is portable; the platform adapter resolves the real
// site-packages paths (interpreter, user-site, Microsoft Store, pipx, uv,
// conda, and each repository's .venv/venv) and passes them in.
package pyenv

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Package is one installed distribution read at rest: PyPI ecosystem, the name
// and exact version from the .dist-info directory name, and that directory's
// path.
type Package struct {
	Name    string
	Version string
	Path    string
}

// Failed is a site-packages directory that could not be listed (counts as
// coverage not read, never as clean).
type Failed struct {
	Path   string
	Reason string
}

// Result is what the collector read.
type Result struct {
	Packages []Package
	Failed   []Failed
}

// Scan lists, one level deep and without opening any file, the *.dist-info
// directories of each site-packages path in dirs. A path that does not exist
// is skipped silently (nothing is installed there); a path that exists but
// cannot be read is reported in Failed. Duplicate dirs are listed once.
func Scan(dirs []string) Result {
	var res Result
	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			res.Failed = append(res.Failed, Failed{Path: dir, Reason: err.Error()})
			continue
		}
		for _, e := range entries {
			if !isDir(e) || !strings.HasSuffix(e.Name(), ".dist-info") {
				continue
			}
			name, version, ok := splitDistInfo(e.Name())
			if !ok {
				continue
			}
			res.Packages = append(res.Packages, Package{
				Name: name, Version: version, Path: filepath.Join(dir, e.Name()),
			})
		}
	}
	sort.Slice(res.Packages, func(i, j int) bool { return res.Packages[i].Path < res.Packages[j].Path })
	sort.Slice(res.Failed, func(i, j int) bool { return res.Failed[i].Path < res.Failed[j].Path })
	return res
}

// isDir reports whether a dir entry is a directory or a symlink (a
// site-packages entry can be a link; a .dist-info is always a directory).
func isDir(e fs.DirEntry) bool {
	return e.IsDir() || e.Type()&fs.ModeSymlink != 0
}

// splitDistInfo splits "<name>-<version>.dist-info" into name and version.
// The version is the part after the last hyphen (PEP 427 names escape other
// hyphens to underscores, and PEP 440 versions carry none). ok is false for a
// malformed name.
func splitDistInfo(entry string) (name, version string, ok bool) {
	base := strings.TrimSuffix(entry, ".dist-info")
	i := strings.LastIndexByte(base, '-')
	if i <= 0 || i == len(base)-1 {
		return "", "", false
	}
	return base[:i], base[i+1:], true
}
