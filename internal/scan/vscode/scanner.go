// Package vscode implements the VS Code extensions drift scanner.
// ScanExtensions is the sole entry point; it is a pure function (no I/O)
// so it can be exercised in unit tests without any OS-specific adapters.
package vscode

import (
	"fmt"
	"strings"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/state"
)

// Finding text approved by human owner (2026-05-21).
const (
	whyExtensionNew = "VS Code extensions run with full user-level permissions and " +
		"can read files, execute processes, and make network calls on your behalf. " +
		"An extension that appeared without a deliberate install may indicate a " +
		"supply-chain compromise, a malicious package installed by another tool, " +
		"or a shared account being used by someone else."

	// actionExtensionNew describes the real v0.1 mechanism: manual JSON edit.
	// Do NOT reference `rosmarops allow add` — that CLI is a stub until v0.2.
	// Rule: suggested_action must describe what actually works today, not aspirational UX.
	// Update this text when the allow subcommand is fully implemented.
	actionExtensionNew = "Confirm you installed this extension intentionally. " +
		"If yes, add an entry under `allow_list.extensions` in your rosmarops.config.json " +
		"with a brief note explaining why; future runs will silently accept it. " +
		"If no, disable and uninstall it immediately, then audit other software " +
		"installed around the same time."
)

// ScanResult bundles all outputs from a single ScanExtensions call.
type ScanResult struct {
	Findings         []finding.Finding
	AllowListedCount int // extensions suppressed by the allow-list this run
}

// ScanExtensions returns findings for VS Code extensions present in current
// but absent from baseline, excluding any whose id is allow-listed.
//
// Baseline key: lowercase(id)+"@"+version. A version update introduces a new
// finding until the next `rosmarops baseline` establishes the new version as trusted.
//
// # Hash-depth trade-off
//
// In-place tamper detection — hashing package.json + extension.vsixmanifest
// per extension — is deferred to v0.3+. The state schema v1 does not persist
// a content hash for extensions, and an attacker who can patch bytes inside
// %USERPROFILE%\.vscode\extensions\ has already achieved user-level code
// execution (patching RosmarOps is equally trivial). Version-change detection
// (this function) covers the realistic threat: extensions installed by package
// managers, malicious marketplace updates, or unauthorized users.
func ScanExtensions(
	current []platform.VSCodeExtension,
	baseline []state.VSCodeExtension,
	allowList []config.AllowEntryID,
) ScanResult {
	// Build lookup sets for O(1) checks.
	baselineSet := make(map[string]struct{}, len(baseline))
	for _, b := range baseline {
		baselineSet[strings.ToLower(b.ID)+"@"+b.Version] = struct{}{}
	}

	allowed := make(map[string]struct{}, len(allowList))
	for _, a := range allowList {
		allowed[strings.ToLower(a.ID)] = struct{}{}
	}

	var (
		findings         []finding.Finding
		allowListedCount int
	)

	for _, ext := range current {
		key := strings.ToLower(ext.ID) + "@" + ext.Version
		if _, inBaseline := baselineSet[key]; inBaseline {
			continue
		}
		if _, isAllowed := allowed[strings.ToLower(ext.ID)]; isAllowed {
			allowListedCount++
			continue
		}

		f := finding.NewFinding(
			findingID(ext.ID, ext.Version),
			finding.SeverityYellow,
			"vscode_extension_new",
			fmt.Sprintf("New VS Code extension: %s %s", ext.ID, ext.Version),
		)
		f.WhyItMatters = whyExtensionNew
		f.SuggestedAction = actionExtensionNew
		f.Confidence = "high"
		f.Evidence = finding.Evidence{
			ExtensionID:    ext.ID,
			PackageVersion: ext.Version,
			// Enabled is informational context; disabled extensions are still
			// reported — bits on disk, one click to reactivate (owner decision 2026-05-21).
			Extra: map[string]any{"enabled": ext.Enabled},
		}

		findings = append(findings, f)
	}

	return ScanResult{
		Findings:         findings,
		AllowListedCount: allowListedCount,
	}
}

// findingID returns a deterministic finding ID for a vscode_extension_new event.
// Format: vscode_extension_new:<sha256[:16]> — hash input is lowercase(id)@version.
func findingID(id, version string) string {
	h := hash.Bytes([]byte(strings.ToLower(id) + "@" + version))
	return "vscode_extension_new:" + h[:16]
}
