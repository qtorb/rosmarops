//go:build darwin

package company

// There is no company mode on macOS: no machine folder, so no company
// configuration or administrator fingerprint list is ever looked for. The
// ownership check (trust_unix.go) still serves other administrator files,
// such as Claude Desktop's managed preferences (BV-277).
func machineDir() string { return "" }
