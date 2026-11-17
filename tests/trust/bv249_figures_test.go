package trust_test

// BV-249 §4 (CTO-59 §4.3, rule b-iii): an agent that writes anywhere on the
// computer without a person writes a startup file, which runs later and reads
// every key. The two cases CTO-59 §4.1 found incoherent go from 75 yellow D
// to 31 red D; no factory figure of R2 changes (TestR2FactoryScores).

import (
	"maps"
	"testing"

	"github.com/qtorb/rosmarops/internal/declared"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/profiles"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
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

// r2ClaudeCodeProfile is Claude Code alone on R2 with a measured profile
// applied as the report measures it, and the same change for R2-todos.
func r2ClaudeCodeProfile(t *testing.T, id string) (report.V2, func(*pipeline.Input)) {
	t.Helper()
	base := r2Settings(platform.ClaudeSettingsFile{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead})
	in := pipeline.Input{
		Settings: base, CredFiles: platform.CredentialFileScan{Entries: r2Creds()},
		Instructions: platform.InstructionScan{Files: r2Instr("claude-code", fcClaudeDir)},
		RepoRoots:    []string{fcRepo},
	}
	pin, ok := pipeline.Profile(in, id)
	if !ok {
		t.Fatalf("%s not applied", id)
	}
	g := r2Assemble(t, pin)
	settings := pin.Settings
	mod := func(all *pipeline.Input) {
		if p, ok := pipeline.Profile(*all, id); ok {
			all.Settings = p.Settings
		}
	}
	return r2BuildV2(t, &g, &settings, nil), mod
}

// BV-249 §3.1 (CTO-59 §1, Albert 16-11): Claude Code with P2 (its sandbox,
// autoAllowBashIfSandboxed false) on Linux and macOS is 99 green B with no
// finding, and R2-todos with it stays 13.
func TestBV249_ClaudeCodeP2(t *testing.T) {
	prev := graph.HostOS
	defer func() { graph.HostOS = prev }()
	for _, goos := range []string{"linux", "darwin"} {
		graph.HostOS = goos
		for _, id := range []string{profiles.P1, profiles.P2} {
			v, mod := r2ClaudeCodeProfile(t, id)
			got, band, aid := r2AgentIndexID(t, v, "claude-code")
			findings, assume := r2Detail(v, aid)
			a := classOfSingle(t, v, "claude-code", mod)
			all, _ := r2AllReportWith(t, mod)
			t.Logf("%s %s: %d (%s) class %s F %v; R2-todos %v; %v %v", goos, id, got, band, a.Class, ptrF(a.F), ptrI(all.Index.Machine.Value), findings, assume)
			for _, o := range v.AgentsOverview {
				for _, c := range o.Rows {
					if o.AgentID == aid && c.Row == rules.RowRunsCommands {
						want := [2]string{rules.DeciderYou, rules.ScopeNone}
						if id == profiles.P2 {
							want = [2]string{rules.DeciderYou, rules.ScopeSandbox} // «you, in the sandbox»
						}
						if got := [2]string{c.Decider, c.Scope}; got != want {
							t.Errorf("%s %s runs cell %v, want %v", goos, id, got, want)
						}
					}
				}
			}
			if id == profiles.P2 && len(findings) > 0 {
				t.Errorf("%s P2: findings %v, want none", goos, findings)
			}
			if id == profiles.P2 && (got != 99 || a.Class != "B" || all.Index.Machine.Value == nil || *all.Index.Machine.Value != 13) {
				t.Errorf("%s P2: %d (%s) class %s, R2-todos %v; want 99 B and 13", goos, got, band, a.Class, ptrI(all.Index.Machine.Value))
			}
		}
	}
}

func ptrF(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

func ptrI(i *int) any {
	if i == nil {
		return nil
	}
	return *i
}

// r2GrokP2 is Grok Build alone on R2 with P2 pasted in its config.toml, and
// the same change for R2-todos.
func r2GrokP2(t *testing.T) (report.V2, func(*pipeline.Input)) {
	t.Helper()
	scan := platform.GrokScan{ConfigDir: fcGrokDir, Exists: true, Layers: []platform.GrokLayer{{Kind: platform.GrokLayerUser, Path: fcGrokCfg, Status: platform.MCPSourceRead}}}
	settings := r2Settings()
	in := pipeline.Input{
		MCP: platform.MCPScan{Servers: graph.GrokMCPServers(scan)}, Settings: settings, Grok: scan,
		CredFiles:    platform.CredentialFileScan{Entries: r2Creds()},
		Instructions: platform.InstructionScan{Files: r2Instr("grok-build", fcGrokDir)},
	}
	pin, ok := pipeline.GrokProfile(in, profiles.P2)
	if !ok {
		t.Fatal("Grok P2 not applied")
	}
	g := r2Assemble(t, pin)
	mod := func(all *pipeline.Input) {
		if p, ok := pipeline.GrokProfile(*all, profiles.P2); ok {
			all.Grok = p.Grok
		}
	}
	return r2BuildV2(t, &g, &settings, func(v *report.V2Input) { v.Grok = pin.Grok }), mod
}

// BV-249 §3.2 (CTO-58 §1.3, Albert 17-11): Grok Build with P2 (strict) is 75
// yellow B on Linux and macOS; its reads row is the project and its runs row
// «you, in the sandbox».
func TestBV249_GrokBuildP2(t *testing.T) {
	prev := graph.HostOS
	defer func() { graph.HostOS = prev }()
	for _, goos := range []string{"linux", "darwin"} {
		graph.HostOS = goos
		v, mod := r2GrokP2(t)
		got, band, aid := r2AgentIndexID(t, v, "grok-build")
		findings, assume := r2Detail(v, aid)
		a := classOfSingle(t, v, "grok-build", mod)
		all, _ := r2AllReportWith(t, mod)
		t.Logf("%s Grok P2: %d (%s) class %s F %v; R2-todos %v; %v %v", goos, got, band, a.Class, ptrF(a.F), ptrI(all.Index.Machine.Value), findings, assume)
		// Albert 17-11: 75 yellow, with the class the measure gives once the
		// reads row follows strict and the runs row is «you, in the sandbox».
		if got != 75 || band != "open_gaps" || a.Class != "B" {
			t.Errorf("%s Grok P2: %d (%s) class %s, want 75 open_gaps B", goos, got, band, a.Class)
		}
		if c := a.Coherent(band); c == nil || !*c {
			t.Errorf("%s Grok P2: class %s not coherent with %s", goos, a.Class, band)
		}
		for _, o := range v.AgentsOverview {
			for _, c := range o.Rows {
				if o.AgentID == aid && c.Row == rules.RowReadsFiles && c.Scope != rules.ScopeSameProject {
					t.Errorf("%s Grok P2 reads row scope %s, want same_project", goos, c.Scope)
				}
				if o.AgentID == aid && c.Row == rules.RowRunsCommands && (c.Decider != rules.DeciderYou || c.Scope != rules.ScopeSandbox) {
					t.Errorf("%s Grok P2 runs row %s/%s, want you/sandbox", goos, c.Decider, c.Scope)
				}
			}
		}
	}
}
