//go:build windows

package main

import "syscall"

// uiLanguage is the Windows display language of the user
// (GetUserDefaultUILanguage, kernel32): a LANGID.
func uiLanguage() uint16 {
	r, _, _ := syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage").Call()
	return uint16(r)
}

// displayLanguageChecked: the report follows the Windows display language.
const displayLanguageChecked = true
