package windows_test

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/graph"
)

// K1, item 1: ListSkills is internal/skills on this machine's home, and its
// agents are the ai_clients of the MCP scan: the skills in the repository's
// .claude\skills reach Claude Code's, Cursor's and Cline's clients, and the
// .agents\skills one only Cursor's and Cline's (Claude Code does not load it).
func TestListSkills_SameClientsAsMCPScan(t *testing.T) {
	e := newIdentityEnv(t)
	e.writeAll(t, true)
	writeFile(t, filepath.Join(e.repoA, ".claude", "skills", "fmt", "SKILL.md"), "x")
	writeFile(t, filepath.Join(e.repoA, ".agents", "skills", "deploy", "SKILL.md"), "x")
	writeFile(t, filepath.Join(e.repoA, ".agents", "skills", "deploy", "scripts", "deploy.ps1"), "x")

	w := windows.New()
	ctx := context.Background()
	scan, err := w.ListMCPServers(ctx, []string{e.repoA, e.repoB})
	if err != nil {
		t.Fatal(err)
	}
	sk, err := w.ListSkills(ctx, []string{e.repoA, e.repoB})
	if err != nil {
		t.Fatal(err)
	}
	f, err := graph.FromMCPScan(scan, testSalt, testTime)
	if err != nil {
		t.Fatal(err)
	}
	f = graph.AddSkills(f, sk, testTime)
	g := graph.Build(f, graph.Provenance{EngineVersion: "x", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(testSalt)}, testTime)
	if _, err := graph.Encode(g); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	agents := map[string]string{}
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent {
			agents[n.ID] = n.Platform
		}
	}
	var got []string
	for _, n := range g.Nodes {
		if n.Type == graph.ResourceAgentInstructions || n.Type == graph.ResourceAgentExecutable {
			rel, _ := filepath.Rel(e.repoA, n.Path)
			got = append(got, n.Type+" "+filepath.ToSlash(rel)+" "+agents[n.Configures])
		}
	}
	sort.Strings(got)
	want := []string{
		"agent_executable .agents/skills/deploy/scripts/deploy.ps1 cline",
		"agent_executable .agents/skills/deploy/scripts/deploy.ps1 cursor",
		"agent_instructions .agents/skills/deploy/SKILL.md cline",
		"agent_instructions .agents/skills/deploy/SKILL.md cursor",
		"agent_instructions .claude/skills/fmt/SKILL.md claude-code",
		"agent_instructions .claude/skills/fmt/SKILL.md cline",
		"agent_instructions .claude/skills/fmt/SKILL.md cursor",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("skill resources:\n got  %q\n want %q", got, want)
	}
}
