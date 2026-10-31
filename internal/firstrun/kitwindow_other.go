//go:build !windows

package firstrun

// AskKitPhrase is Windows only (BV-131, CTO-12 H2): on other systems the
// double-click onboarding does not apply, so the phrase window is never
// shown and the review goes on without a kit (fail closed).
func AskKitPhrase(KitConfig, func(string) bool) bool { return false }
