// Package winpkg reads the Windows package identity of the running process
// (CTO-41 §1.2 A, BV-222): its package family name, full name and installed
// folder, when RosmarOps runs installed from the Microsoft Store. It uses only
// kernel32 through the standard library's syscall; elsewhere, or when the
// process has no package identity, every value is empty.
package winpkg

import "strings"

// Family is RosmarOps's own package family name: the manifest's Name and the
// publisher id derived from its Publisher (packaging/msix/AppxManifest.xml),
// fixed in the code and checked against the manifest by TestFamilyMatchesManifest.
const Family = "AlbertGarciaPujadas.RosmarOps_2a9trf99pxwv0"

// Identity is the running process's package identity; all empty when it has
// none.
type Identity struct {
	FamilyName string // GetCurrentPackageFamilyName
	FullName   string // GetCurrentPackageFullName: Name_Version_Arch_Resource_PublisherId
	Path       string // GetCurrentPackagePath: the package's installed folder
}

// Current is the running process's identity.
func Current() Identity {
	return Identity{FamilyName: FamilyName(), FullName: FullName(), Path: Path()}
}

// Version is the package version in a full name («Name_0.11.3.0_x64__id» →
// «0.11.3.0»), or "".
func Version(fullName string) string {
	parts := strings.Split(fullName, "_")
	if len(parts) < 5 {
		return ""
	}
	return parts[1]
}
