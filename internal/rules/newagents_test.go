package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-88 (decision 2026-10-27, agentes nuevos, N1): Codex detected, not
// evaluated, loads the repository's AGENTS.md; Claude Code, allowed to
// write in the repository, gets a writes_config_of edge to Codex's
// resource for that file and the finding
// foreign_agent_instructions_writable naming Codex.
func TestClaudeCodeWritesCodexInstructions(t *testing.T) {
	codexDir := home + "/.codex"
	mcp := platform.MCPScan{Present: []platform.AgentPresence{{Platform: "codex", ConfigDir: codexDir, Marker: codexDir + "/config.toml"}}}
	settings := platform.ClaudeSettingsScan{
		Files: []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{DefaultMode: "default", Allow: []string{"Bash"}})},
		Trust: []platform.ProjectTrust{{Path: repoWeb, Accepted: true}},
	}
	f, _, err := graph.FromScans(mcp, settings, platform.CredentialFileScan{}, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	agents := repoWeb + "/AGENTS.md"
	f = graph.AddInstructions(f, platform.InstructionScan{Files: []platform.InstructionFile{
		{Product: "claude-code", ConfigDir: claudeDir, Path: agents, Shared: true},
		{Product: "codex", ConfigDir: codexDir, Path: agents, Shared: true},
	}}, now)
	g, err := graph.Infer(graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(salt)}, now), []string{repoWeb}, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.Encode(g); err != nil {
		t.Fatal(err)
	}

	nodes := map[string]graph.Node{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	var codexRes string
	for _, n := range g.Nodes {
		if n.Type == graph.ResourceAgentInstructions && n.Path == agents && nodes[n.Configures].Platform == "codex" {
			codexRes = n.ID
		}
	}
	if codexRes == "" {
		t.Fatal("no Codex resource for AGENTS.md")
	}
	var edge bool
	for _, e := range g.Edges {
		edge = edge || e.Type == graph.EdgeWritesConfigOf && e.To == codexRes && nodes[e.From].Platform == "claude-code"
	}
	if !edge {
		t.Fatal("no writes_config_of from Claude Code to Codex's AGENTS.md")
	}

	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	res := rules.Evaluate(rules.Input{
		Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases, RepoRoots: []string{repoWeb},
		Home: home, Now: now, ClaudeSettings: &settings,
	})
	var found bool
	for _, fd := range res.Findings {
		if fd.Rule == rules.RuleForeignAgentInstructionsWritable && fd.Params.Agent == "Claude Code" && fd.Params.TargetAgent == "Codex" {
			found = true
		}
	}
	if !found {
		var got []string
		for _, fd := range res.Findings {
			got = append(got, fd.Rule+" "+fd.Params.TargetAgent)
		}
		t.Errorf("no foreign_agent_instructions_writable for Codex: %v", got)
	}
}
