//go:build linux

package company

// machineDir is fixed on Linux and not configurable.
func machineDir() string { return "/etc/rosmarops" }
