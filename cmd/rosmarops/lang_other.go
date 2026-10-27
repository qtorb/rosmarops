//go:build !windows

package main

// uiLanguage has no Windows display language to read elsewhere: English.
func uiLanguage() uint16 { return 0 }
