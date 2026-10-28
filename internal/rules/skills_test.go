package rules_test

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	before := skillScenario(t, nil)
	after := skillScenario(t, func(t *testing.T, repo string) {
		skill := filepath.Join(repo, ".agents", "skills", "deploy")
		touchFile(t, filepath.Join(skill, "SKILL.md"))
		touchFile(t, filepath.Join(skill, "scripts", "deploy.sh"))
	})
	equalLists(t, "resources", after.resources, []string{
		"agent_executable .agents/skills/deploy/scripts/deploy.sh cursor",
		"agent_instructions .agents/skills/deploy/SKILL.md cursor",
	})
	equalLists(t, "new findings", added(before.findings, after.findings), []string{
		"foreign_agent_config_writable red Claude Code -> Cursor agent_executable",
		"foreign_agent_instructions_writable yellow Claude Code -> Cursor agent_instructions",
		"own_config_writable red Cursor -> Cursor agent_executable",
		"own_instructions_writable yellow Cursor -> Cursor agent_instructions",
	})
}

// BV-99b, item 3: the typical `npx skills add` — the canonical copy in
// <repo>\.agents\skills\demo with scripts\run.ps1, linked from
// <repo>\.claude\skills\demo. Claude Code and Cursor both get the skill,
// one shared node per agent and canonical path, and nothing goes to
// coverage. Claude Code now loads its own copy too: on top of the four
// findings of a skill only Cursor loads, Claude Code can write its own
// skill (own_*) and Cursor can write Claude Code's (foreign_*).
func TestNpxSkillsAdd_ClaudeCodeAndCursor(t *testing.T) {
	before := skillScenario(t, nil)
	after := skillScenario(t, func(t *testing.T, repo string) {
		canon := filepath.Join(repo, ".agents", "skills", "demo")
		touchFile(t, filepath.Join(canon, "SKILL.md"))
		touchFile(t, filepath.Join(canon, "scripts", "run.ps1"))
		link := filepath.Join(repo, ".claude", "skills", "demo")
		if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(canon, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	})
	equalLists(t, "resources", after.resources, []string{
		"agent_executable .agents/skills/demo/scripts/run.ps1 claude-code",
		"agent_executable .agents/skills/demo/scripts/run.ps1 cursor",
		"agent_instructions .agents/skills/demo/SKILL.md claude-code",
		"agent_instructions .agents/skills/demo/SKILL.md cursor",
	})
	if after.failed != 0 {
		t.Errorf("sources_failed = %d, want 0", after.failed)
	}
	equalLists(t, "new findings", added(before.findings, after.findings), []string{
		"foreign_agent_config_writable red Claude Code -> Cursor agent_executable",
		"foreign_agent_config_writable red Cursor -> Claude Code agent_executable",
		"foreign_agent_instructions_writable yellow Claude Code -> Cursor agent_instructions",
		"foreign_agent_instructions_writable yellow Cursor -> Claude Code agent_instructions",
		"own_config_writable red Claude Code -> Claude Code agent_executable",
		"own_config_writable red Cursor -> Cursor agent_executable",
		"own_instructions_writable yellow Claude Code -> Claude Code agent_instructions",
		"own_instructions_writable yellow Cursor -> Cursor agent_instructions",
	})
}

func touchFile(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func added(before, after []string) []string {
	was := map[string]bool{}
	for _, f := range before {
		was[f] = true
	}
	var out []string
	for _, f := range after {
		if !was[f] {
			out = append(out, f)
		}
	}
	return out
}

func equalLists(t *testing.T, what string, got, want []string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n got  %q\n want %q", what, got, want)
	}
}

// skillOutcome is a scenario's skill resources ("type path platform", path
// relative to the repository), its count of sources_failed and its findings
// ("rule severity agent -> target resource_type").
type skillOutcome struct {
	resources, findings []string
	failed              int
}

// skillScenario evaluates the machine after setup (nil: no skill).
func skillScenario(t *testing.T, setup func(t *testing.T, repo string)) skillOutcome {
	t.Helper()
	tmp := t.TempDir()
	h, repo := filepath.Join(tmp, "home"), filepath.Join(tmp, "repo")
	cDir, cuDir := filepath.Join(h, ".claude"), filepath.Join(h, ".cursor")
	if setup != nil {
		setup(t, repo)
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
	var out skillOutcome
	for _, n := range g.Nodes {
		if n.Kind == graph.KindResource && (n.Type == graph.ResourceAgentInstructions || n.Type == graph.ResourceAgentExecutable) {
			rel, _ := filepath.Rel(repo, n.Path)
			out.resources = append(out.resources, n.Type+" "+filepath.ToSlash(rel)+" "+nodes[n.Configures].Platform)
		}
	}
	sort.Strings(out.resources)
	out.failed = len(g.Coverage.SourcesFailed)

	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	result := rules.Evaluate(rules.Input{
		Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases, RepoRoots: []string{repo},
		Home: h, Now: now, ClaudeSettings: &settings,
	})
	for _, fd := range result.Findings {
		target := fd.Params.TargetAgent
		if target == "" {
			target = fd.Params.Agent
		}
		out.findings = append(out.findings, fd.Rule+" "+fd.Severity+" "+fd.Params.Agent+" -> "+target+" "+fd.Params.ResourceType)
	}
	sort.Strings(out.findings)
	return out
}
