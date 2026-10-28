//go:build !windows

package winpath

// Off Windows there is no system or Windows directory to resolve; callers fall
// back to their previous behaviour.
func SystemDir() string  { return "" }
func WindowsDir() string { return "" }
