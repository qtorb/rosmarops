package trust_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-193 (CTO-22 Gemini §7): end-to-end bands for Google Gemini CLI through the
// full report pipeline (FromScans → FromGeminiCLI → Infer → BuildV2).

// geminiV2 runs the Gemini CLI pipeline end to end.
func geminiV2(t *testing.T, scan platform.GeminiScan, claude []platform.ClaudeSettingsFile, entries []platform.CredentialEntry) report.V2 {
	t.Helper()
	roots := []string{fcRepo}
	if claude == nil {
		claude = []platform.ClaudeSettingsFile{{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead}}
	}
	settings := platform.ClaudeSettingsScan{Files: claude, Trust: []platform.ProjectTrust{{Path: fcRepo, Accepted: true}}}
	frag, _, err := graph.FromScans(platform.MCPScan{Servers: graph.GeminiMCPServers(scan)}, settings, platform.CredentialFileScan{Entries: entries}, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.Merge(frag, graph.FromGeminiCLI(scan, fcNow))
	g := graph.Build(frag, graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(fcSalt)}, fcNow)
	graph.MarkGeminiUntrusted(&g, scan, roots)
	g, err = graph.Infer(g, roots, fcHome)
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

func geminiUserLayer(mode string) platform.GeminiLayer {
	return platform.GeminiLayer{Kind: platform.GeminiLayerUser, Path: fcGemini + "/settings.json", Status: platform.MCPSourceRead, DefaultApprovalMode: mode}
}

// G1: default settings, a high-privilege credential in the workspace. The band
// is red and, with google_web_search as egress (BV-198 M3), cred_read_and_send
// fires on Gemini CLI.
func TestScenario_Gemini_Default(t *testing.T) {
	scan := platform.GeminiScan{ConfigDir: fcGemini, Present: true, FolderTrustEnabled: true, Layers: []platform.GeminiLayer{geminiUserLayer("")}}
	entries := []platform.CredentialEntry{{Path: fcRepo + "/.env", Line: 1, Key: "SUPABASE_SERVICE_ROLE_KEY", Value: secrets.NewSecretValue(fcServiceRole)}}
	v := geminiV2(t, scan, nil, entries)

	if v.Status != "red" {
		t.Errorf("default band: want red, got %q", v.Status)
	}
	if !agentFindings(v, knowledge.GeminiCLIProduct)[rules.RuleCredReadAndSend] {
		t.Errorf("default: missing cred_read_and_send (have %v)", agentFindings(v, knowledge.GeminiCLIProduct))
	}
}

// G2: auto_edit in a trusted repo that holds another agent's config. The band is
// red and foreign_agent_config_writable fires; writes decide nobody.
func TestScenario_Gemini_AutoEdit(t *testing.T) {
	scan := platform.GeminiScan{
		ConfigDir: fcGemini, Present: true, FolderTrustEnabled: true,
		TrustedFolders: map[string]string{fcRepo: "TRUST_FOLDER"},
		Layers:         []platform.GeminiLayer{geminiUserLayer("auto_edit")},
	}
	claude := []platform.ClaudeSettingsFile{
		{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead},
		{Scope: platform.MCPScopeProject, ConfigPath: fcRepo + "/.claude/settings.json", ConfigDir: fcClaudeDir, ScopePath: fcRepo, Status: platform.MCPSourceRead},
	}
	v := geminiV2(t, scan, claude, nil)

	if v.Status != "red" {
		t.Errorf("auto_edit band: want red, got %q", v.Status)
	}
	if !agentFindings(v, knowledge.GeminiCLIProduct)[rules.RuleForeignAgentConfigWritable] {
		t.Errorf("auto_edit: missing foreign_agent_config_writable")
	}
	if c := agentRow(t, v, knowledge.GeminiCLIProduct, rules.RowWritesFiles); c.Decider != rules.DeciderNobody {
		t.Errorf("auto_edit writes: want nobody, got %+v", c)
	}
}
