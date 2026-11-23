//go:build !windows

package main

import "github.com/qtorb/rosmarops/internal/catalog"

// askUsageWindow off Windows: there is no window (BV-286), so the question
// is asked in the terminal.
func askUsageWindow(catalog.Catalog) (usageWindowAnswer, func()) {
	return usageWindowNotShown, nil
}
