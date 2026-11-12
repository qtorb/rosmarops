//go:build linux

package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// snapProbeTimeout bounds the xdg-settings call (BV-219 B4).
const snapProbeTimeout = 5 * time.Second

// The snap browser detectors (BV-219 B4): read-only, they only read the
// default browser's .desktop name and where firefox resolves. Tests replace
// them.
var (
	xdgDefaultBrowser = func() (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), snapProbeTimeout)
		defer cancel()
		out, err := exec.CommandContext(ctx, "xdg-settings", "get", "default-web-browser").Output()
		return string(out), err
	}
	firefoxPath = func() (string, error) {
		p, err := exec.LookPath("firefox")
		if err != nil {
			return "", err
		}
		if r, err := filepath.EvalSymlinks(p); err == nil && isSnapPath(r) {
			return r, nil
		}
		return p, nil
	}
)

// browserIsSnap reports whether the default browser is probably Firefox
// installed as a snap, which cannot read hidden folders such as ~/.rosmarops
// although xdg-open exits 0 (BV-219 B4). Either fact is enough.
func browserIsSnap() bool {
	if name, err := xdgDefaultBrowser(); err == nil && isSnapDesktop(name) {
		return true
	}
	if p, err := firefoxPath(); err == nil && isSnapPath(p) {
		return true
	}
	return false
}

// isSnapDesktop reports whether xdg-settings' output names the snap Firefox's
// .desktop file (firefox_firefox.desktop).
func isSnapDesktop(out string) bool {
	return strings.HasPrefix(strings.TrimSpace(out), "firefox_")
}

// isSnapPath reports whether path is under /snap/bin/.
func isSnapPath(path string) bool {
	return strings.HasPrefix(path, "/snap/bin/")
}
