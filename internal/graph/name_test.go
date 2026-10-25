package graph_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

// writeFixtureVariant writes a copy of the fixture, changed by mutate, to a
// temp file and returns its path.
func writeFixtureVariant(t *testing.T, mutate func(doc map[string]any)) string {
	t.Helper()
	raw, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	mutate(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "graph.json")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

// fixtureNode returns the fixture node with the given id from a decoded doc.
func fixtureNode(t *testing.T, doc map[string]any, id string) map[string]any {
	t.Helper()
	for _, n := range doc["nodes"].([]any) {
		node := n.(map[string]any)
		if node["id"] == id {
			return node
		}
	}
	t.Fatalf("fixture has no node %q", id)
	return nil
}

func TestFromMCP_AgentNodesHaveNames(t *testing.T) {
	want := map[string]string{
		// mcp_server: the server's key in mcpServers.
		"github": graph.AgentMCPServer, "postgres": graph.AgentMCPServer, "filesystem": graph.AgentMCPServer,
		// ai_client: the product's display name.
		"Cursor": graph.AgentAIClient, "Cline": graph.AgentAIClient, "Claude Code": graph.AgentAIClient,
	}
	got := map[string]string{}
	for _, n := range buildSample().Nodes {
		if n.Kind != graph.KindAgent {
			continue
		}
		if n.Name == "" {
			t.Errorf("node %s (%s) has an empty name", n.ID, n.Type)
		}
		got[n.Name] = n.Type
	}
	for name, typ := range want {
		if got[name] != typ {
			t.Errorf("name %q: got type %q, want %q", name, got[name], typ)
		}
	}
}

func TestFromMCP_UnknownPlatformNameFallsBackToLabel(t *testing.T) {
	f := graph.FromMCP([]graph.ClientSource{{Platform: "windsurf", ConfigPath: "/home/a/.codeium/mcp.json", ConfigDir: "/home/a/.codeium"}},
		nil, scanTime)
	var clients []graph.Node
	for _, n := range f.Nodes {
		if n.Type == graph.AgentAIClient {
			clients = append(clients, n)
		}
	}
	if len(clients) != 1 || clients[0].Name != "windsurf" {
		t.Errorf("unknown platform: got %+v, want one ai_client named %q", clients, "windsurf")
	}
}

func TestEncode_MCPFragmentEmitsServerNames(t *testing.T) {
	g := graph.Build(buildSample(), graph.Provenance{
		EngineVersion: "0.2.0-dev", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostIDUnset,
	}, scanTime)
	out, err := graph.Encode(g)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for _, name := range []string{`"name": "postgres"`, `"name": "filesystem"`, `"name": "github"`} {
		if !strings.Contains(string(out), name) {
			t.Errorf("encoded graph does not contain %s", name)
		}
	}
}

func TestEncode_MCPServerWithoutNameRejected(t *testing.T) {
	f := graph.FromMCP(nil, []platform.MCPServer{
		{Client: "cursor", Name: "github", ConfigPath: "/home/a/.cursor/mcp.json"},
	}, scanTime)
	for i := range f.Nodes {
		if f.Nodes[i].Type == graph.AgentMCPServer {
			f.Nodes[i].Name = ""
		}
	}
	g := graph.Build(f, graph.Provenance{EngineVersion: "0.2.0-dev", RulesVersion: "0", KnowledgeVersion: graph.KnowledgeVersion, HostID: "unset"}, scanTime)
	if _, err := graph.Encode(g); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Errorf("mcp_server without name: got %v, want schema error", err)
	}
}

func TestLoadGraph_MCPServerWithoutNameRejected(t *testing.T) {
	path := writeFixtureVariant(t, func(doc map[string]any) {
		delete(fixtureNode(t, doc, "agent_mcp_postgres"), "name")
	})
	if _, err := graph.LoadGraph(path); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Errorf("mcp_server without name: got %v, want schema error", err)
	}
}

func TestLoadGraph_AIClientWithoutNameAccepted(t *testing.T) {
	path := writeFixtureVariant(t, func(doc map[string]any) {
		delete(fixtureNode(t, doc, "agent_cursor"), "name")
	})
	if _, err := graph.LoadGraph(path); err != nil {
		t.Errorf("ai_client without name should be valid: %v", err)
	}
}
