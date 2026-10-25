// Package avexclusions detects new Windows Defender exclusions since the last
// baseline. Any new exclusion not on the allow-list is a RED finding — Defender
// exclusions are a standard technique to hide malicious tools from endpoint
// protection.
package avexclusions

import (
	"strings"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/state"
)

// ---------------------------------------------------------------------------
// Finding text — approved by human owner (review required before commit).
// ---------------------------------------------------------------------------

const (
	whyAVExclusionNew = "A new Windows Defender exclusion appeared that was not in your baseline. " +
		"Exclusions tell Defender to stop scanning specific paths, processes, or file extensions in real " +
		"time. Adding an exclusion is a standard attacker technique to conceal malicious tools from " +
		"endpoint protection — and some legitimate software adds exclusions automatically during " +
		"installation without visible user confirmation."

	actionAVExclusionNew = "Run `Get-MpPreference | Select-Object ExclusionPath, ExclusionProcess, " +
		"ExclusionExtension` to identify which entry is new. If you added it intentionally for a " +
		"development tool that triggers false positives, add an entry under `allow_list.av_exclusions` " +
		"in rosmarops.config.json with the path as the `path` field and a specific note; future runs will " +
		"silently accept it. If you did not add this exclusion, remove it immediately with " +
		"`Remove-MpPreference -ExclusionPath '<value>'` (or -ExclusionProcess / -ExclusionExtension " +
		"as appropriate) and run a full Defender scan."
)

// ScanResult holds the output of ScanAVExclusions.
type ScanResult struct {
	// Findings contains one RED finding per new exclusion.
	Findings []finding.Finding

	// NewBaseline is the full current exclusion set.
	// The caller must persist this to state.Baselines.AVExclusions.
	NewBaseline []state.AVExclusion

	// AllowListedCount is the number of new exclusions suppressed by the allow-list.
	AllowListedCount int
}

// ScanAVExclusions compares the current Defender exclusion set against the
// baselined set and emits a RED finding for each new exclusion that is not
// on the allow-list.
//
// The allow-list is keyed by the exclusion value (AllowEntryPath.Path field).
// Comparison is case-insensitive (Windows paths).
//
// The returned NewBaseline contains the full current exclusion set.
func ScanAVExclusions(
	exclusions []platform.DefenderExclusion,
	baseline []state.AVExclusion,
	allowList []config.AllowEntryPath,
) ScanResult {
	// Build baseline lookup: norm(kind+":"+value) → true.
	baselined := make(map[string]bool, len(baseline))
	for _, b := range baseline {
		baselined[normExcl(b.Kind, b.Value)] = true
	}

	// Build allow-list lookup: norm(path value) → true.
	allowed := make(map[string]bool, len(allowList))
	for _, a := range allowList {
		allowed[normStr(a.Path)] = true
	}

	// Build new baseline from full current exclusion set.
	newBL := make([]state.AVExclusion, 0, len(exclusions))
	for _, e := range exclusions {
		if e.Value == "" {
			continue
		}
		newBL = append(newBL, state.AVExclusion{Kind: e.Kind, Value: e.Value})
	}

	var findings []finding.Finding
	allowListedCount := 0

	for _, e := range exclusions {
		if e.Value == "" {
			continue
		}
		if baselined[normExcl(e.Kind, e.Value)] {
			continue // known exclusion — no finding
		}

		// New exclusion: check allow-list.
		if allowed[normStr(e.Value)] {
			allowListedCount++
			continue
		}

		id := avExclusionFindingID(e.Kind, e.Value)
		f := finding.NewFinding(id, finding.SeverityRed, "av_exclusion_new",
			"New Defender exclusion ("+e.Kind+"): "+e.Value)
		f.WhyItMatters = whyAVExclusionNew
		f.SuggestedAction = actionAVExclusionNew
		f.Confidence = "high"
		f.Evidence = finding.Evidence{
			Extra: map[string]string{
				"exclusion_kind":  e.Kind,
				"exclusion_value": e.Value,
			},
		}
		findings = append(findings, f)
	}

	return ScanResult{
		Findings:         findings,
		NewBaseline:      newBL,
		AllowListedCount: allowListedCount,
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func avExclusionFindingID(kind, value string) string {
	h := hash.Bytes([]byte(kind + ":" + value))
	return "av_exclusion_new:" + h[:16]
}

func normExcl(kind, value string) string {
	return normStr(kind) + ":" + normStr(value)
}

func normStr(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
