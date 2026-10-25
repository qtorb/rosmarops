package report_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/report"
)

func emptyReport(t *testing.T) report.Report {
	t.Helper()
	return report.Build("0.1.0-dev", nil, nil, 0, 0, 0, 0, nil)
}

func TestBuild_EmptyReport_IsGreen(t *testing.T) {
	r := emptyReport(t)
	if r.Header.Status != "green" {
		t.Errorf("empty report: status = %q, want %q", r.Header.Status, "green")
	}
	if r.Header.Counts.Red != 0 || r.Header.Counts.Yellow != 0 {
		t.Errorf("empty report: unexpected counts %+v", r.Header.Counts)
	}
}

func TestBuild_RedFinding_StatusIsRed(t *testing.T) {
	findings := []finding.Finding{
		finding.NewFinding("binary_hash_changed:abcd1234abcd1234", finding.SeverityRed, "binary_hash_changed", "Binary changed"),
	}
	r := report.Build("0.1.0-dev", findings, nil, 0, 0, 0, 0, nil)
	if r.Header.Status != "red" {
		t.Errorf("red finding: status = %q, want red", r.Header.Status)
	}
	if r.Header.Counts.Red != 1 {
		t.Errorf("red finding: counts.red = %d, want 1", r.Header.Counts.Red)
	}
}

func TestBuild_ScanError_ForcesYellow(t *testing.T) {
	errors := []finding.ScanError{{Scope: "repo:/tmp/foo", Message: "timeout"}}
	r := report.Build("0.1.0-dev", nil, errors, 0, 0, 0, 0, nil)
	if r.Header.Status != "yellow" {
		t.Errorf("scan error: status = %q, want yellow", r.Header.Status)
	}
}

func TestBuild_Delta_AddedAndResolved(t *testing.T) {
	prev := report.Build(
		"0.1.0-dev",
		[]finding.Finding{
			{ID: "cat:aaaa1111aaaa1111", Severity: finding.SeverityYellow},
		},
		nil, 0, 0, 0, 0, nil,
	)

	curr := report.Build(
		"0.1.0-dev",
		[]finding.Finding{
			{ID: "cat:bbbb2222bbbb2222", Severity: finding.SeverityYellow},
		},
		nil, 0, 0, 0, 0, &prev,
	)

	if curr.Delta == nil {
		t.Fatal("delta is nil when prev is provided")
	}
	if len(curr.Delta.AddedFindingIDs) != 1 || curr.Delta.AddedFindingIDs[0] != "cat:bbbb2222bbbb2222" {
		t.Errorf("added: %v", curr.Delta.AddedFindingIDs)
	}
	if len(curr.Delta.ResolvedFindingIDs) != 1 || curr.Delta.ResolvedFindingIDs[0] != "cat:aaaa1111aaaa1111" {
		t.Errorf("resolved: %v", curr.Delta.ResolvedFindingIDs)
	}
	if curr.Header.Counts.NewSinceLast != 1 {
		t.Errorf("new_since_last: got %d, want 1", curr.Header.Counts.NewSinceLast)
	}
}

func TestBuild_FindingsSortedRedFirst(t *testing.T) {
	findings := []finding.Finding{
		{ID: "cat:cccc3333cccc3333", Severity: finding.SeverityYellow, Category: "vscode_extension_new"},
		{ID: "cat:dddd4444dddd4444", Severity: finding.SeverityRed, Category: "secret_tracked_file"},
		{ID: "cat:eeee5555eeee5555", Severity: finding.SeverityInfo, Category: "git_hook_new_or_modified"},
	}
	r := report.Build("0.1.0-dev", findings, nil, 0, 0, 0, 0, nil)
	if r.Findings[0].Severity != finding.SeverityRed {
		t.Errorf("first finding should be red, got %q", r.Findings[0].Severity)
	}
	if r.Findings[2].Severity != finding.SeverityInfo {
		t.Errorf("last finding should be info, got %q", r.Findings[2].Severity)
	}
}

func TestRenderJSON_ValidJSON(t *testing.T) {
	r := emptyReport(t)
	data, err := report.RenderJSON(r)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	var probe any
	if err := json.Unmarshal(data, &probe); err != nil {
		t.Errorf("RenderJSON output is not valid JSON: %v", err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("RenderJSON output must end with newline")
	}
}

func TestRenderJSON_SchemaVersionPresent(t *testing.T) {
	r := emptyReport(t)
	data, _ := report.RenderJSON(r)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	if m["schema_version"] == nil {
		t.Error("schema_version missing from JSON output")
	}
}

func TestRenderMarkdown_ContainsStatus(t *testing.T) {
	r := emptyReport(t)
	md := report.RenderMarkdown(r)
	if !strings.Contains(md, "GREEN") {
		t.Error("markdown should contain status GREEN")
	}
	if !strings.Contains(md, "🟢") {
		t.Error("markdown should contain green emoji")
	}
}

func TestRenderMarkdown_ContainsManualChecklist(t *testing.T) {
	r := emptyReport(t)
	md := report.RenderMarkdown(r)
	if !strings.Contains(md, "Manual checklist") {
		t.Error("markdown should contain manual checklist")
	}
}

func TestFilename_Format(t *testing.T) {
	r := report.Build("0.1.0-dev", nil, nil, 0, 0, 0, 0, nil)
	name := report.Filename(r)
	// Must start with "report-" and contain "__GREEN__0R-0Y"
	if !strings.HasPrefix(name, "report-") {
		t.Errorf("filename does not start with 'report-': %q", name)
	}
	if !strings.Contains(name, "__GREEN__0R-0Y") {
		t.Errorf("filename does not contain expected suffix: %q", name)
	}
}

func TestBuild_SummaryByCategory(t *testing.T) {
	findings := []finding.Finding{
		{ID: "cat:ffff6666ffff6666", Severity: finding.SeverityRed, Category: "secret_tracked_file"},
		{ID: "cat:gggg7777gggg7777", Severity: finding.SeverityYellow, Category: "vscode_extension_new"},
		{ID: "cat:hhhh8888hhhh8888", Severity: finding.SeverityYellow, Category: "vscode_extension_new"},
	}
	r := report.Build("0.1.0-dev", findings, nil, 0, 0, 0, 0, nil)
	if r.Summary.ByCategory["secret_tracked_file"].Red != 1 {
		t.Errorf("by_category[secret_tracked_file].red: got %d, want 1", r.Summary.ByCategory["secret_tracked_file"].Red)
	}
	if r.Summary.ByCategory["vscode_extension_new"].Yellow != 2 {
		t.Errorf("by_category[vscode_extension_new].yellow: got %d, want 2", r.Summary.ByCategory["vscode_extension_new"].Yellow)
	}
}
