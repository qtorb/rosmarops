//go:build !windows

package main

import (
	"os"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// connectOffer off Windows is the console question `rosmarops connect` asks
// (BV-137 point 5; BV-158 part 4, decision A). macOS and Linux have no window.
// The caller has already checked that this computer is not connected and that
// the offer is due. The console flow connects over the network once it starts,
// so it only runs when a person is at the console (an interactive stdin): a
// non-interactive double click (a scheduled launch, the black-box tests) makes
// no connection, keeping the double click network-free unless someone answers.
func connectOffer(paths spacePaths, cat catalog.Catalog) connectResult {
	if isSpaceConnected(paths) || !isTerminal(os.Stdin) {
		return connectNotOffered
	}
	return connectInConsole(cat, paths)
}
