package windows_test

// The agents detected and not evaluated (decision 2026-10-27, agentes nuevos,
// N1; BV-88): Codex, Antigravity, Hermes Agent and Claude Desktop.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

type newAgentsEnv struct {
	home, appData, localAppData, repo string
}

func newNewAgentsEnv(t *testing.T) newAgentsEnv {
	t.Helper()
	root := t.TempDir()
	e := newAgentsEnv{
		home: filepath.Join(root, "home"), appData: filepath.Join(root, "roaming"),
		localAppData: filepath.Join(root, "local"), repo: filepath.Join(root, "src", "repo"),
	}
	for _, d := range []string{e.home, e.appData, e.localAppData, e.repo} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", e.home)
	t.Setenv("USERPROFILE", e.home)
	t.Setenv("APPDATA", e.appData)
	t.Setenv("LOCALAPPDATA", e.localAppData)
	t.Setenv("CODEX_HOME", "")
	t.Setenv("HERMES_HOME", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	return e
}

// Claude Desktop's and Antigravity's MCP servers are read as the other
// clients': env as credentials; an Antigravity remote server (serverUrl)
// has its headers read and no command. The four agents are present.
func TestListMCPServers_NewAgents(t *testing.T) {
	e := newNewAgentsEnv(t)
	desktop := filepath.Join(e.appData, "Claude", "claude_desktop_config.json")
	global := filepath.Join(e.home, ".gemini", "config", "mcp_config.json")
	project := filepath.Join(e.repo, ".agents", "mcp_config.json")
	writeFile(t, desktop, `{"mcpServers": {"fs": {"command": "npx", "env": {"TOKEN": "a"}}}}`)
	writeFile(t, global, `{"mcpServers": {
	  "local":  {"command": "npx", "env": {"API_KEY": "b"}},
	  "remote": {"serverUrl": "https://mcp.example.com/mcp", "headers": {"Authorization": "Bearer c"}}}}`)
	writeFile(t, project, `{"mcpServers": {"proj": {"command": "./bin/srv", "headers": {"X": "ignored, not remote"}}}}`)
	writeFile(t, filepath.Join(e.home, ".codex", "config.toml"), "model = \"x\"\n")
	writeFile(t, filepath.Join(e.localAppData, "hermes", "config.yaml"), "model: x\n")

	scan, err := windows.New().ListMCPServers(context.Background(), []string{e.repo})
	if err != nil {
		t.Fatal(err)
	}
	type got struct {
		client, scope, scopePath, configDir string
		env, headers                        []string
		command                             string
	}
	servers := map[string]got{}
	for _, s := range scan.Servers {
		var env, headers []string
		for _, v := range s.Env {
			env = append(env, v.Key)
		}
		for _, v := range s.Headers {
			headers = append(headers, v.Key)
		}
		servers[s.Name] = got{s.Client, s.Scope, s.ScopePath, s.ConfigDir, env, headers, s.Command}
	}
	gemini := filepath.Join(e.home, ".gemini")
	for name, w := range map[string]got{
		"fs":     {"claude-desktop", platform.MCPScopeUser, "", filepath.Join(e.appData, "Claude"), []string{"TOKEN"}, nil, "npx"},
		"local":  {"antigravity", platform.MCPScopeUser, "", gemini, []string{"API_KEY"}, nil, "npx"},
		"remote": {"antigravity", platform.MCPScopeUser, "", gemini, nil, []string{"Authorization"}, ""},
		"proj":   {"antigravity", platform.MCPScopeProject, e.repo, gemini, nil, nil, "./bin/srv"},
	} {
		g := servers[name]
		if g.client != w.client || g.scope != w.scope || g.scopePath != w.scopePath || g.configDir != w.configDir ||
			strings.Join(g.env, ",") != strings.Join(w.env, ",") || strings.Join(g.headers, ",") != strings.Join(w.headers, ",") || g.command != w.command {
			t.Errorf("%s: got %+v, want %+v", name, g, w)
		}
	}
	present := map[string]string{}
	for _, p := range scan.Present {
		present[p.Platform] = p.ConfigDir
	}
	want := map[string]string{
		"codex": filepath.Join(e.home, ".codex"), "antigravity": gemini,
		"hermes": filepath.Join(e.localAppData, "hermes"), "claude-desktop": filepath.Join(e.appData, "Claude"),
	}
	if len(present) != len(want) {
		t.Fatalf("present %v, want %v", present, want)
	}
	for p, dir := range want {
		if present[p] != dir {
			t.Errorf("%s: %q, want %q", p, present[p], dir)
		}
	}
}

// Without Antigravity, a repository's .agents/mcp_config.json is no source
// (nobody loads it), and no new agent is present.
func TestListMCPServers_NewAgentsAbsent(t *testing.T) {
	e := newNewAgentsEnv(t)
	writeFile(t, filepath.Join(e.repo, ".agents", "mcp_config.json"), `{"mcpServers": {"proj": {"command": "npx"}}}`)
	writeFile(t, filepath.Join(e.home, ".gemini", "settings.json"), `{}`) // Gemini CLI
	scan, err := windows.New().ListMCPServers(context.Background(), []string{e.repo})
	if err != nil {
		t.Fatal(err)
	}
	if len(scan.Present) != 0 || len(scan.Servers) != 0 {
		t.Errorf("present %v, servers %v", scan.Present, scan.Servers)
	}
	for _, s := range scan.Sources {
		if s.Platform == "antigravity" || s.Platform == "claude-desktop" {
			t.Errorf("source %+v", s)
		}
	}
}

type listed struct {
	product string
	shared  bool
}

func listedFiles(t *testing.T, repoRoots []string) map[string][]listed {
	t.Helper()
	scan, err := windows.New().ListAgentInstructions(context.Background(), repoRoots)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string][]listed{}
	for _, f := range scan.Files {
		out[f.Path] = append(out[f.Path], listed{f.Product, f.Shared})
	}
	for _, l := range out {
		sort.Slice(l, func(i, j int) bool { return l[i].product < l[j].product })
	}
	return out
}

// Codex: the global AGENTS.override.md wins over AGENTS.md; the repository's
// AGENTS.md is shared. Antigravity: GEMINI.md, AGENTS.md (shared) and
// .agents/rules. Hermes: the first of .hermes.md, HERMES.md, AGENTS.md,
// CLAUDE.md and .cursorrules.
func TestListAgentInstructions_NewAgents(t *testing.T) {
	e := newNewAgentsEnv(t)
	other := filepath.Join(filepath.Dir(e.repo), "other")
	codexDir := filepath.Join(e.home, ".codex")
	writeFile(t, filepath.Join(codexDir, "config.toml"), "")
	writeFile(t, filepath.Join(codexDir, "AGENTS.md"), "x")
	writeFile(t, filepath.Join(codexDir, "AGENTS.override.md"), "x")
	if err := os.MkdirAll(filepath.Join(e.home, ".gemini", "antigravity"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(e.home, ".gemini", "GEMINI.md"), "x")
	writeFile(t, filepath.Join(e.localAppData, "hermes", "config.yaml"), "")
	for _, p := range []string{"AGENTS.md", "GEMINI.md", "HERMES.md", "CLAUDE.md", filepath.Join(".agents", "rules", "a.md"), filepath.Join(".agents", "rules", "sub", "b.txt")} {
		writeFile(t, filepath.Join(e.repo, p), "x")
	}
	writeFile(t, filepath.Join(other, "CLAUDE.md"), "x")
	writeFile(t, filepath.Join(other, ".cursorrules"), "x")

	got := listedFiles(t, []string{e.repo, other})
	want := map[string][]listed{
		filepath.Join(codexDir, "AGENTS.override.md"):             {{"codex", false}},
		filepath.Join(e.home, ".gemini", "GEMINI.md"):             {{"antigravity", false}},
		filepath.Join(e.repo, "AGENTS.md"):                        {{"antigravity", true}, {"codex", true}, {"cursor", true}},
		filepath.Join(e.repo, "GEMINI.md"):                        {{"antigravity", false}},
		filepath.Join(e.repo, "HERMES.md"):                        {{"hermes", false}},
		filepath.Join(e.repo, "CLAUDE.md"):                        {{"claude-code", false}},
		filepath.Join(e.repo, ".agents", "rules", "a.md"):         {{"antigravity", false}},
		filepath.Join(e.repo, ".agents", "rules", "sub", "b.txt"): {{"antigravity", false}},
		filepath.Join(other, "CLAUDE.md"):                         {{"claude-code", false}, {"hermes", true}},
		filepath.Join(other, ".cursorrules"):                      {{"cursor", false}},
	}
	if len(got) != len(want) {
		t.Errorf("listed %v\nwant %v", got, want)
	}
	for p, w := range want {
		g := got[p]
		if len(g) != len(w) {
			t.Errorf("%s: %v, want %v", p, g, w)
			continue
		}
		for i := range w {
			if g[i] != w[i] {
				t.Errorf("%s: %v, want %v", p, g, w)
			}
		}
	}
}

// Hermes reads .hermes.md before everything else; without Hermes, Codex
// and Antigravity nothing of theirs is listed.
func TestListAgentInstructions_NewAgentsOrderAndAbsence(t *testing.T) {
	e := newNewAgentsEnv(t)
	writeFile(t, filepath.Join(e.localAppData, "hermes", "config.yaml"), "")
	for _, p := range []string{".hermes.md", "HERMES.md", "AGENTS.md"} {
		writeFile(t, filepath.Join(e.repo, p), "x")
	}
	writeFile(t, filepath.Join(e.home, ".codex", "AGENTS.md"), "x")  // no config.toml or auth.json: no Codex
	writeFile(t, filepath.Join(e.home, ".gemini", "GEMINI.md"), "x") // Gemini CLI, not Antigravity
	got := listedFiles(t, []string{e.repo})
	if l := got[filepath.Join(e.repo, ".hermes.md")]; len(l) != 1 || l[0] != (listed{"hermes", false}) {
		t.Errorf(".hermes.md: %v", l)
	}
	if l := got[filepath.Join(e.repo, "HERMES.md")]; len(l) != 0 {
		t.Errorf("HERMES.md: %v", l)
	}
	// Claude Code and Cursor list it whether or not they are present (the
	// graph keeps it only for an agent there); not the absent new agents.
	if l := got[filepath.Join(e.repo, "AGENTS.md")]; len(l) != 2 || l[0].product != "claude-code" || l[1].product != "cursor" {
		t.Errorf("AGENTS.md: %v", l)
	}
	for _, p := range []string{filepath.Join(e.home, ".codex", "AGENTS.md"), filepath.Join(e.home, ".gemini", "GEMINI.md")} {
		if l := got[p]; len(l) != 0 {
			t.Errorf("%s listed for an absent agent: %v", p, l)
		}
	}
}

// In the graph, each present agent is an ai_client with its name, and its
// instruction files configure it.
func TestNewAgentsGraph(t *testing.T) {
	e := newNewAgentsEnv(t)
	writeFile(t, filepath.Join(e.home, ".codex", "auth.json"), "{}")
	writeFile(t, filepath.Join(e.repo, "AGENTS.md"), "x")
	writeFile(t, filepath.Join(e.appData, "Claude", "claude_desktop_config.json"), "{}")
	g, _ := instructionsGraph(t, windows.New(), []string{e.repo})
	names := map[string]string{}
	for _, n := range g.Nodes {
		if n.Type == graph.AgentAIClient {
			names[n.Platform] = n.Name
		}
	}
	if names["codex"] != "Codex" || names["claude-desktop"] != "Claude Desktop" || len(names) != 2 {
		t.Errorf("clients %v", names)
	}
	res := instructionResources(g)
	if res[canon(filepath.Join(e.repo, "AGENTS.md"))] != "codex" {
		t.Errorf("AGENTS.md resources %v", res)
	}
}
