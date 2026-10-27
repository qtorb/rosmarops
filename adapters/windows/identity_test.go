package windows_test

// Consolidated identity (decision 2026-10-23, S7): one ai_client per product
// and configuration folder, one agent_config resource per file read, and
// loads edges with the scope of each server.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/graph"
)

type identityEnv struct {
	home, appData, repoA, repoB, projA, projB string
}

func newIdentityEnv(t *testing.T) identityEnv {
	t.Helper()
	root := t.TempDir()
	e := identityEnv{
		home:  filepath.Join(root, "home"),
		repoA: filepath.Join(root, "src", "repo-a"),
		repoB: filepath.Join(root, "src", "repo-b"),
		projA: filepath.Join(root, "src", "local-a"),
		projB: filepath.Join(root, "src", "local-b"),
	}
	e.appData = filepath.Join(e.home, "AppData", "Roaming")
	t.Setenv("HOME", e.home)
	t.Setenv("USERPROFILE", e.home)
	t.Setenv("APPDATA", e.appData)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	os.Unsetenv("CLAUDE_CONFIG_DIR")
	return e
}

func (e identityEnv) writeAll(t *testing.T, withCline bool) {
	t.Helper()
	writeFile(t, filepath.Join(e.home, ".claude.json"), `{
		"mcpServers": {"fs": {"command": "npx"}},
		"projects": {
			"`+filepath.ToSlash(e.projA)+`": {"mcpServers": {"db": {"command": "npx"}}},
			"`+filepath.ToSlash(e.projB)+`": {"mcpServers": {"db": {"command": "npx"}}}
		}
	}`)
	writeFile(t, filepath.Join(e.repoA, ".mcp.json"), `{"mcpServers": {"shared": {"command": "npx"}}}`)
	writeFile(t, filepath.Join(e.repoB, ".mcp.json"), `{"mcpServers": {"shared": {"command": "npx"}}}`)
	writeFile(t, filepath.Join(e.home, ".cursor", "mcp.json"), `{"mcpServers": {"github": {"command": "npx"}}}`)
	if withCline {
		writeFile(t, filepath.Join(e.appData, "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "cline_mcp_settings.json"),
			`{"mcpServers": {"browser": {"command": "npx"}}}`)
	}
}

func buildIdentityGraph(t *testing.T, e identityEnv) graph.Graph {
	t.Helper()
	scan, err := windows.New().ListMCPServers(context.Background(), []string{e.repoA, e.repoB})
	if err != nil {
		t.Fatalf("ListMCPServers: %v", err)
	}
	f, err := graph.FromMCPScan(scan, testSalt, testTime)
	if err != nil {
		t.Fatalf("FromMCPScan: %v", err)
	}
	g := graph.Build(f, graph.Provenance{EngineVersion: "0.2.0-dev", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(testSalt)}, testTime)
	if _, err := graph.Encode(g); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return g
}

// canon is the graph's canonical form of a path built by the test (Unix test
// host: slashes only, case kept).
// canon is the graph's canonical form of a path (forward slashes, lower case
// for a Windows path), the one appliesIn writes into the scope.
func canon(p string) string { return graph.CanonicalPath(p) }

func TestIdentity_OneClientPerProductWithConfigsAndScopes(t *testing.T) {
	e := newIdentityEnv(t)
	e.writeAll(t, true)
	g := buildIdentityGraph(t, e)

	byID := map[string]graph.Node{}
	clients := map[string]graph.Node{} // platform → client
	configs := map[string][]string{}   // client id → agent_config paths
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	for _, n := range g.Nodes {
		switch {
		case n.Type == graph.AgentAIClient:
			if _, dup := clients[n.Platform]; dup {
				t.Errorf("second ai_client for %s: %+v", n.Platform, n)
			}
			clients[n.Platform] = n
		case n.Type == graph.ResourceAgentConfig:
			configs[n.Configures] = append(configs[n.Configures], n.Path)
		}
		if n.Platform == "claude-code-project" {
			t.Errorf("platform claude-code-project still emitted: %+v", n)
		}
	}

	cc, cursor, cline := clients["claude-code"], clients["cursor"], clients["cline"]
	if len(clients) != 3 || cc.ID == "" || cursor.ID == "" || cline.ID == "" {
		t.Fatalf("ai_clients: got %v, want exactly claude-code, cursor and cline", clients)
	}
	wantDirs := map[string]string{
		cc.ID:     filepath.Join(e.home, ".claude"),
		cursor.ID: filepath.Join(e.home, ".cursor"),
		cline.ID:  filepath.Join(e.appData, "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings"),
	}
	for id, dir := range wantDirs {
		if byID[id].ConfigPath != dir {
			t.Errorf("%s config_path: got %q, want folder %q", byID[id].Name, byID[id].ConfigPath, dir)
		}
	}
	if cc.Name != "Claude Code" || cursor.Name != "Cursor" || cline.Name != "Cline" {
		t.Errorf("names: %q %q %q", cc.Name, cursor.Name, cline.Name)
	}

	// One agent_config per file read: ~/.claude.json and the two .mcp.json
	// configure Claude Code; mcp.json and cline_mcp_settings.json their clients.
	sort.Strings(configs[cc.ID])
	wantCC := []string{filepath.Join(e.home, ".claude.json"), filepath.Join(e.repoA, ".mcp.json"), filepath.Join(e.repoB, ".mcp.json")}
	sort.Strings(wantCC)
	if strings.Join(configs[cc.ID], "|") != strings.Join(wantCC, "|") {
		t.Errorf("Claude Code agent_config: got %v, want %v", configs[cc.ID], wantCC)
	}
	if len(configs[cursor.ID]) != 1 || len(configs[cline.ID]) != 1 {
		t.Errorf("Cursor/Cline agent_config: got %v / %v, want one each", configs[cursor.ID], configs[cline.ID])
	}

	// loads: from the product's single client, applies_in per scope.
	type want struct{ certainty, approval string }
	wantLoads := map[string]want{
		"claude-code user":                      {graph.CertaintyObserved, ""},
		"claude-code local:" + canon(e.projA):   {graph.CertaintyObserved, ""},
		"claude-code local:" + canon(e.projB):   {graph.CertaintyObserved, ""},
		"claude-code project:" + canon(e.repoA): {graph.CertaintyInferred, graph.ApprovalUnknown},
		"claude-code project:" + canon(e.repoB): {graph.CertaintyInferred, graph.ApprovalUnknown},
		"cursor user":                           {graph.CertaintyObserved, ""},
		"cline user":                            {graph.CertaintyObserved, ""},
	}
	got := map[string]want{}
	for _, edge := range g.Edges {
		if edge.Type != graph.EdgeLoads {
			continue
		}
		from := byID[edge.From]
		if from.ID != clients[from.Platform].ID {
			t.Errorf("loads %s does not start at the product's client", edge.ID)
		}
		key := from.Platform + " " + edge.AppliesIn
		if _, dup := got[key]; dup {
			t.Errorf("two loads for %s", key)
		}
		got[key] = want{edge.Certainty, edge.ApprovalState}
	}
	for k, w := range wantLoads {
		if got[k] != w {
			t.Errorf("loads %s: got %+v, want %+v", k, got[k], w)
		}
	}
	if len(got) != len(wantLoads) {
		t.Errorf("loads: got %v, want %v", got, wantLoads)
	}

	// coverage lists files, not folders.
	for _, p := range g.Coverage.SourcesRead {
		if p == wantDirs[cc.ID] || p == wantDirs[cursor.ID] {
			t.Errorf("sources_read lists a folder: %s", p)
		}
	}
	if len(g.Coverage.SourcesRead) != 5 {
		t.Errorf("sources_read: got %v, want the 5 files", g.Coverage.SourcesRead)
	}
}

func TestIdentity_ClaudeConfigDirIsTheClientFolder(t *testing.T) {
	e := newIdentityEnv(t)
	e.writeAll(t, false)
	custom := filepath.Join(t.TempDir(), "claude-config")
	t.Setenv("CLAUDE_CONFIG_DIR", custom)
	g := buildIdentityGraph(t, e)
	for _, n := range g.Nodes {
		if n.Type == graph.AgentAIClient && n.Platform == "claude-code" && n.ConfigPath != custom {
			t.Errorf("Claude Code config_path: got %q, want CLAUDE_CONFIG_DIR %q", n.ConfigPath, custom)
		}
	}
}

func TestIdentity_ProductWithoutFilesHasNoClient(t *testing.T) {
	e := newIdentityEnv(t)
	e.writeAll(t, false) // no Cline file
	g := buildIdentityGraph(t, e)
	for _, n := range g.Nodes {
		if n.Platform == "cline" {
			t.Errorf("Cline has no file read but produced %+v", n)
		}
	}
}
