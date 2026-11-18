//go:build !linux && !windows

package company

// There is no company mode here (macOS is outside it): no machine folder,
// and no file is ever trusted.
func machineDir() string { return "" }

func checkTrusted(string) error { return ErrUntrusted }
