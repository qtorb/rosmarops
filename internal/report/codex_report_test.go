package report_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-140 (CTO-16 §7): the example report of a machine where OpenAI Codex runs
// with the approve-everything preset (sandbox_mode = danger-full-access,
// approval_policy = never) and a trusted project, and xAI Grok Build runs with
// yolo and a key in its config.toml. It renders in both languages with Codex
// and Grok Build evaluated, their findings and fixes shown, and no secret in
// clear.
func TestRenderHTML_CodexAndGrokEvaluated(t *testing.T) {
	salt := []byte("0123456789abcdef0123456789abcdef")
	codexDir, repo := "/h/.codex", "/src/a"
	net := false
	user := platform.CodexLayer{
		Kind: platform.CodexLayerUser, Path: codexDir + "/config.toml", Status: platform.MCPSourceRead,
		SandboxMode: "danger-full-access", ApprovalPolicy: "never", HasApprovalPolicy: true, NetworkAccess: &net,
		MCP: []platform.CodexMCPServer{{
			ID: "github", Command: "npx", EnvKeys: []string{"GITHUB_PERSONAL_ACCESS_TOKEN"},
			Env: []platform.EnvVar{{Key: "GITHUB_PERSONAL_ACCESS_TOKEN", Value: secrets.NewSecretValue("ghp_FAKEFAKEFAKEFAKE1234")}},
		}},
	}
	proj := platform.CodexLayer{Kind: platform.CodexLayerProject, Path: repo + "/.codex/config.toml", Root: repo, Status: platform.MCPSourceRead, Trusted: true}
	scan := platform.CodexScan{ConfigDir: codexDir, Exists: true, Layers: []platform.CodexLayer{user, proj}}

	settings := platform.ClaudeSettingsScan{
		Files: []platform.ClaudeSettingsFile{
			{Scope: platform.MCPScopeUser, ConfigPath: "/h/.claude/settings.json", ConfigDir: "/h/.claude", Status: platform.MCPSourceRead},
			{Scope: platform.MCPScopeProject, ScopePath: repo, ConfigPath: repo + "/.claude/settings.json", ConfigDir: "/h/.claude", Status: platform.MCPSourceRead},
		},
		Trust: []platform.ProjectTrust{{Path: repo, Accepted: true}},
	}
	cf := platform.CredentialFileScan{Entries: []platform.CredentialEntry{
		{Path: "/h/.claude/.credentials.json", Line: 1, Key: "SUPABASE_SECRET_KEY", Value: secrets.NewSecretValue("sb_secret_" + strings.Repeat("S", 22) + "_" + strings.Repeat("T", 8))},
		{Path: codexDir + "/auth.json", Line: 1, Key: "OPENAI_API_KEY", Value: secrets.NewSecretValue("sk-proj-" + strings.Repeat("O", 48))},
	}}
	grokDir := "/h/.grok"
	grok := platform.GrokScan{ConfigDir: grokDir, Exists: true, Layers: []platform.GrokLayer{{
		Kind: platform.GrokLayerUser, Path: grokDir + "/config.toml", Status: platform.MCPSourceRead, Yolo: true,
		APIKeys: []platform.EnvVar{{Key: "mine", Value: secrets.NewSecretValue("xai-FAKEapikey0000WXYZ")}},
		MCP: []platform.GrokMCPServer{{
			ID: "ghgrok", Command: "npx", EnvKeys: []string{"GITHUB_PERSONAL_ACCESS_TOKEN"},
			Env: []platform.EnvVar{{Key: "GITHUB_PERSONAL_ACCESS_TOKEN", Value: secrets.NewSecretValue("ghp_GROKFAKEFAKEFAKE5678")}},
		}},
	}}}
	mcp := platform.MCPScan{Servers: append(graph.CodexMCPServers(scan), graph.GrokMCPServers(grok)...)}
	f, _, err := graph.FromScansWithGrok(mcp, settings, platform.CursorScan{}, cf, graph.Repos{}, platform.HermesScan{}, scan, grok, salt, v2Now)
	if err != nil {
		t.Fatal(err)
	}
	f = graph.AddInstructions(f, platform.InstructionScan{Files: []platform.InstructionFile{
		{Product: "claude-code", ConfigDir: "/h/.claude", Path: repo + "/CLAUDE.md"},
		{Product: "codex", ConfigDir: codexDir, Path: repo + "/AGENTS.md"},
	}}, v2Now)
	g, err := graph.Infer(graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(salt)}, v2Now), []string{repo}, "/h")
	if err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	v1 := report.Build("t", nil, nil, 0, 0, 0, 0, nil)
	v1.GeneratedAt = "2026-11-06T12:00:00Z"
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("ab", 32), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: []string{repo}, Families: [][]string{{repo}}, V1: v1, GeneratedAt: v2Now,
		ClaudeSettings: &settings, Home: "/h", Codex: scan, Grok: grok,
	})

	for _, lang := range []string{"en", "es"} {
		html := renderHTML(t, lang, v)
		for _, agent := range []string{"Codex", "Grok Build"} {
			if !strings.Contains(html, agent) {
				t.Errorf("%s: the report does not mention %s", lang, agent)
			}
		}
		for _, leak := range []string{"FAKEFAKE", "FAKEapikey", strings.Repeat("S", 22), strings.Repeat("O", 48)} {
			if strings.Contains(html, leak) {
				t.Errorf("%s: a secret leaked into the report (%q)", lang, leak)
			}
		}
	}
}
