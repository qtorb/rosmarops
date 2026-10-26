//go:build !windows

package notify

import (
	"context"
	"errors"
)

// Run is Windows-only: elsewhere there is no notification to show.
func Run(context.Context, string) (string, error) {
	return "", errors.New("notify: not on Windows")
}

// OpenReport is Windows-only: elsewhere no report is opened.
func OpenReport(string) error {
	return errors.New("notify: not on Windows")
}
