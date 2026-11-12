package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/qtorb/rosmarops/internal/winpkg"
)

// How a binary change was accepted (state integrity.binary_accepted_via, F3).
const (
	acceptedViaBaseline = "baseline"
	acceptedViaConsole  = "console"
	acceptedViaStore    = "store"
)

// storeUpdate is a Microsoft Store update the integrity check accepted, for
// the report (report.integrity.binary_store).
type storeUpdate struct {
	Old, New string // versions
}

// The process's package identity and real executable; tests replace them.
var (
	currentPackage = winpkg.Current
	currentExe     = func() (string, error) {
		p, err := os.Executable()
		if err != nil {
			return "", err
		}
		return filepath.EvalSymlinks(p)
	}
)

// storeAccepts reports whether a changed binary is a Microsoft Store update of
// RosmarOps itself that the integrity check accepts without asking (invariant
// 8, CTO-41 §1.2 B, signed by Albert 12-11 17:16). All must hold:
//  1. the process's package family is RosmarOps's own (winpkg.Family);
//  2. its real executable is inside the package's own installed folder as
//     Windows reports it (GetCurrentPackagePath), never the %LOCALAPPDATA%
//     alias;
//  3. the package version equals the binary's version (0.11.3.0 for 0.11.3);
//  4. the binary's version is greater than the state's engine_version
//     (strict semver): never an older version, never the same version with
//     another fingerprint, never with no recorded version.
//
// The fifth condition (a readable state with references) is the caller's: the
// check only runs in the branch of a changed binary against a recorded one.
func storeAccepts(id winpkg.Identity, exe, localAppData, binVersion, stateVersion string) bool {
	if id.FamilyName != winpkg.Family || id.Path == "" || exe == "" {
		return false
	}
	if !insideDir(exe, id.Path) {
		return false
	}
	if localAppData != "" && insideDir(exe, filepath.Join(localAppData, "Microsoft", "WindowsApps")) {
		return false
	}
	core, pre, ok := parseSemver(binVersion)
	if !ok || pre != "" || winpkg.Version(id.FullName) != core+".0" {
		return false
	}
	return semverGreater(binVersion, stateVersion)
}

// insideDir reports whether p is below dir, without regard to case or to the
// slash used (Windows paths).
func insideDir(p, dir string) bool {
	norm := func(s string) string {
		return strings.TrimRight(strings.ToLower(strings.ReplaceAll(s, "/", `\`)), `\`)
	}
	np, nd := norm(p), norm(dir)
	return nd != "" && strings.HasPrefix(np, nd+`\`)
}

// parseSemver splits a strict semver («1.2.3» or «1.2.3-pre») into its numeric
// core and prerelease.
func parseSemver(v string) (core, pre string, ok bool) {
	core, pre, _ = strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return "", "", false
	}
	for _, p := range parts {
		if p == "" || (len(p) > 1 && p[0] == '0') {
			return "", "", false
		}
		if _, err := strconv.Atoi(p); err != nil {
			return "", "", false
		}
	}
	return core, pre, true
}

// semverGreater reports whether a > b by semver precedence; false when either
// is not a version (an empty engine_version is never older).
func semverGreater(a, b string) bool {
	ca, pa, okA := parseSemver(a)
	cb, pb, okB := parseSemver(b)
	if !okA || !okB {
		return false
	}
	xa, xb := strings.Split(ca, "."), strings.Split(cb, ".")
	for i := 0; i < 3; i++ {
		na, _ := strconv.Atoi(xa[i])
		nb, _ := strconv.Atoi(xb[i])
		if na != nb {
			return na > nb
		}
	}
	// Same core: a release is greater than its prereleases; two prereleases
	// or two releases are not ordered here (never greater).
	return pa == "" && pb != ""
}
