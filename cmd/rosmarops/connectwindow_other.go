//go:build !windows

package main

import "github.com/qtorb/rosmarops/internal/catalog"

// connectOffer off Windows is the console question `rosmarops connect` asks
// (BV-137 point 5; BV-158 part 4, decision A). macOS and Linux have no window.
// The caller has already checked that this computer is not connected and that
// the offer is due.
func connectOffer(paths spacePaths, cat catalog.Catalog) connectResult {
	if isSpaceConnected(paths) {
		return connectNotOffered
	}
	return connectInConsole(cat, paths)
}
