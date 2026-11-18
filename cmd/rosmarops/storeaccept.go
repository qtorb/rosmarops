package main

import (
	"os"
	"path/filepath"

	"github.com/qtorb/rosmarops/internal/winpkg"
)

// How a binary change was accepted (state integrity.binary_accepted_via, F3).
const (
	acceptedViaBaseline = "baseline"
	acceptedViaConsole  = "console"
	acceptedViaStore    = "store"
	// acceptedViaAdminList: the administrator's fingerprint list of a
	// managed computer holds the new binary with its version.
	acceptedViaAdminList = "admin_list"
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
	return winpkg.Accepts(id, exe, localAppData, binVersion, stateVersion)
}

// semverGreater reports whether a > b by semver precedence (winpkg's).
func semverGreater(a, b string) bool { return winpkg.SemverGreater(a, b) }

// insideDir reports whether p is below dir (winpkg's, for Windows paths).
func insideDir(p, dir string) bool { return winpkg.InsideDir(p, dir) }
