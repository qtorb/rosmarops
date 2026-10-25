package shellprofile_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/shellprofile"
)

// fakeProfiles is a stable set of profiles used across tests.
var fakeProfiles = []platform.ShellProfile{
	{Scope: "CurrentUserCurrentHost", Path: `C:\Users\test\Documents\PowerShell\Microsoft.PowerShell_profile.ps1`, SHA256: "aaaa" + rep("a", 60)},
	{Scope: "CurrentUserAllHosts", Path: `C:\Users\test\Documents\PowerShell\profile.ps1`, SHA256: ""},
	{Scope: "AllUsersCurrentHost", Path: `C:\Windows\System32\WindowsPowerShell\v1.0\Microsoft.PowerShell_profile.ps1`, SHA256: ""},
	{Scope: "AllUsersAllHosts", Path: `C:\Windows\System32\WindowsPowerShell\v1.0\profile.ps1`, SHA256: ""},
}

func rep(s string, n int) string {
	out := ""
	for range n {
		out += s
	}
	return out
}

func TestScanShellProfiles_NoProfiles_NoFindings(t *testing.T) {
	result := shellprofile.ScanShellProfiles(nil, "")
	if len(result.Findings) != 0 {
		t.Errorf("nil profiles: want 0 findings, got %d", len(result.Findings))
	}
	if result.NewSHA != "" {
		t.Errorf("nil profiles: want empty NewSHA, got %q", result.NewSHA)
	}
}

func TestScanShellProfiles_NoBaseline_NoFinding(t *testing.T) {
	// First run: no baseline → no finding, but SHA is computed.
	result := shellprofile.ScanShellProfiles(fakeProfiles, "")
	if len(result.Findings) != 0 {
		t.Errorf("no baseline: want 0 findings (just establish hash), got %d", len(result.Findings))
	}
	if result.NewSHA == "" {
		t.Error("no baseline: want non-empty NewSHA")
	}
}

func TestScanShellProfiles_Unchanged_NoFinding(t *testing.T) {
	// Establish baseline.
	first := shellprofile.ScanShellProfiles(fakeProfiles, "")
	baseline := first.NewSHA

	// Second run with same profiles → no finding.
	second := shellprofile.ScanShellProfiles(fakeProfiles, baseline)
	if len(second.Findings) != 0 {
		t.Errorf("unchanged profiles: want 0 findings, got %d", len(second.Findings))
	}
	if second.NewSHA != baseline {
		t.Errorf("unchanged profiles: NewSHA should equal baseline, got %q vs %q", second.NewSHA, baseline)
	}
}

func TestScanShellProfiles_Changed_RedFinding(t *testing.T) {
	first := shellprofile.ScanShellProfiles(fakeProfiles, "")
	baseline := first.NewSHA

	// Simulate profile modification: SHA of CurrentUserCurrentHost changes.
	modified := make([]platform.ShellProfile, len(fakeProfiles))
	copy(modified, fakeProfiles)
	modified[0].SHA256 = "bbbb" + rep("b", 60)

	result := shellprofile.ScanShellProfiles(modified, baseline)

	if len(result.Findings) != 1 {
		t.Fatalf("modified profile: want 1 finding, got %d", len(result.Findings))
	}
	f := result.Findings[0]
	if f.Severity != "red" {
		t.Errorf("finding severity = %q, want red", f.Severity)
	}
	if f.Category != "shell_profile_modified" {
		t.Errorf("finding category = %q, want shell_profile_modified", f.Category)
	}
	if f.WhyItMatters == "" {
		t.Error("finding WhyItMatters must not be empty")
	}
	if f.SuggestedAction == "" {
		t.Error("finding SuggestedAction must not be empty")
	}
}

func TestScanShellProfiles_NewProfileAppears_RedFinding(t *testing.T) {
	// Baseline with all SHAs empty.
	allEmpty := []platform.ShellProfile{
		{Scope: "CurrentUserCurrentHost", Path: `C:\test\profile.ps1`, SHA256: ""},
	}
	first := shellprofile.ScanShellProfiles(allEmpty, "")
	baseline := first.NewSHA

	// Now the profile exists.
	withFile := []platform.ShellProfile{
		{Scope: "CurrentUserCurrentHost", Path: `C:\test\profile.ps1`, SHA256: "cccc" + rep("c", 60)},
	}
	result := shellprofile.ScanShellProfiles(withFile, baseline)
	if len(result.Findings) != 1 {
		t.Fatalf("new profile file: want 1 finding, got %d", len(result.Findings))
	}
}

func TestCombinedSHA_DeterministicAcrossOrder(t *testing.T) {
	a := []platform.ShellProfile{
		{Scope: "A", SHA256: "aaa" + rep("a", 61)},
		{Scope: "B", SHA256: "bbb" + rep("b", 61)},
	}
	b := []platform.ShellProfile{
		{Scope: "B", SHA256: "bbb" + rep("b", 61)},
		{Scope: "A", SHA256: "aaa" + rep("a", 61)},
	}
	if shellprofile.CombinedSHA(a) != shellprofile.CombinedSHA(b) {
		t.Error("CombinedSHA must be order-independent")
	}
}
