//go:build !windows

package main

// freeConsole detaches the process from its console window: nothing to do
// outside Windows.
func freeConsole() {}
