//go:build !windows

package firstrun

import "errors"

// Pick is Windows-only: elsewhere there is no folder picker, as if
// cancelled.
func Pick(string) (string, error) {
	return "", errors.New("folder picker: not on Windows")
}
