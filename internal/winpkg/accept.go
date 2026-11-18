package winpkg

import (
	"path/filepath"
	"strconv"
	"strings"
)

// Accepts reports whether a changed binary is a Microsoft Store update of
// RosmarOps itself that the integrity check accepts without asking (invariant
// 8). All must hold:
//  1. the process's package family is RosmarOps's own (Family);
//  2. its real executable is inside the package's own installed folder as
//     Windows reports it (GetCurrentPackagePath), never the %LOCALAPPDATA%
//     alias;
//  3. the package version equals the binary's version (0.11.3.0 for 0.11.3);
//  4. the binary's version is greater than the state's engine_version
//     (strict semver): never an older version, never the same version with
//     another fingerprint, never with no recorded version.
//
// The fifth condition (a readable state with references) is the caller's.
// It is a pure function, so it is tested on every system.
func Accepts(id Identity, exe, localAppData, binVersion, stateVersion string) bool {
	if !IdentityIsOwn(id, exe, localAppData) {
		return false
	}
	core, pre, ok := parseSemver(binVersion)
	if !ok || pre != "" || Version(id.FullName) != core+".0" {
		return false
	}
	return SemverGreater(binVersion, stateVersion)
}

// IdentityIsOwn is conditions 1 and 2 of Accepts: the process runs with
// RosmarOps's own package identity (Family) and its real executable is inside
// that package's installed folder as Windows reports it, never the
// %LOCALAPPDATA% alias. The usage statistics report the install channel
// «store» with this same check, so the two can never disagree.
func IdentityIsOwn(id Identity, exe, localAppData string) bool {
	if id.FamilyName != Family || id.Path == "" || exe == "" {
		return false
	}
	if !InsideDir(exe, id.Path) {
		return false
	}
	return localAppData == "" || !InsideDir(exe, filepath.Join(localAppData, "Microsoft", "WindowsApps"))
}

// InsideDir reports whether p is below dir, without regard to case or to the
// slash used (Windows paths).
func InsideDir(p, dir string) bool {
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

// SemverGreater reports whether a > b by semver precedence; false when either
// is not a version (an empty engine_version is never older).
func SemverGreater(a, b string) bool {
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
