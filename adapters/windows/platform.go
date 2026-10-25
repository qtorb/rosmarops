// Package windows provides Windows-specific implementations of platform.Platform.
// All I/O is local: filesystem reads and exec.CommandContext calls with
// explicit timeouts. No network calls are made anywhere in this package.
package windows

import (
	"path/filepath"
	"runtime"
)

// Claude Code managed settings locations on Windows
// (https://code.claude.com/docs/en/managed-settings#where-each-mechanism-stores-the-policy).
const (
	managedSettingsDir    = `C:\Program Files\ClaudeCode`
	legacyManagedSettings = `C:\ProgramData\ClaudeCode\managed-settings.json`
)

// WindowsPlatform implements platform.Platform for Windows using filesystem
// reads and PowerShell-backed helpers where necessary.
// Construct one with New() and pass it to scan functions.
type WindowsPlatform struct {
	// managed holds where Claude Code managed settings are read from; nil
	// when they are not read (platforms other than Windows).
	managed *managedLocations
}

// managedLocations are the sources of Claude Code managed settings.
type managedLocations struct {
	dir      string         // managed-settings.json, managed-settings.d\, managed-mcp.json
	legacy   string         // legacy path: checked for existence only
	registry RegistryReader // HKLM and HKCU Settings values
}

// New returns a ready-to-use WindowsPlatform. Managed settings are read only
// on Windows, where they exist.
func New() *WindowsPlatform {
	w := &WindowsPlatform{}
	if runtime.GOOS == "windows" {
		w.managed = &managedLocations{dir: managedSettingsDir, legacy: legacyManagedSettings, registry: psRegistry{}}
	}
	return w
}

// NewWithManaged returns a WindowsPlatform that reads Claude Code managed
// settings from dir (and checks legacy for existence), with registry as the
// registry reader, on any platform. For tests.
func NewWithManaged(dir, legacy string, registry RegistryReader) *WindowsPlatform {
	return &WindowsPlatform{managed: &managedLocations{dir: filepath.Clean(dir), legacy: legacy, registry: registry}}
}
