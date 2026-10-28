//go:build darwin

package main

import (
	"github.com/qtorb/rosmarops/adapters/darwin"
	"github.com/qtorb/rosmarops/internal/platform"
)

// newPlatform is this system's adapter: macOS's (decision 2026-10-28,
// macOS, M1a).
func newPlatform() platform.Platform { return darwin.New() }
