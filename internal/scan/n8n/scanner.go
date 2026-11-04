// Package n8n reads an n8n user folder at rest (BV-175): no network, no writes.
// n8n is a workflow automation tool; its user folder holds the database of the
// credentials it connects to (email, payments, databases) and, when not moved
// to an environment variable, the key that encrypts them. RosmarOps reports
// only the presence of that key beside the database, never its value
// (invariant 3), and lists the community node packages so the threat-intel
// cross can flag a known-malicious one, like any global npm package.
package n8n

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// maxFileBytes is the size above which the config or a manifest is not opened
// (invariant 6: files over the content-scan limit are not read). The config
// and package.json files are tiny; this only guards against a pathological one.
const maxFileBytes = 1 << 20 // 1 MiB

// Package is a community node package installed in the n8n folder: its name,
// version and the package.json path. Never a credential value.
type Package struct {
	Name    string
	Version string
	Path    string
}

// Result is what one n8n user folder yields.
type Result struct {
	// Present is true when the folder exists (used to detect n8n as an agent).
	Present bool
	// Folder is the folder as read.
	Folder string
	// KeyPresent is true when the folder's `config` file holds an
	// `encryptionKey` entry. Only the presence is recorded, never the value.
	KeyPresent bool
	// DBPresent is true when the folder holds `database.sqlite`.
	DBPresent bool
	// Packages are the community node packages under nodes/node_modules.
	Packages []Package
}

// KeyAndData reports the n8n_key_with_data condition (BV-175): the credential
// encryption key and the database live together in the folder.
func (r Result) KeyAndData() bool { return r.KeyPresent && r.DBPresent }

// Scan reads the n8n user folder at folder. It opens no socket and writes
// nothing. A missing folder gives Present=false and no error; unreadable
// entries are returned as errors with the rest of the result still filled.
func Scan(folder string) (Result, []error) {
	res := Result{Folder: folder}
	info, err := os.Stat(folder)
	if errors.Is(err, fs.ErrNotExist) {
		return res, nil
	}
	if err != nil {
		return res, []error{fmt.Errorf("stat %s: %w", folder, err)}
	}
	if !info.IsDir() {
		return res, nil
	}
	res.Present = true

	var errs []error
	key, err := configHasEncryptionKey(filepath.Join(folder, "config"))
	if err != nil {
		errs = append(errs, err)
	}
	res.KeyPresent = key

	if fi, err := os.Stat(filepath.Join(folder, "database.sqlite")); err == nil && !fi.IsDir() {
		res.DBPresent = true
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		errs = append(errs, fmt.Errorf("stat database.sqlite: %w", err))
	}

	pkgs, perrs := readModules(filepath.Join(folder, "nodes", "node_modules"))
	res.Packages = pkgs
	errs = append(errs, perrs...)
	return res, errs
}

// configHasEncryptionKey reports whether the n8n `config` file holds a
// top-level `encryptionKey` entry. The value is never read into any output: the
// file is parsed into raw members and only the presence of the key is returned
// (invariant 3). A missing file is not an error.
func configHasEncryptionKey(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.IsDir() || info.Size() > maxFileBytes {
		return false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read %s: %w", path, err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return false, nil // not JSON we understand: no key recorded
	}
	_, ok := m["encryptionKey"]
	return ok, nil
}

// readModules lists the packages of nodes/node_modules the same way the global
// npm scanner does: top-level entries and @scope/name, reading only name and
// version from each package.json. A missing directory yields no packages.
func readModules(dir string) ([]Package, []error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []error{fmt.Errorf("read %s: %w", dir, err)}
	}
	var pkgs []Package
	var errs []error
	add := func(pkgDir string) {
		p, err := readManifest(filepath.Join(pkgDir, "package.json"))
		if err != nil {
			errs = append(errs, err)
			return
		}
		pkgs = append(pkgs, p)
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if strings.HasPrefix(name, "@") {
			scoped, err := os.ReadDir(filepath.Join(dir, name))
			if err != nil {
				errs = append(errs, fmt.Errorf("read %s: %w", filepath.Join(dir, name), err))
				continue
			}
			for _, s := range scoped {
				if !strings.HasPrefix(s.Name(), ".") {
					add(filepath.Join(dir, name, s.Name()))
				}
			}
			continue
		}
		add(filepath.Join(dir, name))
	}
	return pkgs, errs
}

// readManifest reads only name and version from a package.json.
func readManifest(path string) (Package, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Package{}, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() > maxFileBytes {
		return Package{}, fmt.Errorf("%s: larger than %d bytes, not read", path, maxFileBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Package{}, fmt.Errorf("read %s: %w", path, err)
	}
	var m struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return Package{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if m.Name == "" {
		return Package{}, fmt.Errorf("%s: no name", path)
	}
	return Package{Name: m.Name, Version: m.Version, Path: path}, nil
}
