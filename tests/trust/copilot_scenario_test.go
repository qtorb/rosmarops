package trust_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-140 (CTO-16 Copilot §7): end-to-end tests for GitHub Copilot in VS Code,
// through the full report pipeline (FromScans → FromCopilot → Infer → BuildV2),
// asserting the band, the findings and the overview rows, so CI catches drift
// from §7.

const fcCopilotDir = fcHome + "/AppData/Roaming/Code/User"

func copilotBoolp(b bool) *bool { return &b }

// copilotV2 runs the Copilot pipeline end to end and returns the v2 report.
func copilotV2(t *testing.T, scan platform.CopilotScan, claude []platform.ClaudeSettingsFile, instr []platform.InstructionFile, entries []platform.CredentialEntry) report.V2 {
	t.Helper()
	roots := []string{fcRepo}
	if claude == nil {
		claude = []platform.ClaudeSettingsFile{{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead}}
	}
	settings := platform.ClaudeSettingsScan{Files: claude, Trust: []platform.ProjectTrust{{Path: fcRepo, Accepted: true}}}
	mcp := platform.MCPScan{Servers: graph.CopilotMCPServers(scan)}
	frag, _, err := graph.FromScans(mcp, settings, platform.CredentialFileScan{Entries: entries}, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.Merge(frag, graph.FromCopilot(scan, true, fcNow))
	if len(instr) > 0 {
		frag = graph.AddInstructions(frag, platform.InstructionScan{Files: instr}, fcNow)
	}
	g, err := pipeline.BuildGraph(frag, pipeline.GraphOpts{
		Provenance: graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(fcSalt)},
		RepoRoots:  roots, Home: fcHome, Now: fcNow,
	})
	if err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings0(64), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: roots, V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: fcNow,
		ClaudeSettings: &settings, Home: fcHome,
	})
	return v
}

func strings0(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = '0'
	}
	return string(b)
}

// Copilot YOLO global (CTO-16 Copilot §7 case 2): chat.tools.global.autoApprove
// true in user settings — every row decides nobody.
func TestScenario_Copilot_YoloGlobal(t *testing.T) {
	scan := platform.CopilotScan{ConfigDir: fcCopilotDir, Present: true, Layers: []platform.CopilotLayer{{
		Kind: platform.CopilotLayerUser, Path: fcCopilotDir + "/settings.json", Status: platform.MCPSourceRead,
		GlobalAutoApprove: copilotBoolp(true),
	}}}
	v := copilotV2(t, scan, nil, nil, nil)

	for _, row := range []string{rules.RowWritesFiles, rules.RowRunsCommands, rules.RowSendsOut} {
		if c := agentRow(t, v, knowledge.GitHubCopilotProduct, row); c.Decider != rules.DeciderNobody {
			t.Errorf("yolo %s: want nobody, got %+v", row, c)
		}
	}
}

// Copilot cross with Claude Code (CTO-16 Copilot §7 case 5): Claude Code with
// Write allowed in the repo rewrites Copilot's .vscode/settings.json
// (foreign_agent_config_writable, red); Copilot's workspace edits reach Claude
// Code's CLAUDE.md (foreign_agent_instructions_writable). Band red.
func TestScenario_Copilot_CrossClaude(t *testing.T) {
	claude := []platform.ClaudeSettingsFile{
		{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead},
		{Scope: platform.MCPScopeProject, ScopePath: fcRepo, ConfigPath: fcRepo + "/.claude/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead, Permissions: platform.ClaudePermissions{Allow: []string{"Write", "Edit"}}},
	}
	proj := platform.CopilotLayer{Kind: platform.CopilotLayerProject, Path: fcRepo + "/.vscode/settings.json", Root: fcRepo, Status: platform.MCPSourceRead}
	scan := platform.CopilotScan{ConfigDir: fcCopilotDir, Present: true, Layers: []platform.CopilotLayer{
		{Kind: platform.CopilotLayerUser, Path: fcCopilotDir + "/settings.json", Status: platform.MCPSourceRead}, proj,
	}}
	instr := []platform.InstructionFile{{Product: "claude-code", ConfigDir: fcClaudeDir, Path: fcRepo + "/CLAUDE.md"}}

	v := copilotV2(t, scan, claude, instr, nil)

	if v.Status != "red" {
		t.Errorf("cross band: want red, got %q", v.Status)
	}
	if !agentFindings(v, knowledge.GitHubCopilotProduct)[rules.RuleForeignAgentInstructionsWritable] {
		t.Errorf("cross: Copilot should reach CLAUDE.md (foreign_agent_instructions_writable)")
	}
	if !agentFindings(v, knowledge.ClaudeCodeProduct)[rules.RuleForeignAgentConfigWritable] {
		t.Errorf("cross: Claude Code should rewrite Copilot's .vscode/settings.json (foreign_agent_config_writable)")
	}
}
