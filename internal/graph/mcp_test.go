package graph_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

var scanTime = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

const (
	cursorPath  = `C:\Users\albert\.cursor\mcp.json`
	clinePath   = `C:\Users\albert\AppData\Roaming\Code\User\globalStorage\saoudrizwan.claude-dev\settings\cline_mcp_settings.json`
	projectRoot = `C:\src\vigil`
	projectPath = `C:\src\vigil\.mcp.json`

	cursorFolder = `C:\Users\albert\.cursor`
	clineFolder  = `C:\Users\albert\AppData\Roaming\Code\User\globalStorage\saoudrizwan.claude-dev\settings`
	claudeFolder = `C:\Users\albert\.claude`
)

// sampleInput is three clients (one of them, Cline, with no servers) and
// three servers, two of them in the same Claude Code project file.
func sampleInput() ([]graph.ClientSource, []platform.MCPServer) {
	clients := []graph.ClientSource{
		{Platform: "cursor", ConfigPath: cursorPath, ConfigDir: cursorFolder},
		{Platform: "cline", ConfigPath: clinePath, ConfigDir: clineFolder},
		{Platform: "claude-code-project", ConfigPath: projectPath, ConfigDir: claudeFolder},
	}
	project := func(name string) platform.MCPServer {
		return platform.MCPServer{
			Client: "claude-code-project", Name: name, Command: "npx", ConfigPath: projectPath,
			ConfigDir: claudeFolder, Scope: platform.MCPScopeProject, ScopePath: projectRoot,
		}
	}
	servers := []platform.MCPServer{
		{Client: "cursor", Name: "github", Command: "npx", ConfigPath: cursorPath, ConfigDir: cursorFolder, Scope: platform.MCPScopeUser},
		project("postgres"),
		project("filesystem"),
	}
	return clients, servers
}

func buildSample() graph.Fragment {
	clients, servers := sampleInput()
	return graph.FromMCP(clients, servers, scanTime)
}

func countNodes(f graph.Fragment, agentType string) int {
	n := 0
	for _, node := range f.Nodes {
		if node.Kind == graph.KindAgent && node.Type == agentType {
			n++
		}
	}
	return n
}

func TestFromMCP_NodesAndEdges(t *testing.T) {
	f := buildSample()

	if got := countNodes(f, graph.AgentAIClient); got != 3 {
		t.Errorf("ai_client nodes: got %d, want 3 (including Cline with no servers)", got)
	}
	if got := countNodes(f, graph.AgentMCPServer); got != 3 {
		t.Errorf("mcp_server nodes: got %d, want 3", got)
	}
	configs := map[string]graph.Node{} // path → agent_config resource
	for _, n := range f.Nodes {
		if n.Kind == graph.KindResource && n.Type == graph.ResourceAgentConfig {
			configs[n.Path] = n
		}
	}
	if len(configs) != 3 {
		t.Errorf("agent_config resources: got %d, want 3 (one per file read)", len(configs))
	}
	if len(f.Nodes) != 9 {
		t.Errorf("total nodes: got %d, want 9", len(f.Nodes))
	}
	if len(f.Edges) != 3 {
		t.Fatalf("edges: got %d, want 3", len(f.Edges))
	}

	byID := make(map[string]graph.Node, len(f.Nodes))
	for _, n := range f.Nodes {
		byID[n.ID] = n
	}
	for _, e := range f.Edges {
		from, to := byID[e.From], byID[e.To]
		if e.Type != graph.EdgeLoads || from.Type != graph.AgentAIClient || to.Type != graph.AgentMCPServer {
			t.Errorf("edge %s: got %s %s -> %s, want loads ai_client -> mcp_server", e.ID, e.Type, from.Type, to.Type)
		}
		if e.Source != to.ConfigPath || configs[e.Source].Configures != from.ID {
			t.Errorf("edge %s: source %q must be the server's file and an agent_config of client %s", e.ID, e.Source, from.ID)
		}
		project := to.ConfigPath == projectPath
		switch {
		case project && (e.Certainty != graph.CertaintyInferred || e.ApprovalState != graph.ApprovalUnknown || e.AppliesIn != "project:c:/src/vigil"):
			t.Errorf("project edge %s: got %s/%q/%q, want inferred/unknown/project:c:/src/vigil", e.ID, e.Certainty, e.ApprovalState, e.AppliesIn)
		case !project && (e.Certainty != graph.CertaintyObserved || e.ApprovalState != "" || e.AppliesIn != "user"):
			t.Errorf("user edge %s: got %s/%q/%q, want observed/no approval/user", e.ID, e.Certainty, e.ApprovalState, e.AppliesIn)
		}
	}
	if err := graph.ValidateIntegrity(graph.Graph{Nodes: f.Nodes, Edges: f.Edges}); err != nil {
		t.Errorf("fragment integrity: %v", err)
	}
}

func TestFromMCP_ClientWithoutServersIsRecorded(t *testing.T) {
	f := buildSample()
	for _, n := range f.Nodes {
		if n.Type == graph.AgentAIClient && n.Platform == "cline" {
			return
		}
	}
	t.Error("Cline client (config read, no servers) has no ai_client node")
}

func TestFromMCP_DeterministicIDs(t *testing.T) {
	a, b := buildSample(), buildSample()
	if !reflect.DeepEqual(a, b) {
		t.Errorf("two builds from the same input differ:\n%+v\n%+v", a, b)
	}

	// Input order must not matter either.
	clients, servers := sampleInput()
	servers[0], servers[2] = servers[2], servers[0]
	c := graph.FromMCP(clients, servers, scanTime)
	if !reflect.DeepEqual(a, c) {
		t.Errorf("reordered input produced a different fragment")
	}
}

func TestFromMCP_SameFileServersGetDistinctIDs(t *testing.T) {
	// postgres and filesystem are both declared in projectPath.
	ids := map[string]bool{}
	for _, n := range buildSample().Nodes {
		if n.Type == graph.AgentMCPServer && n.ConfigPath == projectPath {
			ids[n.ID] = true
		}
	}
	if len(ids) != 2 {
		t.Errorf("servers in %s: got %d distinct ids %v, want 2", projectPath, len(ids), ids)
	}
}

func TestFromMCP_PathCanonicalizationGivesSameID(t *testing.T) {
	build := func(path string) graph.Fragment {
		return graph.FromMCP(nil, []platform.MCPServer{
			{Client: "cursor", Name: "github", ConfigPath: path},
		}, scanTime)
	}
	ids := func(f graph.Fragment) []string {
		var out []string
		for _, n := range f.Nodes {
			out = append(out, n.ID)
		}
		for _, e := range f.Edges {
			out = append(out, e.ID)
		}
		return out
	}

	a := ids(build(`C:\Users\Albert\.cursor\mcp.json`))
	b := ids(build(`c:/users/albert/.cursor/MCP.json`))
	if !reflect.DeepEqual(a, b) {
		t.Errorf("same Windows path, different case/slashes: ids differ\n%v\n%v", a, b)
	}

	// Unix paths are case-sensitive and must keep distinct ids.
	c := ids(build(`/home/albert/.cursor/mcp.json`))
	d := ids(build(`/home/Albert/.cursor/mcp.json`))
	if reflect.DeepEqual(c, d) {
		t.Errorf("Unix paths differing in case got the same ids: %v", c)
	}
}

func TestFromMCP_CoverageHasNoUnsupportedSources(t *testing.T) {
	// ~/.claude.json and <repo>/.mcp.json are read now: FromMCP no longer
	// declares them as source_unsupported.
	f := buildSample()
	for _, sf := range f.Coverage.SourcesFailed {
		if strings.HasPrefix(sf.Reason, graph.ReasonSourceUnsupported) {
			t.Errorf("unexpected unsupported source in coverage: %+v", sf)
		}
	}
	if len(f.Coverage.SourcesRead) != 3 {
		t.Errorf("sources_read: got %v, want the 3 client config files", f.Coverage.SourcesRead)
	}
}

func TestEncode_MCPFragmentValidatesAgainstSchema(t *testing.T) {
	g := graph.Build(buildSample(), graph.Provenance{
		EngineVersion:    "0.2.0-dev",
		RulesVersion:     graph.RulesVersion,
		KnowledgeVersion: graph.KnowledgeVersion,
		HostID:           graph.HostIDUnset,
	}, scanTime)

	out, err := graph.Encode(g)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}

	// Round-trip through the loader: schema + integrity must pass on disk too.
	path := filepath.Join(t.TempDir(), "graph.v2.json")
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := graph.LoadGraph(path)
	if err != nil {
		t.Fatalf("LoadGraph(encoded): %v", err)
	}
	if len(loaded.Nodes) != len(g.Nodes) || len(loaded.Edges) != len(g.Edges) {
		t.Errorf("round trip: got %d nodes / %d edges, want %d / %d",
			len(loaded.Nodes), len(loaded.Edges), len(g.Nodes), len(g.Edges))
	}
}

func TestEncode_EmptyFragmentValidates(t *testing.T) {
	g := graph.Build(graph.Fragment{}, graph.Provenance{
		EngineVersion: "0.2.0-dev", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostIDUnset,
	}, scanTime)
	if _, err := graph.Encode(g); err != nil {
		t.Errorf("empty graph: %v", err)
	}
}

func TestEncode_FixtureRoundTrip(t *testing.T) {
	// The fixture uses every node kind and most edge types; encoding it must
	// emit only each variant's fields and still validate.
	g := loadFixture(t)
	if _, err := graph.Encode(g); err != nil {
		t.Errorf("Encode(fixture): %v", err)
	}
}

func TestEncode_RejectsInvalidGraph(t *testing.T) {
	g := graph.Build(buildSample(), graph.Provenance{
		EngineVersion: "0.2.0-dev", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostIDUnset,
	}, scanTime)

	broken := g
	broken.Edges = append([]graph.Edge(nil), g.Edges...)
	broken.Edges[0].To = "agent_mcp_missing"
	if _, err := graph.Encode(broken); err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Errorf("dangling edge: got %v, want integrity error", err)
	}

	bad := g
	bad.Edges = append([]graph.Edge(nil), g.Edges...)
	bad.Edges[0].Certainty = "confirmed"
	if _, err := graph.Encode(bad); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Errorf("invalid certainty: got %v, want schema error", err)
	}
}
