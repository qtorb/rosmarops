package avexclusions_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/avexclusions"
	"github.com/qtorb/rosmarops/internal/state"
)

func TestScanAVExclusions_NoExclusions_NoFindings(t *testing.T) {
	result := avexclusions.ScanAVExclusions(nil, nil, nil)
	if len(result.Findings) != 0 {
		t.Errorf("no exclusions: want 0 findings, got %d", len(result.Findings))
	}
}

func TestScanAVExclusions_NewPathExclusion_RedFinding(t *testing.T) {
	excls := []platform.DefenderExclusion{
		{Kind: "path", Value: `C:\evil\path`},
	}
	result := avexclusions.ScanAVExclusions(excls, nil, nil)
	if len(result.Findings) != 1 {
		t.Fatalf("new path exclusion: want 1 finding, got %d", len(result.Findings))
	}
	f := result.Findings[0]
	if f.Severity != "red" {
		t.Errorf("severity = %q, want red", f.Severity)
	}
	if f.Category != "av_exclusion_new" {
		t.Errorf("category = %q, want av_exclusion_new", f.Category)
	}
	if f.WhyItMatters == "" {
		t.Error("WhyItMatters must not be empty")
	}
	if f.SuggestedAction == "" {
		t.Error("SuggestedAction must not be empty")
	}
}

func TestScanAVExclusions_KnownExclusion_NoFinding(t *testing.T) {
	excls := []platform.DefenderExclusion{
		{Kind: "process", Value: "node.exe"},
	}
	baseline := []state.AVExclusion{
		{Kind: "process", Value: "node.exe"},
	}
	result := avexclusions.ScanAVExclusions(excls, baseline, nil)
	if len(result.Findings) != 0 {
		t.Errorf("known exclusion: want 0 findings, got %d", len(result.Findings))
	}
}

func TestScanAVExclusions_AllowListed_Suppressed(t *testing.T) {
	excls := []platform.DefenderExclusion{
		{Kind: "extension", Value: ".ps1"},
	}
	allow := []config.AllowEntryPath{
		{Path: ".ps1", Note: "PowerShell scripts, dev tooling"},
	}
	result := avexclusions.ScanAVExclusions(excls, nil, allow)
	if len(result.Hits) != 1 || result.Hits[0].List != config.ListAVExclusions || result.Hits[0].Note != allow[0].Note { // B9
		t.Errorf("hits %+v", result.Hits)
	}
	if len(result.Findings) != 0 {
		t.Errorf("allow-listed exclusion: want 0 findings, got %d", len(result.Findings))
	}
	if result.AllowListedCount != 1 {
		t.Errorf("AllowListedCount = %d, want 1", result.AllowListedCount)
	}
}

func TestScanAVExclusions_CaseInsensitiveBaseline(t *testing.T) {
	// Windows paths are case-insensitive.
	excls := []platform.DefenderExclusion{
		{Kind: "path", Value: `C:\TOOLS\Dev`},
	}
	baseline := []state.AVExclusion{
		{Kind: "path", Value: `C:\tools\dev`},
	}
	result := avexclusions.ScanAVExclusions(excls, baseline, nil)
	if len(result.Findings) != 0 {
		t.Errorf("case-insensitive path: want 0 findings, got %d", len(result.Findings))
	}
}

func TestScanAVExclusions_MultipleExclusions_IndependentFindings(t *testing.T) {
	excls := []platform.DefenderExclusion{
		{Kind: "path", Value: `C:\new1`},
		{Kind: "path", Value: `C:\new2`},
		{Kind: "process", Value: "evil.exe"},
	}
	result := avexclusions.ScanAVExclusions(excls, nil, nil)
	if len(result.Findings) != 3 {
		t.Errorf("3 new exclusions: want 3 findings, got %d", len(result.Findings))
	}
}

func TestScanAVExclusions_NewBaselineBuilt(t *testing.T) {
	excls := []platform.DefenderExclusion{
		{Kind: "path", Value: `C:\tools`},
		{Kind: "extension", Value: ".log"},
	}
	result := avexclusions.ScanAVExclusions(excls, nil, nil)
	if len(result.NewBaseline) != 2 {
		t.Errorf("NewBaseline len = %d, want 2", len(result.NewBaseline))
	}
}
