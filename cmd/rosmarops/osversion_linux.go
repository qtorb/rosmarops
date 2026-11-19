//go:build linux

package main

import "os"

// osVersion is ID and VERSION_ID of /etc/os-release.
func osVersion() string {
	raw, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	return osReleaseVersion(string(raw))
}
