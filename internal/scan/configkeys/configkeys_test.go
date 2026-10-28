package configkeys

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/platform"
)

// B5: one red finding per file and key, from every scan, each once, with the
// catalog's English texts.
func TestFindings(t *testing.T) {
	managed := platform.ClaudeSettingsFile{ConfigPath: "m.json", KeyConflicts: []string{"/Permissions"}}
	fs := Findings(
		platform.MCPScan{Sources: []platform.MCPSource{{ConfigPath: ".mcp.json", KeyConflicts: []string{"/mcpServers/s/Command", "/mcpServers/s/Command"}}}},
		platform.ClaudeSettingsScan{
			Files:   []platform.ClaudeSettingsFile{{ConfigPath: "settings.json", KeyConflicts: []string{"/permissions/DefaultMode"}}},
			Managed: []platform.ManagedSource{{Files: []platform.ClaudeSettingsFile{managed}}},
			VSCode:  &platform.VSCodeModeSetting{Path: "vs.json", KeyConflicts: []string{"/x/0/Value"}},
		},
		platform.CursorScan{Files: []platform.CursorFile{{Path: "sandbox.json", KeyConflicts: []string{"/ReadBoundary"}}}},
	)
	if len(fs) != 5 {
		t.Fatalf("%d findings, want 5: %+v", len(fs), fs)
	}
	for _, f := range fs {
		if f.Severity != finding.SeverityRed || f.Category != Category || !strings.HasPrefix(f.ID, Category+":") ||
			f.Evidence.File == "" || f.Evidence.Extra == nil ||
			!strings.Contains(f.Title, "capital letters") || f.WhyItMatters == "" || f.SuggestedAction == "" {
			t.Errorf("finding %+v", f)
		}
	}
	if len(Findings(platform.MCPScan{}, platform.ClaudeSettingsScan{}, platform.CursorScan{})) != 0 {
		t.Error("findings with no conflict")
	}
}

// BV-96: the key conflicts of the JSON credential files read with exact keys
// are findings too.
func TestFromCredentialFiles(t *testing.T) {
	fs := FromCredentialFiles(platform.CredentialFileScan{KeyConflicts: []platform.KeyConflict{
		{Path: "/h/hermes/auth.json", Pointer: "/credential_pool/p/0/Api_Key"},
		{Path: "/h/hermes/auth.json", Pointer: "/credential_pool/p/0/Api_Key"},
	}})
	if len(fs) != 1 || fs[0].Severity != finding.SeverityRed || fs[0].Evidence.File != "/h/hermes/auth.json" {
		t.Errorf("findings %+v", fs)
	}
}
