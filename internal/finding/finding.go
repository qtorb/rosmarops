// Package finding defines the Finding type, severity constants, and the
// fail-secure status reducer used across all scan categories.
//
// The reducer is the single source of truth for overall report status.
// It is NOT left to caller discipline — callers submit findings and scan
// error counts; the reducer decides the status.
package finding

import (
	"time"

	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// Severity represents the urgency of a finding.
type Severity string

const (
	SeverityRed    Severity = "red"
	SeverityYellow Severity = "yellow"
	SeverityInfo   Severity = "info"
)

// Status is the overall report status derived by the Reducer.
type Status string

const (
	StatusGreen  Status = "green"
	StatusYellow Status = "yellow"
	StatusRed    Status = "red"
)

// Finding mirrors the `finding` definition in schemas/report.v1.json exactly.
// All fields match the JSON schema property names via struct tags.
type Finding struct {
	// ID is stable across runs: <category>:<sha256-prefix-of-evidence>.
	// Minimum 16 hex chars of prefix; 64 max (full hash).
	ID       string   `json:"id"`
	Severity Severity `json:"severity"`
	Category string   `json:"category"`
	Title    string   `json:"title"`

	// Evidence contains where/what was observed. Fields used depend on category.
	// Never contains a full secret value.
	Evidence Evidence `json:"evidence,omitempty"`

	// WhyItMatters explains the risk in plain language (1–3 sentences).
	WhyItMatters string `json:"why_it_matters"`

	// SuggestedAction is the concrete next step in imperative voice.
	SuggestedAction string `json:"suggested_action"`

	// Confidence is RosmarOps's confidence this is a true positive.
	Confidence string `json:"confidence"`

	// FirstSeen is when this finding ID first appeared in any report (RFC3339 UTC).
	FirstSeen string `json:"first_seen,omitempty"`

	// IsNewSinceLast is true if this finding ID was absent from the previous report.
	IsNewSinceLast bool `json:"is_new_since_last,omitempty"`

	// GroupDetail is the pattern or detail the v2 report groups v1 findings
	// by (category, detail, family). Never written to report.v1.json.
	GroupDetail string `json:"-"`

	// Committed are, for secret_tracked and secret_in_history, the secrets
	// behind the finding as the v2 graph needs them (decision 2026-10-24,
	// memoria, §2.2): salted fingerprint, where it is committed and whether
	// the repository declares a remote. Never the value; never written to
	// report.v1.json.
	Committed []CommittedSecret `json:"-"`
}

// Presence of a committed secret.
const (
	PresenceTracked     = "tracked"      // in the current tree
	PresenceHistoryOnly = "history_only" // only in past commits
)

// CommittedSecret is one secret committed in a repository.
type CommittedSecret struct {
	PatternID string
	Ref       secrets.Ref
	Repo      string
	Presence  string
	File      string // repository-relative, forward slashes
	Line      int    // tracked
	Commit    string // history_only: the full commit hash
	HasRemote bool
}

// Evidence holds the observable details of a finding.
// Never contains a full secret value — redacted_preview only.
type Evidence struct {
	File            string `json:"file,omitempty"`
	Line            int    `json:"line,omitempty"`
	RedactedPreview string `json:"redacted_preview,omitempty"`
	RepoPath        string `json:"repo_path,omitempty"`
	ExtensionID     string `json:"extension_id,omitempty"`
	PackageName     string `json:"package_name,omitempty"`
	PackageVersion  string `json:"package_version,omitempty"`
	MCPServer       string `json:"mcp_server,omitempty"`
	MCPClient       string `json:"mcp_client,omitempty"`
	Command         string `json:"command,omitempty"`
	CommitSHA       string `json:"commit_sha,omitempty"`
	PreviousSHA256  string `json:"previous_sha256,omitempty"`
	CurrentSHA256   string `json:"current_sha256,omitempty"`
	Extra           any    `json:"extra,omitempty"`
}

// ScanError is a non-fatal error from a scan scope that prevented complete
// analysis. Any non-empty errors slice forces the report status to at least
// yellow (enforced by Reducer).
type ScanError struct {
	Scope   string `json:"scope"`
	Message string `json:"message"`
}

// Reducer derives the overall report status from findings and scan error count.
//
// Fail-secure rules (enforced here, not in callers):
//   - Any red finding → StatusRed.
//   - Any yellow finding OR any scan error → StatusYellow.
//   - Zero findings and zero scan errors → StatusGreen.
//
// StatusGreen is only possible when BOTH conditions hold:
// zero findings of any severity AND zero scan errors.
func Reducer(findings []Finding, scanErrors int) Status {
	for _, f := range findings {
		if f.Severity == SeverityRed {
			return StatusRed
		}
	}
	if scanErrors > 0 {
		return StatusYellow
	}
	for _, f := range findings {
		if f.Severity == SeverityYellow {
			return StatusYellow
		}
	}
	return StatusGreen
}

// NewFinding is a convenience constructor that sets FirstSeen to now (UTC)
// and IsNewSinceLast to true. Used when creating a brand-new finding.
func NewFinding(id string, sev Severity, category, title string) Finding {
	return Finding{
		ID:             id,
		Severity:       sev,
		Category:       category,
		Title:          title,
		FirstSeen:      time.Now().UTC().Format(time.RFC3339),
		IsNewSinceLast: true,
	}
}
