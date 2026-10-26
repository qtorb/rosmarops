package vscode_test

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/vscode"
	"github.com/qtorb/rosmarops/internal/state"
)

// helpers

func ext(id, version string, enabled bool) platform.VSCodeExtension {
	return platform.VSCodeExtension{ID: id, Version: version, Enabled: enabled}
}

func baselined(id, version string) state.VSCodeExtension {
	return state.VSCodeExtension{ID: id, Version: version}
}

func allowed(id string) config.AllowEntryID {
	return config.AllowEntryID{ID: id, Note: "approved"}
}

// now is the run's time; notFirst is a reference that is not empty, so the
// run is not a first run (decision 2026-10-26, agentes y MCP nuevos, A5).
var (
	now      = time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	notFirst = []state.VSCodeExtension{baselined("seen.before", "1.0.0")}
)

// tests

func TestScanExtensions_EmptyCurrentAndBaseline(t *testing.T) {
	r := vscode.ScanExtensions(nil, nil, nil, now)
	if len(r.Findings) != 0 {
		t.Errorf("expected 0 findings, got %d", len(r.Findings))
	}
	if r.AllowListedCount != 0 {
		t.Errorf("expected 0 allow-listed, got %d", r.AllowListedCount)
	}
}

func TestScanExtensions_NotInReference_AllNew(t *testing.T) {
	current := []platform.VSCodeExtension{
		ext("ms-python.python", "2026.4.0", true),
		ext("eamodio.gitlens", "17.12.2", true),
	}
	r := vscode.ScanExtensions(current, notFirst, nil, now)
	if len(r.Findings) != 2 {
		t.Fatalf("expected 2 findings, got %d", len(r.Findings))
	}
	for _, f := range r.Findings {
		if f.Severity != finding.SeverityYellow {
			t.Errorf("expected yellow, got %s", f.Severity)
		}
		if f.Category != "vscode_extension_new" {
			t.Errorf("expected vscode_extension_new, got %s", f.Category)
		}
		if f.WhyItMatters == "" {
			t.Error("why_it_matters must not be empty")
		}
		if f.SuggestedAction == "" {
			t.Error("suggested_action must not be empty")
		}
		if f.Confidence != "high" {
			t.Errorf("expected confidence=high, got %s", f.Confidence)
		}
	}
}

func TestScanExtensions_InBaseline_NoFindings(t *testing.T) {
	current := []platform.VSCodeExtension{
		ext("ms-python.python", "2026.4.0", true),
	}
	baseline := []state.VSCodeExtension{
		baselined("ms-python.python", "2026.4.0"),
	}
	r := vscode.ScanExtensions(current, baseline, nil, now)
	if len(r.Findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(r.Findings))
	}
}

func TestScanExtensions_AllowListed_Suppressed(t *testing.T) {
	current := []platform.VSCodeExtension{
		ext("ritwickdey.liveserver", "5.7.10", true),
	}
	r := vscode.ScanExtensions(current, notFirst, []config.AllowEntryID{
		allowed("ritwickdey.liveserver"),
	}, now)
	if len(r.Findings) != 0 {
		t.Fatalf("expected 0 findings (allow-listed), got %d", len(r.Findings))
	}
	if r.AllowListedCount != 1 {
		t.Errorf("expected allow-listed count 1, got %d", r.AllowListedCount)
	}
}

func TestScanExtensions_VersionUpgrade_IsNewFinding(t *testing.T) {
	// The old version is in baseline, the new version is a new finding.
	current := []platform.VSCodeExtension{
		ext("eamodio.gitlens", "17.12.2", true),
	}
	baseline := []state.VSCodeExtension{
		baselined("eamodio.gitlens", "17.11.0"),
	}
	r := vscode.ScanExtensions(current, baseline, nil, now)
	if len(r.Findings) != 1 {
		t.Fatalf("expected 1 finding for version upgrade, got %d", len(r.Findings))
	}
	if r.Findings[0].Evidence.PackageVersion != "17.12.2" {
		t.Errorf("unexpected version in evidence: %s", r.Findings[0].Evidence.PackageVersion)
	}
}

func TestScanExtensions_DisabledExtension_StillReported(t *testing.T) {
	// Disabled extensions are still attack surface: bits on disk, one click to reactivate.
	current := []platform.VSCodeExtension{
		ext("suspicious.extension", "1.0.0", false),
	}
	r := vscode.ScanExtensions(current, notFirst, nil, now)
	if len(r.Findings) != 1 {
		t.Fatalf("expected 1 finding for disabled extension, got %d", len(r.Findings))
	}
	extra, ok := r.Findings[0].Evidence.Extra.(map[string]any)
	if !ok {
		t.Fatal("evidence.extra must be map[string]any")
	}
	if extra["enabled"] != false {
		t.Errorf("expected enabled=false in extra, got %v", extra["enabled"])
	}
}

func TestScanExtensions_CaseInsensitiveID(t *testing.T) {
	// VS Code IDs are case-insensitive; normalize to lowercase before comparison.
	current := []platform.VSCodeExtension{
		ext("MS-Python.Python", "2026.4.0", true),
	}
	baseline := []state.VSCodeExtension{
		baselined("ms-python.python", "2026.4.0"),
	}
	r := vscode.ScanExtensions(current, baseline, nil, now)
	if len(r.Findings) != 0 {
		t.Fatalf("expected 0 findings (case-insensitive match), got %d", len(r.Findings))
	}
}

func TestScanExtensions_FindingID_DeterministicAndPrefixed(t *testing.T) {
	current := []platform.VSCodeExtension{
		ext("ms-python.python", "2026.4.0", true),
	}
	r1 := vscode.ScanExtensions(current, notFirst, nil, now)
	r2 := vscode.ScanExtensions(current, notFirst, nil, now)
	if len(r1.Findings) != 1 || len(r2.Findings) != 1 {
		t.Fatal("expected 1 finding per call")
	}
	id := r1.Findings[0].ID
	if r1.Findings[0].ID != r2.Findings[0].ID {
		t.Errorf("finding ID not deterministic: %s vs %s", id, r2.Findings[0].ID)
	}
	if !strings.HasPrefix(id, "vscode_extension_new:") {
		t.Errorf("finding ID has wrong prefix: %s", id)
	}
}

func TestScanExtensions_EvidenceExtensionID(t *testing.T) {
	current := []platform.VSCodeExtension{
		ext("github.vscode-github-actions", "0.31.5", true),
	}
	r := vscode.ScanExtensions(current, notFirst, nil, now)
	if len(r.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(r.Findings))
	}
	if r.Findings[0].Evidence.ExtensionID != "github.vscode-github-actions" {
		t.Errorf("unexpected extension_id: %s", r.Findings[0].Evidence.ExtensionID)
	}
}

func TestScanExtensions_MixedBaselineAndNew(t *testing.T) {
	current := []platform.VSCodeExtension{
		ext("ms-python.python", "2026.4.0", true),    // in baseline
		ext("eamodio.gitlens", "17.12.2", true),      // NEW
		ext("ritwickdey.liveserver", "5.7.10", true), // allow-listed
	}
	baseline := []state.VSCodeExtension{
		baselined("ms-python.python", "2026.4.0"),
	}
	allowList := []config.AllowEntryID{
		allowed("ritwickdey.liveserver"),
	}
	r := vscode.ScanExtensions(current, baseline, allowList, now)
	if len(r.Findings) != 1 {
		t.Fatalf("expected 1 finding, got %d: %v", len(r.Findings), r.Findings)
	}
	if r.Findings[0].Evidence.ExtensionID != "eamodio.gitlens" {
		t.Errorf("wrong finding: %s", r.Findings[0].Evidence.ExtensionID)
	}
	if r.AllowListedCount != 1 {
		t.Errorf("expected allow-listed count 1, got %d", r.AllowListedCount)
	}
}

// Decision 2026-10-26 (agentes y MCP nuevos, A5): a first run (empty
// reference) records every extension as the starting point and reports only
// the ones installed in the last 7 days (with an install date); the
// reference to persist holds them all.
func TestScanExtensions_FirstRun(t *testing.T) {
	old := ext("ms-python.python", "2026.4.0", true)
	old.InstalledAt = now.Add(-30 * 24 * time.Hour)
	undated := ext("eamodio.gitlens", "17.12.2", true)
	recent := ext("ritwickdey.liveserver", "5.7.10", true)
	recent.InstalledAt = now.Add(-2 * 24 * time.Hour)
	r := vscode.ScanExtensions([]platform.VSCodeExtension{old, undated}, nil, nil, now)
	if len(r.Findings) != 0 || !r.FirstRun || r.Recorded != 2 || r.Recent != 0 || len(r.NewBaseline) != 2 {
		t.Errorf("first run, none recent: %+v", r)
	}
	r = vscode.ScanExtensions([]platform.VSCodeExtension{old, undated, recent}, nil, nil, now)
	if len(r.Findings) != 1 || r.Findings[0].Evidence.ExtensionID != "ritwickdey.liveserver" || r.Recent != 1 || r.Recorded != 3 {
		t.Errorf("first run, one recent: %+v", r)
	}
}

// A5: the reference is updated on every run: two runs with the same
// extensions, the second reports nothing; first_seen is kept.
func TestScanExtensions_ReferenceUpdatedEveryRun(t *testing.T) {
	current := []platform.VSCodeExtension{ext("ms-python.python", "2026.4.0", true)}
	r1 := vscode.ScanExtensions(current, notFirst, nil, now)
	if len(r1.Findings) != 1 || len(r1.NewBaseline) != 1 || r1.NewBaseline[0].FirstSeen != now.Format(time.RFC3339) {
		t.Fatalf("run 1: %+v", r1)
	}
	r2 := vscode.ScanExtensions(current, r1.NewBaseline, nil, now.Add(24*time.Hour))
	if len(r2.Findings) != 0 || r2.FirstRun || r2.NewBaseline[0].FirstSeen != r1.NewBaseline[0].FirstSeen {
		t.Errorf("run 2: %+v", r2)
	}
	// With nothing else, the status can be green again (L59.5).
	if st := finding.Reducer(r2.Findings, 0); st != finding.StatusGreen {
		t.Errorf("run 2 status %s", st)
	}
}

// A3, A5: an agent extension never gives vscode_extension_new, first run or
// not, recent or not: the graph and its memory cover it.
func TestScanExtensions_AgentExtensionsNotReported(t *testing.T) {
	copilot := ext("GitHub.Copilot", "1.300.0", true)
	copilot.InstalledAt = now.Add(-time.Hour)
	for _, base := range [][]state.VSCodeExtension{nil, notFirst} {
		if r := vscode.ScanExtensions([]platform.VSCodeExtension{copilot}, base, nil, now); len(r.Findings) != 0 || len(r.NewBaseline) != 1 {
			t.Errorf("reference %v: %+v", base, r)
		}
	}
}
