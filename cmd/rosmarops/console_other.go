//go:build !windows

package main

// freeConsole detaches the process from its console window: nothing to do
// outside Windows.
func freeConsole() {}

// setConsoleTitle is a no-op outside Windows: no console title to set (BV-151).
func setConsoleTitle(string) {}

// enableVTProcessing reports that ANSI sequences are interpreted. Outside
// Windows terminals handle them natively, so this is always true (BV-151).
func enableVTProcessing() bool { return true }

// disableQuickEdit is a no-op outside Windows: there is no QuickEdit console
// mode to turn off (BV-211 §2). It returns a no-op restore function.
func disableQuickEdit() func() { return func() {} }
