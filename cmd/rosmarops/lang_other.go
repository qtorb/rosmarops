//go:build !windows

package main

import (
	"log/slog"

	"github.com/qtorb/rosmarops/internal/platform"
)

// uiLanguage has no Windows display language to read elsewhere: English
// (platform.ErrNotApplicable; macOS's is M1b).
func uiLanguage() uint16 {
	slog.Info("language: system display language", "err", platform.ErrNotApplicable)
	return 0
}
