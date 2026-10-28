package windows

import (
	"os"
	"path/filepath"
)

// PythonSitePackages returns the site-packages directories to inspect for
// installed Python packages (BV-100, F0a): the interpreters' and user site,
// the Microsoft Store Python, pipx, uv tools, conda (base and envs), and each
// repository's .venv and venv. Wildcards are expanded; non-existent paths are
// returned too and skipped by the collector. Reading these is a directed
// listing of known folders, not a recursive walk, so the forced exclusions are
// untouched. The layout is Windows's (Lib\site-packages); the error is always
// nil on Windows (macOS returns platform.ErrNotApplicable instead, M1b).
func (w *WindowsPlatform) PythonSitePackages(repoRoots []string) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	localAppData := envOr("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	appData := envOr("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	programFiles := envOr("ProgramFiles", `C:\Program Files`)

	var globs []string
	// python.org: all-users and per-user interpreters, and the user site.
	globs = append(globs,
		filepath.Join(programFiles, "Python3*", "Lib", "site-packages"),
		filepath.Join(localAppData, "Programs", "Python", "Python3*", "Lib", "site-packages"),
		filepath.Join(appData, "Python", "Python3*", "site-packages"),
		// Microsoft Store Python (redirected user site).
		filepath.Join(localAppData, "Packages", "PythonSoftwareFoundation.Python.*", "LocalCache", "local-packages", "Python3*", "site-packages"),
		// pipx and uv tool venvs.
		filepath.Join(localAppData, "pipx", "venvs", "*", "Lib", "site-packages"),
		filepath.Join(localAppData, "uv", "tools", "*", "Lib", "site-packages"),
	)
	// conda: base and named environments.
	for _, base := range []string{filepath.Join(home, "miniconda3"), filepath.Join(home, "anaconda3")} {
		globs = append(globs,
			filepath.Join(base, "Lib", "site-packages"),
			filepath.Join(base, "envs", "*", "Lib", "site-packages"),
		)
	}

	var dirs []string
	for _, g := range globs {
		matches, err := filepath.Glob(g)
		if err != nil || len(matches) == 0 {
			continue
		}
		dirs = append(dirs, matches...)
	}
	// Each repository's project virtualenv: a single, known directory per root
	// (not the excluded recursive walk).
	for _, root := range repoRoots {
		dirs = append(dirs,
			filepath.Join(root, ".venv", "Lib", "site-packages"),
			filepath.Join(root, "venv", "Lib", "site-packages"),
		)
	}
	return dirs, nil
}

// envOr returns the environment variable value, or fallback when it is unset
// or empty.
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}
