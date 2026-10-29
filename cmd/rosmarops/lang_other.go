//go:build !windows

package main

import (
	"log/slog"

	"github.com/qtorb/rosmarops/internal/platform"
)

// uiLanguage has no Windows display language to read elsewhere: English
// (platform.ErrNotApplicable). The macOS report lists it as not checked yet
// (display_language; decision 2026-10-29, macOS vista previa, point 3).
func uiLanguage() uint16 {
	slog.Info("language: system display language", "err", platform.ErrNotApplicable)
	return 0
}

// displayLanguageChecked: no system language is read here.
const displayLanguageChecked = false
