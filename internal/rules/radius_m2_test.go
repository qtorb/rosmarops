package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-198 M2 (CTO-23 §0): the any_project radius (×1.5 on a credential finding)
// applies whenever the edge's read capability reaches outside the project —
// the whole machine or any_path — whatever the product or the way. These tests
// put one user-level credential (~/.aws/credentials) on the machine and check
// that Codex, Grok Build, Hermes Agent and Cline each record it with radius
// any_project, not same_project.

// userCred is the one user-level credential the M2 tests share: a Supabase
// service_role key (high class) in ~/.aws/credentials, outside any project.
var userCred = []entry{{home + "/.aws/credentials", 1, "SUPABASE_SERVICE_ROLE_KEY", serviceRole}}

// credRadius is the radius of the client's credential finding
// (cred_read_and_send or cred_readable); it fails if the client has neither.
func credRadius(t *testing.T, res rules.Result, client string) string {
	t.Helper()
	for _, f := range res.Findings {
		if len(f.Evidence.Nodes) == 0 || f.Evidence.Nodes[0] != client {
			continue
		}
		if f.Rule == rules.RuleCredReadAndSend || f.Rule == rules.RuleCredReadable {
			return f.Radius
		}
	}
	t.Fatalf("no cred_read_and_send/cred_readable finding for %s", client)
	return ""
}

const hermesDir = home + "/.hermes"

// hermesEvaluate runs the Hermes pipeline (FromScansWithHermes → Infer →
// Evaluate) for one config.yaml and returns the graph, the result and the
// Hermes ai_client id. The config's source registers the client.
func hermesEvaluate(t *testing.T, cfg platform.HermesConfig, creds []entry) (graph.Graph, rules.Result, string) {
	t.Helper()
	var cf platform.CredentialFileScan
	for _, e := range creds {
		cf.Entries = append(cf.Entries, platform.CredentialEntry{Path: e.path, Line: e.line, Key: e.key, Value: secrets.NewSecretValue(e.value)})
	}
	roots := []string{repoWeb}
	settings := platform.ClaudeSettingsScan{
		Files: []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{})},
		Trust: []platform.ProjectTrust{{Path: repoWeb, Accepted: true}},
	}
	source := platform.MCPSource{
		Platform: agentdirs.Hermes, ConfigPath: cfg.Path, ConfigDir: cfg.ConfigDir,
		Status: platform.MCPSourceRead, LoadsMCP: true,
	}
	hermes := platform.HermesScan{Configs: []platform.HermesConfig{cfg}, Sources: []platform.MCPSource{source}}
	mcp := platform.MCPScan{Sources: hermes.Sources, Servers: hermes.Servers}
	f, _, err := graph.FromScansWithHermes(mcp, settings, platform.CursorScan{}, cf, graph.Repos{}, hermes, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	g := graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(salt)}, now)
	g, err = graph.Infer(g, roots, home)
	if err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	res := rules.Evaluate(rules.Input{Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases, RepoRoots: roots, Home: home, Now: now, ClaudeSettings: &settings, Hermes: hermes})
	var client string
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient && n.Platform == knowledge.HermesProduct {
			client = n.ID
		}
	}
	if client == "" {
		t.Fatal("no Hermes Agent ai_client in graph")
	}
	return g, res, client
}

// M2 Codex: the default sandbox (workspace-write) reads any_path, so a user
// credential is reached with radius any_project.
func TestM2_Codex_UserCredAnyProject(t *testing.T) {
	scan := platform.CodexScan{
		ConfigDir: codexDir, Exists: true,
		Layers: []platform.CodexLayer{codexLayer(platform.CodexLayerUser, codexConfig, "")},
	}
	_, res, client := codexEvaluate(t, scan, userCred)
	if r := credRadius(t, res, client); r != rules.RadiusAnyProject {
		t.Errorf("Codex user credential radius = %q, want any_project", r)
	}
}

// M2 Grok Build: ask mode reads any path, so the user credential is readable
// with radius any_project.
func TestM2_Grok_UserCredAnyProject(t *testing.T) {
	scan := platform.GrokScan{ConfigDir: grokDir, Exists: true, Layers: []platform.GrokLayer{grokUserLayer(func(l *platform.GrokLayer) {
		l.PermissionMode, l.HasPermissionMode = "ask", true
	})}}
	_, res, client := grokEvaluate(t, scan, nil, userCred)
	if r := credRadius(t, res, client); r != rules.RadiusAnyProject {
		t.Errorf("Grok user credential radius = %q, want any_project", r)
	}
}

// M2 Hermes Agent: its read tool reaches any path (local backend), so the user
// credential carries radius any_project.
func TestM2_Hermes_UserCredAnyProject(t *testing.T) {
	cfg := platform.HermesConfig{
		Path: hermesDir + "/config.yaml", ConfigDir: hermesDir,
		Status: platform.MCPSourceRead, ApprovalMode: "smart", TerminalBackend: "local",
	}
	_, res, client := hermesEvaluate(t, cfg, userCred)
	if r := credRadius(t, res, client); r != rules.RadiusAnyProject {
		t.Errorf("Hermes user credential radius = %q, want any_project", r)
	}
}

// M2 Cline: it reads the whole machine without asking, so the user credential
// carries radius any_project.
func TestM2_Cline_UserCredAnyProject(t *testing.T) {
	_, res, client := clineEvaluate(t, clBase(), nil, nil, userCred)
	if r := credRadius(t, res, client); r != rules.RadiusAnyProject {
		t.Errorf("Cline user credential radius = %q, want any_project", r)
	}
}
