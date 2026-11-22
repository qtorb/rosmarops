package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// openCodeEvaluate runs one OpenCode-family scan through the graph, the
// inference and the rules, with a credential in the project.
func openCodeEvaluate(t *testing.T, scan platform.OpenCodeScan) (rules.Result, string) {
	t.Helper()
	roots := []string{repoWeb}
	settings := platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{})}, Trust: []platform.ProjectTrust{{Path: repoWeb, Accepted: true}}}
	cf := platform.CredentialFileScan{Entries: []platform.CredentialEntry{{Path: repoWeb + "/.env", Line: 1, Key: "GITHUB_TOKEN", Value: secrets.NewSecretValue("ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789")}}}
	f, _, err := graph.FromScans(platform.MCPScan{Servers: graph.OpenCodeMCPServers(scan)}, settings, cf, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	base, err := knowledge.OpenCode()
	if scan.Product == knowledge.KiloCodeProduct {
		base, err = knowledge.KiloCode()
	}
	if err != nil {
		t.Fatal(err)
	}
	f = graph.Merge(f, graph.FromOpenCode(scan, base, now))
	g := graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(salt)}, now)
	if g, err = graph.Infer(g, roots, home); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.Encode(g); err != nil {
		t.Fatalf("not a valid graph.v2: %v", err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	res := rules.Evaluate(rules.Input{Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases, RepoRoots: roots, Home: home, Now: now, ClaudeSettings: &settings})
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient && n.Platform == scan.Product {
			return res, n.ID
		}
	}
	t.Fatal("no ai_client")
	return res, ""
}

func ocDeciders(t *testing.T, res rules.Result, client string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, a := range res.AgentsOverview {
		if a.AgentID != client {
			continue
		}
		if !a.Evaluated {
			t.Fatalf("not evaluated: %+v", a)
		}
		for _, r := range a.Rows {
			out[r.Row] = r.Decider
		}
	}
	return out
}

// BV-276: OpenCode at the factory reads, edits, runs and sends without
// asking; Kilo Code asks before commands; a file that denies bash blocks it.
// The project's credential is within reach in every case.
func TestOpenCodeOverview(t *testing.T) {
	oc := platform.OpenCodeScan{Product: knowledge.OpenCodeProduct, Present: true, ConfigDir: home + "/.config/opencode"}
	res, client := openCodeEvaluate(t, oc)
	d := ocDeciders(t, res, client)
	for row, want := range map[string]string{
		rules.RowReadsFiles: rules.DeciderNobody, rules.RowWritesFiles: rules.DeciderNobody,
		rules.RowRunsCommands: rules.DeciderNobody, rules.RowSendsOut: rules.DeciderNobody,
	} {
		if d[row] != want {
			t.Errorf("opencode factory %s = %q, want %q", row, d[row], want)
		}
	}
	readable := false
	for _, f := range res.Findings {
		if len(f.Evidence.Nodes) > 0 && f.Evidence.Nodes[0] == client && (f.Rule == rules.RuleCredReadAndSend || f.Rule == rules.RuleCredReadable) {
			readable = true
		}
	}
	if !readable {
		t.Error("the project's credential is not within OpenCode's reach")
	}

	kilo := platform.OpenCodeScan{Product: knowledge.KiloCodeProduct, Present: true, ConfigDir: home + "/.config/kilo"}
	res, client = openCodeEvaluate(t, kilo)
	if d := ocDeciders(t, res, client); d[rules.RowRunsCommands] != rules.DeciderYou || d[rules.RowReadsFiles] != rules.DeciderNobody {
		t.Errorf("kilo factory %v", d)
	}

	oc.Layers = []platform.OpenCodeLayer{{
		Kind: platform.OpenCodeLayerUser, Path: home + "/.config/opencode/opencode.json", Status: platform.MCPSourceRead,
		Permission: map[string]platform.OpenCodeRule{"bash": {Action: "deny"}, "webfetch": {Action: "ask"}, "websearch": {Action: "ask"}},
	}}
	res, client = openCodeEvaluate(t, oc)
	if d := ocDeciders(t, res, client); d[rules.RowRunsCommands] != rules.DeciderBlocked || d[rules.RowSendsOut] != rules.DeciderYou {
		t.Errorf("configured %v", d)
	}
}
