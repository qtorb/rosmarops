//go:build !darwin

package main

import (
	windowsadapter "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/platform"
)

// newPlatform is this system's adapter: Windows's, as before M1a (it is
// also the one on Linux).
func newPlatform() platform.Platform { return windowsadapter.New() }
