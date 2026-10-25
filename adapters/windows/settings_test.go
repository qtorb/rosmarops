package windows_test

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

const fakeSettingsToken = "ghp_SETTINGSabcdefghijklmnopqrstuvwxyz01"

// settingsWithMCP is a Claude Code settings file declaring MCP servers (which
// Claude Code does not load from settings files) and a top-level env block.
const settingsWithMCP = `{
  "env": {"GITHUB_TOKEN": "` + fakeSettingsToken + `"},
  "mcpServers": {
    "from-settings": {"command": "npx", "env": {"GH_TOKEN": "` + fakeSettingsToken + `"}},
    "remote-in-settings": {"type": "http", "url": "https://x", "headers": {"Authorization": "Bearer ` + fakeSettingsToken + `"}}
  }
}`

// scanHome sets up a temporary HOME and repo, writes files (paths relative to
// HOME or to the repo when prefixed "repo/"), and runs ListMCPServers.
func scanHome(t *testing.T, files map[string]string) platform.MCPScan {
	t.Helper()
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	for rel, content := range files {
		path := filepath.Join(home, rel)
		if strings.HasPrefix(rel, "repo/") {
			path = filepath.Join(repo, strings.TrimPrefix(rel, "repo/"))
		}
		writeFile(t, path, content)
	}
	scan, err := windows.New().ListMCPServers(context.Background(), []string{repo})
	if err != nil {
		t.Fatalf("ListMCPServers: %v", err)
	}
	return scan
}

func fragment(t *testing.T, scan platform.MCPScan) graph.Fragment {
	t.Helper()
	f, err := graph.FromMCPScan(scan, testSalt, testTime)
	if err != nil {
		t.Fatalf("FromMCPScan: %v", err)
	}
	return f
}

func TestSettingsJSON_NotAnMCPSourceInGraph(t *testing.T) {
	for name, rel := range map[string]string{
		"global settings.json":        ".claude/settings.json",
		"project settings.json":       "repo/.claude/settings.json",
		"project settings.local.json": "repo/.claude/settings.local.json",
	} {
		t.Run(name, func(t *testing.T) {
			scan := scanHome(t, map[string]string{rel: settingsWithMCP})
			f := fragment(t, scan)
			if len(f.Nodes) != 0 || len(f.Edges) != 0 {
				t.Errorf("settings file produced %d nodes and %d edges, want none: %+v", len(f.Nodes), len(f.Edges), f.Nodes)
			}
			for _, p := range f.Coverage.SourcesRead {
				if isSettingsFile(p) {
					t.Errorf("settings file listed in sources_read: %s", p)
				}
			}
			out, err := graph.Encode(graph.Build(f, graph.Provenance{EngineVersion: "0.1.0-dev", RulesVersion: "0", KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(testSalt)}, testTime))
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}
			if strings.Contains(string(out), fakeSettingsToken) || strings.Contains(string(out), "from-settings") {
				t.Error("graph contains data from the settings file")
			}
		})
	}
}

func TestSettingsJSON_StillFedToV1Scanner(t *testing.T) {
	scan := scanHome(t, map[string]string{
		".claude/settings.json":            settingsWithMCP,
		"repo/.claude/settings.local.json": settingsWithMCP,
	})
	var fromSettings int
	for _, s := range scan.Servers {
		if isSettingsFile(s.ConfigPath) {
			fromSettings++
			if s.LoadsMCP {
				t.Errorf("server %s from %s has LoadsMCP true", s.Name, s.ConfigPath)
			}
		}
	}
	if fromSettings != 4 {
		t.Errorf("v1 input: got %d servers from settings files, want 4 (2 per file)", fromSettings)
	}
	for _, src := range scan.Sources {
		isSettings := isSettingsFile(src.ConfigPath)
		if src.LoadsMCP == isSettings {
			t.Errorf("source %s: LoadsMCP = %v, want %v", src.ConfigPath, src.LoadsMCP, !isSettings)
		}
	}
}

func TestSettingsJSON_DoesNotChangeOtherSourcesIDs(t *testing.T) {
	home, repo := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	writeFile(t, filepath.Join(home, ".claude.json"), claudeJSON)
	writeFile(t, filepath.Join(repo, ".mcp.json"), `{"mcpServers": {"shared": {"command": "npx", "env": {"T": "`+fakeHeaderToken+`"}}}}`)

	scan := func() graph.Fragment {
		s, err := windows.New().ListMCPServers(context.Background(), []string{repo})
		if err != nil {
			t.Fatalf("ListMCPServers: %v", err)
		}
		return fragment(t, s)
	}
	without := scan()

	// Same directories, now with settings files declaring MCP servers and
	// the same secret as .mcp.json.
	writeFile(t, filepath.Join(home, ".claude", "settings.json"), settingsWithMCP)
	writeFile(t, filepath.Join(repo, ".claude", "settings.local.json"),
		`{"mcpServers": {"x": {"command": "npx", "env": {"T": "`+fakeHeaderToken+`"}}}}`)
	withSettings := scan()

	if len(without.Nodes) == 0 {
		t.Fatal("test setup: no nodes from ~/.claude.json and .mcp.json")
	}
	if !reflect.DeepEqual(without, withSettings) {
		t.Errorf("settings files changed the graph built from ~/.claude.json and .mcp.json:\nwithout: %+v\nwith:    %+v", without, withSettings)
	}
}

// isSettingsFile reports whether path is a Claude Code settings file
// (settings.json or settings.local.json), by exact base name.
func isSettingsFile(path string) bool {
	base := filepath.Base(path)
	return base == "settings.json" || base == "settings.local.json"
}
