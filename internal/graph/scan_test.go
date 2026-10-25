package graph_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

const (
	homeDir      = "/home/albert"
	repoRoot     = "/src/vigil"
	globalPath   = "/home/albert/.claude/settings.json"
	localPath    = "/src/vigil/.claude/settings.local.json"
	cursorConfig = "/home/albert/.cursor/mcp.json"
	clineConfig  = "/home/albert/AppData/Roaming/Code/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json"

	// Configuration folders: the identity of each product's ai_client.
	claudeDir = "/home/albert/.claude"
	cursorDir = "/home/albert/.cursor"
	clineDir  = "/home/albert/AppData/Roaming/Code/User/globalStorage/saoudrizwan.claude-dev/settings"
)

var testProvenance = graph.Provenance{EngineVersion: "0.2.0-dev", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostIDUnset}

// testSalt is a fixed 32-byte salt for deterministic fingerprints.
var testSalt = []byte("0123456789abcdef0123456789abcdef")

// fromScan runs FromMCPScan on scan with the test home, repo root and salt.
func fromScan(t *testing.T, scan platform.MCPScan) graph.Fragment {
	t.Helper()
	f, err := graph.FromMCPScan(scan, testSalt, scanTime)
	if err != nil {
		t.Fatalf("FromMCPScan: %v", err)
	}
	return f
}

// sampleScan is one source of each outcome: Claude Code global read with no
// servers, a project local file that failed to parse, Cline missing, and
// Cursor read with one server.
func sampleScan() platform.MCPScan {
	return platform.MCPScan{
		Servers: []platform.MCPServer{
			{Client: "cursor", Name: "github", Command: "npx", Args: []string{}, ConfigPath: cursorConfig, LoadsMCP: true, ConfigDir: cursorDir, Scope: platform.MCPScopeUser},
		},
		Sources: []platform.MCPSource{
			{Platform: "claude-code", ConfigPath: globalPath, ConfigDir: claudeDir, Status: platform.MCPSourceRead, LoadsMCP: true},
			{Platform: "claude-code-project", ConfigPath: localPath, ConfigDir: claudeDir, Status: platform.MCPSourceFailed, Reason: graph.Reason(graph.ReasonParseError, "unexpected end of JSON input"), LoadsMCP: true},
			{Platform: "cline", ConfigPath: clineConfig, ConfigDir: clineDir, Status: platform.MCPSourceMissing, LoadsMCP: true},
			{Platform: "cursor", ConfigPath: cursorConfig, ConfigDir: cursorDir, Status: platform.MCPSourceRead, LoadsMCP: true},
		},
	}
}

func TestClientsFromSources_ByStatus(t *testing.T) {
	clients, failed := graph.ClientsFromSources(sampleScan().Sources)

	if len(clients) != 2 || clients[0].ConfigPath != globalPath || clients[1].ConfigPath != cursorConfig {
		t.Errorf("clients: got %+v, want the two read sources (including the one without servers)", clients)
	}
	if len(failed) != 1 || failed[0].Path != localPath || !strings.HasPrefix(failed[0].Reason, graph.ReasonParseError+": ") {
		t.Errorf("failed: got %+v, want the malformed local file with its reason", failed)
	}
	for _, c := range clients {
		if c.ConfigPath == clineConfig {
			t.Error("missing source became a client")
		}
	}
	for _, f := range failed {
		if f.Path == clineConfig {
			t.Error("missing source became a failure")
		}
	}
}

func TestFromMCPScan_ReadSourceWithoutServersIsClient(t *testing.T) {
	f := fromScan(t, sampleScan())
	var client, config bool
	for _, n := range f.Nodes {
		if n.Type == graph.AgentAIClient && n.ConfigPath == claudeDir {
			client = true
		}
		if n.Type == graph.ResourceAgentConfig && n.Path == globalPath {
			config = true
		}
		if n.ConfigPath == localPath || n.Path == localPath || n.ConfigPath == clineDir || n.Path == clineConfig {
			t.Errorf("failed or missing source produced node %+v", n)
		}
	}
	if !client || !config {
		t.Errorf("Claude Code global config (read, no servers): ai_client %v, agent_config %v; want both", client, config)
	}
}

func TestFromMCPScan_RealFailuresInCoverage(t *testing.T) {
	f := fromScan(t, sampleScan())

	if len(f.Coverage.SourcesFailed) != 1 {
		t.Fatalf("sources_failed: got %+v, want only the malformed local file", f.Coverage.SourcesFailed)
	}
	sf := f.Coverage.SourcesFailed[0]
	if sf.Path != localPath || !strings.HasPrefix(sf.Reason, graph.ReasonParseError+": ") {
		t.Errorf("real read failure: got %+v, want %s with a parse_error reason", sf, localPath)
	}
	for _, unsupported := range []string{filepath.Join(homeDir, ".claude.json"), filepath.Join(repoRoot, ".mcp.json")} {
		for _, sf := range f.Coverage.SourcesFailed {
			if sf.Path == unsupported {
				t.Errorf("%s still declared in sources_failed: %+v", unsupported, sf)
			}
		}
	}
	if len(f.Coverage.SourcesRead) != 2 {
		t.Errorf("sources_read: got %v, want the two read files", f.Coverage.SourcesRead)
	}
}

func TestFromMCPScan_GraphValidates(t *testing.T) {
	g := graph.Build(fromScan(t, sampleScan()), testProvenance, scanTime)
	if _, err := graph.Encode(g); err != nil {
		t.Errorf("Encode: %v", err)
	}
}

func TestWriteFile_WritesValidGraph(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "reports") // does not exist yet
	g := graph.Build(fromScan(t, sampleScan()), testProvenance, scanTime)

	path, err := graph.WriteFile(dir, g)
	if err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if path != filepath.Join(dir, graph.FileName) {
		t.Errorf("path: got %q, want %q", path, filepath.Join(dir, graph.FileName))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 && os.Getenv("OS") != "Windows_NT" {
		t.Errorf("file mode: got %o, want 600", perm)
	}
	if _, err := graph.LoadGraph(path); err != nil {
		t.Errorf("written graph does not load: %v", err)
	}
}

func TestWriteFile_InvalidGraphRemovesStale(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, graph.FileName)
	if err := os.WriteFile(stale, []byte("{}"), 0o600); err != nil {
		t.Fatalf("write stale: %v", err)
	}

	g := graph.Build(fromScan(t, sampleScan()), testProvenance, scanTime)
	g.Edges[0].To = "agent_mcp_missing"

	if _, err := graph.WriteFile(dir, g); err == nil {
		t.Fatal("WriteFile with a broken graph: got nil error")
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale graph should be removed after a failed write, stat err = %v", err)
	}
}

func TestRemoveStale_NoFileIsNotAnError(t *testing.T) {
	if err := graph.RemoveStale(t.TempDir()); err != nil {
		t.Errorf("RemoveStale on empty dir: %v", err)
	}
}
