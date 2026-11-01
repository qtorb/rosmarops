//go:build !windows

package main

import "github.com/qtorb/rosmarops/internal/catalog"

// maybeConnectWindow is a no-op off Windows: macOS and Linux connect through
// `rosmarops connect` in the console (BV-137 point 5), not a window.
func maybeConnectWindow(_ spacePaths, _ catalog.Catalog) {}
