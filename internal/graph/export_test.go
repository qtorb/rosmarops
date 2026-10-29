package graph

// SetSandboxPlatformUnknown makes the run's system Windows (true) or not
// for sandboxPlatformUnknown, and returns the function that restores it.
func SetSandboxPlatformUnknown(windows bool) (restore func()) {
	prev := sandboxPlatformUnknown
	sandboxPlatformUnknown = windows
	return func() { sandboxPlatformUnknown = prev }
}
