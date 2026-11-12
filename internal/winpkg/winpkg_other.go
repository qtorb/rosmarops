//go:build !windows

package winpkg

// FamilyName is "" outside Windows: there is no package identity.
func FamilyName() string { return "" }

// FullName is "" outside Windows.
func FullName() string { return "" }

// Path is "" outside Windows.
func Path() string { return "" }
