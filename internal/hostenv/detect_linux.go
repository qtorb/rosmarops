//go:build linux

package hostenv

import "os"

// Detect is this computer's environment.
func Detect() *Env { return DetectLinux(os.DirFS("/")) }
