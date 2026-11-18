package trust_test

// The Microsoft Store path of the integrity check (invariant 8): a changed
// binary is accepted without asking only when it runs with RosmarOps's own
// package identity, from the package's own installed folder (never the
// %LOCALAPPDATA% alias, never another folder), with the package version equal
// to the binary's and newer than the recorded one. The check is a pure
// function, so it is proved here on every system with the identity given.

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/winpkg"
)

func TestStoreUpdateIntegrity(t *testing.T) {
	const (
		lad     = `C:\Users\ana\AppData\Local`
		pkgFull = "AlbertGarciaPujadas.RosmarOps_0.11.4.0_x64__2a9trf99pxwv0"
		pkgPath = `C:\Program Files\WindowsApps\` + pkgFull
		pkgExe  = pkgPath + `\rosmarops.exe`
		alias   = lad + `\Microsoft\WindowsApps\rosmarops.exe`
	)
	id := func(family, full, path string) winpkg.Identity {
		return winpkg.Identity{FamilyName: family, FullName: full, Path: path}
	}
	own := id(winpkg.Family, pkgFull, pkgPath)
	for _, c := range []struct {
		name               string
		id                 winpkg.Identity
		exe, bin, stateVer string
		want               bool
	}{
		{"own family, package folder under WindowsApps, newer version", own, pkgExe, "0.11.4", "0.11.3", true},
		{"another family", id("Evil.RosmarOps_2a9trf99pxwv0", pkgFull, pkgPath), pkgExe, "0.11.4", "0.11.3", false},
		{"no package", winpkg.Identity{}, pkgExe, "0.11.4", "0.11.3", false},
		{"the %LOCALAPPDATA% alias", own, alias, "0.11.4", "0.11.3", false},
		{"the alias reported as the package folder", id(winpkg.Family, pkgFull, lad+`\Microsoft\WindowsApps`), alias, "0.11.4", "0.11.3", false},
		{"another folder", own, `C:\Users\ana\Downloads\rosmarops.exe`, "0.11.4", "0.11.3", false},
		{"an older version", id(winpkg.Family, "AlbertGarciaPujadas.RosmarOps_0.11.1.0_x64__2a9trf99pxwv0", pkgPath), pkgExe, "0.11.1", "0.11.2", false},
		{"the same version with another fingerprint", own, pkgExe, "0.11.4", "0.11.4", false},
		{"no recorded version", own, pkgExe, "0.11.4", "", false},
		{"package version differs from the binary's", own, pkgExe, "0.11.5", "0.11.3", false},
		{"case and slashes do not matter", id(winpkg.Family, pkgFull, strings.ToLower(pkgPath)), strings.ReplaceAll(pkgExe, `\`, "/"), "0.11.4", "0.11.3", true},
	} {
		if got := winpkg.Accepts(c.id, c.exe, lad, c.bin, c.stateVer); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
