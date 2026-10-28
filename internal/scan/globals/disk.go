package globals

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// maxManifestBytes caps every file read here (CLAUDE.md invariant 6): larger
// files are not opened.
const maxManifestBytes = 2 << 20

// npmGlobalModulesDir returns npm's global node_modules directory, or "" when
// no npm prefix can be resolved (npm is then treated as not installed).
//
// The prefix is resolved as npm documents it
// (https://docs.npmjs.com/cli/v10/configuring-npm/folders#prefix-configuration,
// https://docs.npmjs.com/cli/v10/using-npm/config#environment-variables):
//  1. the npm_config_prefix environment variable (npm matches the name
//     case-insensitively);
//  2. the prefix key of the user npmrc (see userNpmrcPath);
//  3. the platform default: %AppData%\npm on Windows; elsewhere one level
//     above the directory of the node executable.
//
// Global packages live in <prefix>\node_modules on Windows and in
// <prefix>/lib/node_modules elsewhere (same page, "Node Modules").
func npmGlobalModulesDir(home string) (string, error) {
	prefix, ok := envFold("npm_config_prefix")
	if !ok {
		if path := userNpmrcPath(home); path != "" {
			var err error
			prefix, ok, err = npmrcPrefix(path)
			if err != nil {
				return "", err
			}
		}
	}
	if !ok {
		prefix = npmDefaultPrefix(home)
	}
	if prefix == "" {
		return "", nil
	}
	if runtime.GOOS == "windows" {
		return filepath.Join(prefix, "node_modules"), nil
	}
	return filepath.Join(prefix, "lib", "node_modules"), nil
}

// envFold returns a non-empty npm_config_* environment variable, matched
// case-insensitively as npm and pnpm do; the exact spelling wins when several
// are set.
func envFold(name string) (string, bool) {
	if v, ok := os.LookupEnv(name); ok && v != "" {
		return v, true
	}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		if strings.EqualFold(k, name) && v != "" {
			return v, true
		}
	}
	return "", false
}

// userNpmrcPath is the user-level npmrc npm reads: npm_config_userconfig
// when set (case-insensitive), otherwise ~/.npmrc
// (https://docs.npmjs.com/cli/v10/using-npm/config#userconfig). "" when
// neither is known.
func userNpmrcPath(home string) string {
	if v, ok := envFold("npm_config_userconfig"); ok {
		return expandNpmPath(v)
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".npmrc")
}

// UserNpmrcPath is userNpmrcPath for other collectors.
func UserNpmrcPath(home string) string { return userNpmrcPath(home) }

// npmrcPrefix reads only the prefix key of an npmrc file. The file may hold
// auth tokens: every other line is discarded as it is read and no other key is
// kept. A missing file is not an error.
func npmrcPrefix(path string) (string, bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() > maxManifestBytes {
		return "", false, fmt.Errorf("%s: larger than %d bytes, not read", path, maxManifestBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	var prefix string
	found := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxManifestBytes)
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), "=")
		if !ok || strings.TrimSpace(key) != "prefix" {
			continue
		}
		prefix, found = strings.TrimSpace(val), true // last one wins, as in ini files
	}
	if err := sc.Err(); err != nil {
		return "", false, fmt.Errorf("read %s: %w", path, err)
	}
	if !found {
		return "", false, nil
	}
	return expandNpmPath(prefix), true, nil
}

// expandNpmPath applies npm's value rules for a path setting: ${VAR} is
// replaced by the environment variable
// (https://docs.npmjs.com/cli/v10/configuring-npm/npmrc#files); surrounding
// quotes are removed (ini) and a leading ~ is the home directory (path
// settings), as npm does.
func expandNpmPath(v string) string {
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		v = v[1 : len(v)-1]
	}
	v = os.Expand(v, func(name string) string {
		return os.Getenv(name)
	})
	if v == "~" || strings.HasPrefix(v, "~/") || strings.HasPrefix(v, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			v = filepath.Join(home, v[1:])
		}
	}
	return v
}

// npmDefaultPrefix is npm's documented default prefix: %AppData%\npm on
// Windows, and elsewhere the parent of the directory holding the node
// executable found on PATH (symlinks resolved). "" when node is not found.
func npmDefaultPrefix(home string) string {
	if runtime.GOOS == "windows" {
		appData := os.Getenv("APPDATA")
		if appData == "" && home != "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		if appData == "" {
			return ""
		}
		return filepath.Join(appData, "npm")
	}
	node := findOnPath("node")
	if node == "" {
		return ""
	}
	if real, err := filepath.EvalSymlinks(node); err == nil {
		node = real
	}
	return filepath.Dir(filepath.Dir(node))
}

// findOnPath returns the first executable regular file named name in PATH,
// or "". It only stats candidates; nothing is executed.
func findOnPath(name string) string {
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" {
			continue
		}
		p := filepath.Join(dir, name)
		info, err := os.Stat(p)
		if err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0 {
			return p
		}
	}
	return ""
}

// pnpmGlobalModulesDirs returns the node_modules directories of pnpm's global
// packages.
//
// The global directory is, in order:
//  1. the globalDir setting given as npm_config_global_dir (case-insensitive):
//     pnpm 10 reads npm_config_* environment variables
//     (github.com/pnpm/pnpm.io, versioned_docs/version-10.x/npmrc.md) and the
//     setting is globalDir (settings.md#globaldir); observed with pnpm 10.28
//     to take precedence over PNPM_HOME and XDG_DATA_HOME;
//  2. $PNPM_HOME/global when PNPM_HOME is set (observed pnpm 10.28
//     behaviour; the pnpm 10 globalDir documentation does not name it);
//  3. the documented globalDir default
//     (github.com/pnpm/pnpm.io, versioned_docs/version-10.x/settings.md#globaldir):
//     $XDG_DATA_HOME/pnpm/global, or ~/AppData/Local/pnpm/global on Windows,
//     ~/Library/pnpm/global on macOS and ~/.local/share/pnpm/global on Linux.
//
// Packages live in <globalDir>/<layout version>/node_modules; every numeric
// subdirectory is read.
//
// A package installed as a link reports the version from its package.json;
// `pnpm ls -g` shows "link:<path>" instead.
func pnpmGlobalModulesDirs(home string) ([]string, error) {
	globalDir := pnpmGlobalDir(home)
	if globalDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(globalDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", globalDir, err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() && isDigits(e.Name()) {
			dirs = append(dirs, filepath.Join(globalDir, e.Name(), "node_modules"))
		}
	}
	return dirs, nil
}

func pnpmGlobalDir(home string) string {
	if v, ok := envFold("npm_config_global_dir"); ok {
		return expandNpmPath(v)
	}
	if v := os.Getenv("PNPM_HOME"); v != "" {
		return filepath.Join(v, "global")
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "pnpm", "global")
	}
	if home == "" {
		return ""
	}
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(home, "AppData", "Local", "pnpm", "global")
	case "darwin":
		return filepath.Join(home, "Library", "pnpm", "global")
	default:
		return filepath.Join(home, ".local", "share", "pnpm", "global")
	}
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// readGlobalModules lists the top-level packages of a node_modules directory,
// @scope/name included, reading name and version from each package.json.
// Entries starting with "." (.bin, .pnpm, .package-lock.json, ...) are not
// packages. Symlinked packages are followed to read their package.json only;
// nothing below a package directory is walked. A missing directory yields no
// packages; an unreadable package.json is logged by the caller via the
// returned errors and the remaining packages are still returned.
func readGlobalModules(dir string) ([]GlobalPackage, []error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []error{fmt.Errorf("read %s: %w", dir, err)}
	}
	var pkgs []GlobalPackage
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
func readManifest(path string) (GlobalPackage, error) {
	info, err := os.Stat(path)
	if err != nil {
		return GlobalPackage{}, fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() > maxManifestBytes {
		return GlobalPackage{}, fmt.Errorf("%s: larger than %d bytes, not read", path, maxManifestBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return GlobalPackage{}, fmt.Errorf("read %s: %w", path, err)
	}
	var m struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return GlobalPackage{}, fmt.Errorf("parse %s: %w", path, err)
	}
	if m.Name == "" {
		return GlobalPackage{}, fmt.Errorf("%s: no name", path)
	}
	return GlobalPackage{Name: m.Name, Version: m.Version, Path: path}, nil
}
