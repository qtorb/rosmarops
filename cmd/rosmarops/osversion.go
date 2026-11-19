package main

import (
	"bufio"
	"strings"
)

// The system's version for the problem list (problems.v1 os_version): only the
// operating system's name and version, read from the system's own files or
// API, never by running a program. The value keeps only the characters the
// schema allows and at most 40 of them.

// osVersionMaxLen is problems.v1's maxLength for os_version.
const osVersionMaxLen = 40

// cleanOSVersion keeps the characters problems.v1 allows in os_version
// (letters, digits, dot, underscore, space and hyphen), trimmed, at most 40.
func cleanOSVersion(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '.', r == '_', r == ' ', r == '-':
			b.WriteRune(r)
		}
	}
	out := strings.TrimSpace(b.String())
	if len(out) > osVersionMaxLen {
		out = strings.TrimSpace(out[:osVersionMaxLen])
	}
	return out
}

// osReleaseVersion is «ID VERSION_ID» from the content of /etc/os-release
// (for example «ubuntu 24.04»); either part may be missing.
func osReleaseVersion(content string) string {
	var id, ver string
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		k, v, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		switch k {
		case "ID":
			id = v
		case "VERSION_ID":
			ver = v
		}
	}
	return cleanOSVersion(strings.TrimSpace(id + " " + ver))
}

// plistProductVersion is the ProductVersion of macOS's SystemVersion.plist
// (an XML property list): the <string> that follows <key>ProductVersion</key>.
func plistProductVersion(content string) string {
	_, rest, ok := strings.Cut(content, "<key>ProductVersion</key>")
	if !ok {
		return ""
	}
	_, rest, ok = strings.Cut(rest, "<string>")
	if !ok {
		return ""
	}
	v, _, ok := strings.Cut(rest, "</string>")
	if !ok {
		return ""
	}
	return cleanOSVersion(v)
}
