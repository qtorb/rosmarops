//go:build !linux && !windows && !darwin

package company

// There is no company mode here: no machine folder,
// and no file is ever trusted.
func machineDir() string { return "" }

func checkTrusted(string) error { return ErrUntrusted }
