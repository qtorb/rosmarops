package trust_test

// BV-249 §4 (CTO-59 §4.3, rule b-iii): an agent that writes anywhere on the
// computer without a person writes a startup file, which runs later and reads
// every key. The two cases CTO-59 §4.1 found incoherent go from 75 yellow D
// to 31 red D; no factory figure of R2 changes (TestR2FactoryScores).

import (
	"maps"
	"testing"

	"github.com/qtorb/rosmarops/internal/declared"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

// r2ClaudeCodeAutoSandbox is Claude Code alone on R2 in «auto», its sandbox
// on and reads limited to the working directories (CTO-58).
func r2ClaudeCodeAutoSandbox(t *testing.T) (report.V2, func(*pipeline.Input)) {
	t.Helper()
	file := platform.ClaudeSettingsFile{
		Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead,
		Permissions: platform.ClaudePermissions{
			DefaultMode: "auto", BlockReadsOutsideWorkingDirectories: true,
			Sandbox: platform.ClaudeSandbox{Enabled: true, EnabledSet: true},
		},
	}
	settings := r2Settings(file)
	g := r2Assemble(t, pipeline.Input{
		Settings: settings, CredFiles: platform.CredentialFileScan{Entries: r2Creds()},
		Instructions: platform.InstructionScan{Files: r2Instr("claude-code", fcClaudeDir)},
	})
	mod := func(in *pipeline.Input) { in.Settings = r2Settings(file) }
	return r2BuildV2(t, &g, &settings, nil), mod
}

func TestBV249_StartupFileFigures(t *testing.T) {
	workspace := maps.Clone(declared.Factory(declared.Cursor))
	workspace[declared.C3ReadAccess] = declared.ReadWorkspace
	cursor := r2CursorWith(t, workspace, false)
	claude, claudeMod := r2ClaudeCodeAutoSandbox(t)
	for _, c := range []struct {
		name, product string
		v             report.V2
		mod           func(*pipeline.Input)
	}{
		{"Cursor in Auto-review with Workspace", "cursor", cursor, func(in *pipeline.Input) { in.Cursor.Declared = workspace }},
		{"Claude Code in auto with sandbox and read limit", "claude-code", claude, claudeMod},
	} {
		got, band, id := r2AgentIndexID(t, c.v, c.product)
		findings, assume := r2Detail(c.v, id)
		a := classOfSingle(t, c.v, c.product, c.mod)
		t.Logf("%s: %d (%s) class %s %v %v", c.name, got, band, a.Class, findings, assume)
		if got != 31 || band != "exposed" || a.Class != "D" {
			t.Errorf("%s: %d (%s) class %s, want 31 exposed D", c.name, got, band, a.Class)
		}
	}
}
