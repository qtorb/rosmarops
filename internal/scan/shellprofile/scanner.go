// Package shellprofile detects changes to PowerShell $PROFILE files since the
// last baseline. A modified shell profile is a RED finding — profiles execute
// on every new terminal session and are a classic persistence vector.
package shellprofile

import (
	"fmt"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/platform"
)

// ---------------------------------------------------------------------------
// Finding text — approved by human owner (review required before commit).
// ---------------------------------------------------------------------------

const (
	whyShellProfileModified = "Your PowerShell profile (one or more of the four $PROFILE scopes) has " +
		"changed since the last baseline. Shell profiles execute automatically on every new terminal session, " +
		"making them a classic persistence vector — compromised developer tooling, malicious npm install " +
		"scripts, and supply-chain attacks routinely modify profiles to survive reboots and re-infect new shells."

	actionShellProfileModified = "Open each profile file listed in the evidence and compare its current " +
		"content to what you last intentionally changed. If you made the change (e.g., adding an alias or " +
		"function), run `rosmarops baseline --profile` to record the new hash; future runs will accept it. " +
		"If you do not recognize the change, treat your machine as potentially compromised: audit recent " +
		"software installs, check startup entries with Sysinternals Autoruns, and rotate any credentials " +
		"that may have been exposed in the modified profile."
)

// ScanResult holds the output of ScanShellProfiles.
type ScanResult struct {
	// Findings contains at most one finding: shell_profile_modified (RED).
	Findings []finding.Finding

	// NewSHA is the combined SHA-256 of all profile scopes as of this scan.
	// The caller must persist this to state.Baselines.ShellProfileSHA256.
	NewSHA string
}

// ScanShellProfiles compares the current PowerShell profile state against
// the baselineSHA recorded in state.
//
// If any profile scope has changed (or a new one appeared, or one was deleted)
// since the baseline, a single RED finding is emitted covering all changed scopes.
//
// A zero-length profiles slice (PS unavailable) produces no findings.
func ScanShellProfiles(profiles []platform.ShellProfile, baselineSHA string) ScanResult {
	if len(profiles) == 0 {
		return ScanResult{}
	}

	combined := combinedSHA(profiles)

	if baselineSHA == "" || combined == baselineSHA {
		// No baseline yet (first run) OR hashes match — no finding.
		return ScanResult{NewSHA: combined}
	}

	// Hashes differ — build evidence that lists all profiles and their paths.
	extra := make(map[string]string, len(profiles))
	for _, p := range profiles {
		sha := p.SHA256
		if sha == "" {
			sha = "(file does not exist)"
		}
		extra[fmt.Sprintf("profile_%s_path", p.Scope)] = p.Path
		extra[fmt.Sprintf("profile_%s_sha256", p.Scope)] = sha
	}

	id := "shell_profile_modified:0"
	f := finding.NewFinding(id, finding.SeverityRed, "shell_profile_modified",
		"PowerShell $PROFILE changed since baseline")
	f.WhyItMatters = whyShellProfileModified
	f.SuggestedAction = actionShellProfileModified
	f.Confidence = "high"
	f.Evidence = finding.Evidence{
		PreviousSHA256: baselineSHA[:16] + "…",
		CurrentSHA256:  combined[:16] + "…",
		Extra:          extra,
	}

	return ScanResult{
		Findings: []finding.Finding{f},
		NewSHA:   combined,
	}
}

// CombinedSHA computes and returns the combined SHA-256 of all profile scopes.
// Exported so cmd_baseline.go can compute it without duplicating logic.
func CombinedSHA(profiles []platform.ShellProfile) string {
	return combinedSHA(profiles)
}

// combinedSHA computes a stable SHA-256 that represents the union of all
// profile scope hashes. The canonical form is a newline-joined list of
// "Scope=sha256" pairs sorted by scope name. Absent files use the all-zeros
// sentinel rather than an empty string so that absent ≠ present-but-empty.
func combinedSHA(profiles []platform.ShellProfile) string {
	const absent = "0000000000000000000000000000000000000000000000000000000000000000"
	parts := make([]string, 0, len(profiles))
	for _, p := range profiles {
		sha := p.SHA256
		if sha == "" {
			sha = absent
		}
		parts = append(parts, p.Scope+"="+sha)
	}
	sort.Strings(parts)
	return hash.Bytes([]byte(strings.Join(parts, "\n")))
}
