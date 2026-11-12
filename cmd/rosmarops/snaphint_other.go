//go:build !linux

package main

// browserIsSnap is Linux only (BV-219 B4): on Windows and macOS there is no
// snap browser, so the hint is never shown.
func browserIsSnap() bool { return false }
