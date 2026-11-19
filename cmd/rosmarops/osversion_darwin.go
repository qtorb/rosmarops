//go:build darwin

package main

import "os"

// osVersion is macOS's ProductVersion, read from SystemVersion.plist without
// running sw_vers.
func osVersion() string {
	raw, err := os.ReadFile("/System/Library/CoreServices/SystemVersion.plist")
	if err != nil {
		return ""
	}
	return plistProductVersion(string(raw))
}
