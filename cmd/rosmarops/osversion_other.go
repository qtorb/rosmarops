//go:build !linux && !darwin && !windows

package main

// osVersion is empty where no reader exists.
func osVersion() string { return "" }
