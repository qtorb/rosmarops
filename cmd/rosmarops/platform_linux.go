//go:build linux

package main

import (
	linuxadapter "github.com/qtorb/rosmarops/adapters/linux"
	"github.com/qtorb/rosmarops/internal/platform"
)

// newPlatform is this system's adapter: Linux's (BV-165).
func newPlatform() platform.Platform { return linuxadapter.New() }
