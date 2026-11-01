//go:build !windows

package notify

import "github.com/qtorb/rosmarops/internal/platform"

// RevealInExplorer is Windows only (BV-131, CTO-12 H4): on other systems the
// double-click onboarding does not apply.
func RevealInExplorer(string) error { return platform.ErrNotApplicable }
