// Package report builds, renders, and writes RosmarOps reports.
//
// Report files come in pairs: a Markdown file for human reading and a JSON
// sidecar for machine consumption (future web viewer). Both are written
// atomically to the reports directory.
//
// Write targets: only the configured reports directory (default
// ~/.rosmarops/reports/). No other paths are touched.
package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/finding"
)

const schemaVersion = 1

// Report mirrors schemas/report.v1.json exactly.
type Report struct {
	SchemaVersion int                 `json:"schema_version"`
	EngineVersion string              `json:"engine_version"`
	GeneratedAt   string              `json:"generated_at"`
	Platform      Platform            `json:"platform"`
	Header        Header              `json:"header"`
	Findings      []finding.Finding   `json:"findings"`
	Summary       Summary             `json:"summary"`
	Repos         []RepoInfo          `json:"repos,omitempty"`
	Errors        []finding.ScanError `json:"errors,omitempty"`
	Delta         *Delta              `json:"delta,omitempty"`
}

// Platform describes the OS and architecture of the host.
type Platform struct {
	OS           string `json:"os"`
	Arch         string `json:"arch"`
	HostnameHash string `json:"hostname_hash,omitempty"`
}

// Header is the top-level summary visible in the Markdown report header.
type Header struct {
	Status                string  `json:"status"`
	Counts                Counts  `json:"counts"`
	ReposScanned          int     `json:"repos_scanned"`
	ReposFailed           int     `json:"repos_failed"`
	ConsecutiveGreen      int     `json:"consecutive_green"`
	ScanDurationSeconds   float64 `json:"scan_duration_seconds,omitempty"`
	CadenceRecommendation string  `json:"cadence_recommendation,omitempty"`
}

// Counts breaks down findings by severity and suppression.
// All fields except new_since_last are required in the schema.
type Counts struct {
	Red                int `json:"red"`
	Yellow             int `json:"yellow"`
	Info               int `json:"info"`
	NewSinceLast       int `json:"new_since_last,omitempty"`
	AllowListedThisRun int `json:"allow_listed_this_run"`
}

// Summary provides category-level aggregates and the top findings for
// the Markdown header noise budget.
type Summary struct {
	ByCategory map[string]CategoryCounts `json:"by_category"`
	TopReds    []string                  `json:"top_reds"`
	TopYellows []string                  `json:"top_yellows"`
}

// CategoryCounts breaks down findings by severity within one category.
type CategoryCounts struct {
	Red    int `json:"red,omitempty"`
	Yellow int `json:"yellow,omitempty"`
	Info   int `json:"info,omitempty"`
}

// RepoInfo is a per-repo scan result summary (used by future web viewer).
type RepoInfo struct {
	Path             string `json:"path"`
	Branch           string `json:"branch,omitempty"`
	Status           string `json:"status"`
	UncommittedFiles int    `json:"uncommitted_files,omitempty"`
	ErrorMessage     string `json:"error_message,omitempty"`
}

// Delta describes what changed since the previous report.
type Delta struct {
	PreviousReportAt   string   `json:"previous_report_at,omitempty"`
	AddedFindingIDs    []string `json:"added_finding_ids"`
	ResolvedFindingIDs []string `json:"resolved_finding_ids"`
}

// Build constructs a Report from scan results. It is a pure function with no
// side effects (no writes, no network). prev may be nil on first run.
// allowListedThisRun is the total count of findings suppressed by allow-list
// entries across all scanners this run; it is always written to counts (even 0).
func Build(
	engineVersion string,
	findings []finding.Finding,
	errors []finding.ScanError,
	reposScanned, reposFailed int,
	consecutiveGreen int,
	allowListedThisRun int,
	prev *Report,
) Report {
	now := time.Now().UTC().Format(time.RFC3339)
	status := finding.Reducer(findings, len(errors))

	// Count by severity.
	var reds, yellows, infos int
	for _, f := range findings {
		switch f.Severity {
		case finding.SeverityRed:
			reds++
		case finding.SeverityYellow:
			yellows++
		case finding.SeverityInfo:
			infos++
		}
	}

	// Delta against previous report.
	var delta *Delta
	var newCount int
	if prev != nil {
		prevIDs := make(map[string]struct{}, len(prev.Findings))
		for _, f := range prev.Findings {
			prevIDs[f.ID] = struct{}{}
		}
		currIDs := make(map[string]struct{}, len(findings))
		for _, f := range findings {
			currIDs[f.ID] = struct{}{}
		}
		var added, resolved []string
		for _, f := range findings {
			if _, ok := prevIDs[f.ID]; !ok {
				added = append(added, f.ID)
			}
		}
		for _, f := range prev.Findings {
			if _, ok := currIDs[f.ID]; !ok {
				resolved = append(resolved, f.ID)
			}
		}
		sort.Strings(added)
		sort.Strings(resolved)
		newCount = len(added)
		delta = &Delta{
			PreviousReportAt:   prev.GeneratedAt,
			AddedFindingIDs:    added,
			ResolvedFindingIDs: resolved,
		}
	}

	// Sort findings: red first, then yellow, then info; within severity, by category then id.
	sorted := make([]finding.Finding, len(findings))
	copy(sorted, findings)
	sort.Slice(sorted, func(i, j int) bool {
		si := severityOrder(sorted[i].Severity)
		sj := severityOrder(sorted[j].Severity)
		if si != sj {
			return si < sj
		}
		if sorted[i].Category != sorted[j].Category {
			return sorted[i].Category < sorted[j].Category
		}
		return sorted[i].ID < sorted[j].ID
	})

	// Build summary.by_category.
	byCategory := make(map[string]CategoryCounts)
	for _, f := range findings {
		cc := byCategory[f.Category]
		switch f.Severity {
		case finding.SeverityRed:
			cc.Red++
		case finding.SeverityYellow:
			cc.Yellow++
		case finding.SeverityInfo:
			cc.Info++
		}
		byCategory[f.Category] = cc
	}

	// top_reds: up to 5 red finding IDs.
	var topReds []string
	for _, f := range sorted {
		if f.Severity == finding.SeverityRed && len(topReds) < 5 {
			topReds = append(topReds, f.ID)
		}
	}
	// top_yellows: up to 10 yellow finding IDs.
	var topYellows []string
	for _, f := range sorted {
		if f.Severity == finding.SeverityYellow && len(topYellows) < 10 {
			topYellows = append(topYellows, f.ID)
		}
	}
	if topReds == nil {
		topReds = []string{}
	}
	if topYellows == nil {
		topYellows = []string{}
	}

	// Cadence recommendation.
	cadence := ""
	if consecutiveGreen >= 3 && status == finding.StatusGreen {
		cadence = "consider_biweekly"
	} else if status == finding.StatusGreen {
		cadence = "keep_weekly"
	}

	return Report{
		SchemaVersion: schemaVersion,
		EngineVersion: engineVersion,
		GeneratedAt:   now,
		Platform:      currentPlatform(),
		Header: Header{
			Status: string(status),
			Counts: Counts{
				Red:                reds,
				Yellow:             yellows,
				Info:               infos,
				NewSinceLast:       newCount,
				AllowListedThisRun: allowListedThisRun,
			},
			ReposScanned:          reposScanned,
			ReposFailed:           reposFailed,
			ConsecutiveGreen:      consecutiveGreen,
			CadenceRecommendation: cadence,
		},
		Findings: sorted,
		Summary: Summary{
			ByCategory: byCategory,
			TopReds:    topReds,
			TopYellows: topYellows,
		},
		Errors: errors,
		Delta:  delta,
	}
}

// RenderJSON serialises r as canonical JSON: 2-space indent, UTF-8, sorted
// map keys (Go's encoding/json sorts map keys deterministically).
func RenderJSON(r Report) ([]byte, error) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal report JSON: %w", err)
	}
	return append(data, '\n'), nil
}

// RenderMarkdown renders r as GitHub-Flavored Markdown.
func RenderMarkdown(r Report) string {
	var sb strings.Builder

	// Header.
	dateStr := parseDate(r.GeneratedAt)
	sb.WriteString(fmt.Sprintf("# RosmarOps — %s\n\n", dateStr))
	sb.WriteString(fmt.Sprintf("Status: %s %s\n", statusEmoji(r.Header.Status), strings.ToUpper(r.Header.Status)))
	sb.WriteString(fmt.Sprintf("Red: %d   Yellow: %d   New since last: %d   Allow-listed: %d\n",
		r.Header.Counts.Red, r.Header.Counts.Yellow, r.Header.Counts.NewSinceLast,
		r.Header.Counts.AllowListedThisRun))
	if r.Header.ReposFailed > 0 {
		sb.WriteString(fmt.Sprintf("Repos scanned: %d (%d with error)\n", r.Header.ReposScanned, r.Header.ReposFailed))
	} else {
		sb.WriteString(fmt.Sprintf("Repos scanned: %d\n", r.Header.ReposScanned))
	}
	sb.WriteString(fmt.Sprintf("Consecutive greens: %d\n", r.Header.ConsecutiveGreen))
	sb.WriteString(fmt.Sprintf("Engine: rosmarops %s   Schema: %d\n", r.EngineVersion, r.SchemaVersion))

	if r.Header.CadenceRecommendation == "consider_biweekly" {
		sb.WriteString("\n> 💡 3+ consecutive greens. Consider switching to biweekly cadence (`rosmarops schedule`).\n")
	}

	// Errors.
	if len(r.Errors) > 0 {
		sb.WriteString("\n## Scan Errors\n\n")
		for _, e := range r.Errors {
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", e.Scope, e.Message))
		}
	}

	// Findings.
	if len(r.Findings) == 0 {
		sb.WriteString("\n## Findings\n\nNo findings this run. ✅\n")
	} else {
		sb.WriteString("\n## Findings\n\n")
		for _, f := range r.Findings {
			sb.WriteString(fmt.Sprintf("### %s %s · %s\n\n",
				severityEmoji(f.Severity), strings.ToUpper(string(f.Severity)), f.Title))
			if f.Evidence.File != "" {
				sb.WriteString(fmt.Sprintf("File: `%s`", f.Evidence.File))
				if f.Evidence.Line > 0 {
					sb.WriteString(fmt.Sprintf(" (line %d)", f.Evidence.Line))
				}
				sb.WriteString("\n")
			}
			if f.Evidence.RedactedPreview != "" {
				sb.WriteString(fmt.Sprintf("Preview: `%s`\n", f.Evidence.RedactedPreview))
			}
			if f.Evidence.ExtensionID != "" {
				sb.WriteString(fmt.Sprintf("Extension: `%s`\n", f.Evidence.ExtensionID))
			}
			if f.Evidence.MCPServer != "" {
				sb.WriteString(fmt.Sprintf("MCP server: `%s` (client: %s)\n", f.Evidence.MCPServer, f.Evidence.MCPClient))
			}
			sb.WriteString(fmt.Sprintf("Why it matters: %s\n", f.WhyItMatters))
			sb.WriteString(fmt.Sprintf("Suggested action: %s\n", f.SuggestedAction))
			sb.WriteString(fmt.Sprintf("Confidence: %s\n\n", f.Confidence))
		}
	}

	// Manual checklist.
	sb.WriteString("---\n\n## Manual checklist\n\n")
	sb.WriteString("- [ ] Read the header\n")
	sb.WriteString("- [ ] Reviewed RED findings (if any)\n")
	sb.WriteString("- [ ] Reviewed YELLOW findings (if any)\n")
	sb.WriteString("- [ ] Updated allow-list with notes (if accepting any)\n")
	sb.WriteString("- [ ] Checked GitHub Security tab in browser\n")
	sb.WriteString("- [ ] Checked provider dashboards for unusual activity\n")
	sb.WriteString("- [ ] Rotated or revoked anything that needed it\n")

	return sb.String()
}

// DefaultReportDir returns the default directory for report files.
func DefaultReportDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".rosmarops", "reports"), nil
}

// Filename returns the canonical filename (without extension) for a report.
// Format: report-<YYYY-MM-DD>__<COLOR>__<R>R-<Y>Y
func Filename(r Report) string {
	dateStr := parseDate(r.GeneratedAt)
	color := strings.ToUpper(r.Header.Status)
	return fmt.Sprintf("report-%s__%s__%dR-%dY",
		dateStr, color, r.Header.Counts.Red, r.Header.Counts.Yellow)
}

// Write writes both the Markdown and JSON sidecar files to dir.
// It creates dir if it does not exist (only under the allowed write scope).
func Write(r Report, dir string) (mdPath, jsonPath string, err error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("create report dir %s: %w", dir, err)
	}

	base := Filename(r)
	mdPath = filepath.Join(dir, base+".md")
	jsonPath = filepath.Join(dir, base+".json")

	md := RenderMarkdown(r)
	if err := os.WriteFile(mdPath, []byte(md), 0o600); err != nil {
		return "", "", fmt.Errorf("write markdown report: %w", err)
	}

	jsonData, err := RenderJSON(r)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(jsonPath, jsonData, 0o600); err != nil {
		return "", "", fmt.Errorf("write json sidecar: %w", err)
	}

	return mdPath, jsonPath, nil
}

// severityOrder maps severity to a sort key (lower = higher priority in output).
func severityOrder(s finding.Severity) int {
	switch s {
	case finding.SeverityRed:
		return 0
	case finding.SeverityYellow:
		return 1
	default:
		return 2
	}
}

func statusEmoji(status string) string {
	switch status {
	case "red":
		return "🔴"
	case "yellow":
		return "🟡"
	default:
		return "🟢"
	}
}

func severityEmoji(s finding.Severity) string {
	switch s {
	case finding.SeverityRed:
		return "🔴"
	case finding.SeverityYellow:
		return "🟡"
	default:
		return "ℹ️"
	}
}

func parseDate(rfc3339 string) string {
	t, err := time.Parse(time.RFC3339, rfc3339)
	if err != nil {
		return rfc3339
	}
	return t.UTC().Format("2006-01-02")
}

// goos is the system the run is on; the tests set it.
var goos = runtime.GOOS

func currentPlatform() Platform {
	osName := goos
	if osName == "darwin" {
		osName = "macos"
	}
	return Platform{
		OS:   osName,
		Arch: runtime.GOARCH,
	}
}
