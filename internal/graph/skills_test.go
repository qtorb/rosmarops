package graph_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

// K1, item 2: a SKILL.md in .agents\skills that Claude Code's and Cursor's
// clients both get (the scan lists it per agent) is one agent_instructions
// resource per agent, and a script one agent_executable per agent; an agent
// with no ai_client (Codex here) gets nothing, and only the failures of the
// agents present reach coverage. The executable of a skill and the command
// of a stdio MCP server at the same path, for the same agent, are one node.
func TestAddSkills_PerAgentResources(t *testing.T) {
	const (
		skillMD  = "/src/app/.agents/skills/deploy/SKILL.md"
		script   = "/src/app/.agents/skills/deploy/scripts/deploy.sh"
		codexDir = "/home/albert/.codex"
	)
	scan := inferScan()
	scan.Servers = append(scan.Servers, platform.MCPServer{
		Client: "cursor", Name: "deploy", Command: script, Args: []string{}, LoadsMCP: true,
		ConfigPath: cursorMCPJSON, ConfigDir: cursorDir, Scope: platform.MCPScopeUser,
	})
	f, _, err := graph.FromScan(scan, platform.ClaudeSettingsScan{}, testSalt, scanTime)
	if err != nil {
		t.Fatal(err)
	}
	both := func(path string) []platform.InstructionFile {
		return []platform.InstructionFile{
			{Product: "claude-code", ConfigDir: claudeDir, Path: path, Shared: true},
			{Product: "cursor", ConfigDir: cursorDir, Path: path, Shared: true},
			{Product: "codex", ConfigDir: codexDir, Path: path, Shared: true},
		}
	}
	f = graph.AddSkills(f, platform.SkillScan{
		Instructions: both(skillMD),
		Executables:  both(script),
		Failed: []platform.InstructionFailure{
			{Product: "cursor", ConfigDir: cursorDir, Path: "/src/app/.cursor/skills/linked", Reason: graph.Reason(graph.ReasonSourceUnsupported, "symbolic link not followed")},
			{Product: "codex", ConfigDir: codexDir, Path: "/home/albert/.agents/skills/x", Reason: graph.Reason(graph.ReasonReadError, "denied")},
		},
	}, scanTime)
	g := graph.Build(f, testProvenance, scanTime)
	if _, err := graph.Encode(g); err != nil {
		t.Fatal(err)
	}

	nodes := map[string]graph.Node{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	got := map[string][]string{} // type+path → platforms of the agents configured
	for _, n := range g.Nodes {
		if n.Path == skillMD || n.Path == script {
			got[n.Type+" "+n.Path] = append(got[n.Type+" "+n.Path], nodes[n.Configures].Platform)
		}
	}
	for key, want := range map[string]string{
		graph.ResourceAgentInstructions + " " + skillMD: "claude-code cursor",
		graph.ResourceAgentExecutable + " " + script:    "claude-code cursor",
	} {
		ps := got[key]
		if len(ps) == 2 && ps[0] > ps[1] {
			ps[0], ps[1] = ps[1], ps[0]
		}
		if strings.Join(ps, " ") != want {
			t.Errorf("%s configures %q, want %q", key, ps, want)
		}
	}

	var failed []string
	for _, sf := range g.Coverage.SourcesFailed {
		failed = append(failed, sf.Path)
	}
	if strings.Join(failed, ",") != "/src/app/.cursor/skills/linked" {
		t.Errorf("sources_failed = %q, want only Cursor's", failed)
	}
}
