package rules_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/skills"
)

// BV-99 (decision 2026-10-28, K1): Claude Code (the owner's settings: no
// defaultMode, the built-in auto) and Cursor, and one skill in the
// repository's .agents\skills with a script. Claude Code does not load
// .agents\skills; Cursor does: SKILL.md is Cursor's agent_instructions and
// the script Cursor's agent_executable. The findings come from the rules
// that already exist, with no new text: the four new ones are Claude Code
// writing Cursor's skill (instructions, executable) and Cursor writing its
// own.
func TestSkillInAgentsFolder_ClaudeCodeAndCursor(t *testing.T) {
	before, after := skillScenario(t, false), skillScenario(t, true)
	was := map[string]bool{}
	for _, f := range before {
		was[f] = true
	}
	var added []string
	for _, f := range after {
		if !was[f] {
			added = append(added, f)
		}
	}
	want := []string{
		"foreign_agent_config_writable red Claude Code -> Cursor agent_executable",
		"foreign_agent_instructions_writable yellow Claude Code -> Cursor agent_instructions",
		"own_config_writable red Cursor -> Cursor agent_executable",
		"own_instructions_writable yellow Cursor -> Cursor agent_instructions",
	}
	if len(added) != len(want) || len(after) != len(before)+len(want) {
		t.Fatalf("new findings = %q, want %q (before %q)", added, want, before)
	}
	for i := range want {
		if added[i] != want[i] {
			t.Errorf("new finding %d = %q, want %q", i, added[i], want[i])
		}
	}
}

// skillScenario evaluates the machine, with or without the skill, and
// returns its findings as "rule severity agent -> target resource_type".
func skillScenario(t *testing.T, withSkill bool) []string {
	t.Helper()
	tmp := t.TempDir()
	h, repo := filepath.Join(tmp, "home"), filepath.Join(tmp, "repo")
	cDir, cuDir := filepath.Join(h, ".claude"), filepath.Join(h, ".cursor")
	skill := filepath.Join(repo, ".agents", "skills", "deploy")
	var files []string
	if withSkill {
		files = []string{filepath.Join(skill, "SKILL.md"), filepath.Join(skill, "scripts", "deploy.sh")}
	}
	for _, p := range files {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	env := func(string) (string, bool) { return "", false }
	scan := skills.Discover(context.Background(), h, env, []string{repo})

	mcp := platform.MCPScan{Sources: []platform.MCPSource{
		{Platform: "cursor", ConfigPath: filepath.Join(cuDir, "mcp.json"), ConfigDir: cuDir, Status: platform.MCPSourceRead, LoadsMCP: true},
	}}
	settings := platform.ClaudeSettingsScan{
		Files: []platform.ClaudeSettingsFile{{
			Scope: platform.MCPScopeUser, ConfigPath: filepath.Join(cDir, "settings.json"), ConfigDir: cDir, Status: platform.MCPSourceRead,
		}},
		Trust: []platform.ProjectTrust{{Path: repo, Accepted: true}},
	}
	f, _, err := graph.FromScansWithCursor(mcp, settings, platform.CursorScan{ConfigDir: cuDir, Exists: true}, platform.CredentialFileScan{}, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	f = graph.AddSkills(f, scan, now)
	g, err := graph.Infer(graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Version, HostID: graph.HostID(salt)}, now), []string{repo}, h)
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
	var res []string
	for _, n := range g.Nodes {
		if n.Kind == graph.KindResource && (n.Type == graph.ResourceAgentInstructions || n.Type == graph.ResourceAgentExecutable) {
			rel, _ := filepath.Rel(repo, n.Path)
			res = append(res, n.Type+" "+filepath.ToSlash(rel)+" "+nodes[n.Configures].Platform)
		}
	}
	sort.Strings(res)
	wantRes := []string{
		"agent_executable .agents/skills/deploy/scripts/deploy.sh cursor",
		"agent_instructions .agents/skills/deploy/SKILL.md cursor",
	}
	if !withSkill {
		wantRes = nil
	}
	if len(res) != len(wantRes) || (withSkill && (res[0] != wantRes[0] || res[1] != wantRes[1])) {
		t.Fatalf("resources = %q, want %q", res, wantRes)
	}

	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	result := rules.Evaluate(rules.Input{
		Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases, RepoRoots: []string{repo},
		Home: h, Now: now, ClaudeSettings: &settings,
	})
	var got []string
	for _, fd := range result.Findings {
		target := fd.Params.TargetAgent
		if target == "" {
			target = fd.Params.Agent
		}
		got = append(got, fd.Rule+" "+fd.Severity+" "+fd.Params.Agent+" -> "+target+" "+fd.Params.ResourceType)
	}
	sort.Strings(got)
	return got
}
