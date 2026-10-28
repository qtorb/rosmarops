package report

import (
	"html"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/memory"
)

// Decision 2026-10-26 (agentes y MCP nuevos, A1): an MCP server is compared
// by client, name and scope, not by its node id: moved to another file of
// the same scope it is not new; moved from user to project it is.
func TestSeenAgents_MCPScope(t *testing.T) {
	salt := []byte("0123456789abcdef0123456789abcdef")
	g := func(serverID, appliesIn string) graph.Graph {
		return graph.Graph{
			Nodes: []graph.Node{
				{ID: "agent_client_cu", Kind: graph.KindAgent, Type: graph.AgentAIClient, Name: "Cursor", Platform: "cursor"},
				{ID: serverID, Kind: graph.KindAgent, Type: graph.AgentMCPServer, Name: "browser", Platform: "cursor"},
			},
			Edges: []graph.Edge{{ID: "edge_loads_" + serverID, Type: graph.EdgeLoads, From: "agent_client_cu", To: serverID, AppliesIn: appliesIn}},
		}
	}
	run := func(day string, gr graph.Graph) memory.Run {
		return memory.Run{Date: day, Report: "r", RulesVersion: "1", KnowledgeVersion: "k", Agents: []string{"Cursor"}, Repos: 1, Complete: true, Seen: seenAgents(gr)}
	}
	m, _, _ := memory.Update(memory.File{}, run("2026-10-01", g("agent_mcp_a", "user")), nil, salt)
	m, _, root := memory.Update(m, run("2026-10-02", g("agent_mcp_b", "user")), nil, salt)
	if root.NewMCP != nil || root.NewAgents != nil {
		t.Errorf("moved within user scope: %v %v", root.NewMCP, root.NewAgents)
	}
	_, _, root = memory.Update(m, run("2026-10-03", g("agent_mcp_c", "project:/src/a")), nil, salt)
	wantMoved := []memory.MCPRef{{Name: "browser", Key: "mcp\x00cursor\x00browser\x00project:/src/a"}}
	if !reflect.DeepEqual(root.NewMCP, wantMoved) {
		t.Errorf("user → project: %v", root.NewMCP)
	}

	// A8 (BV-60): a report over that memory with another salt carries the
	// reason salt_changed; report.v2.json validates, and the HTML (en, es)
	// and the technical Markdown say the whole sentence.
	gr := g("agent_mcp_b", "user")
	gr.Provenance = graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: "k", HostID: "host_0123456789abcdef"}
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)
	v, _ := BuildV2(V2Input{
		Graph: &gr, GraphSHA256: strings.Repeat("ab", 32), Base: base, RepoRoots: []string{"/src/a"},
		V1: Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: at, Memory: m, Salt: []byte("another-salt-another-salt-012345"),
	})
	if v.Memory.Reason != memory.ReasonSaltChanged || v.NewMCP != nil {
		t.Fatalf("memory %+v, new MCP %v", v.Memory, v.NewMCP)
	}
	raw, err := RenderV2JSON(v)
	if err != nil {
		t.Fatalf("report.v2.json with salt_changed: %v", err)
	}
	if !strings.Contains(string(raw), `"reason": "salt_changed"`) {
		t.Error("report.v2.json lacks the reason")
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		line := cat.Text("memory.not_comparable", map[string]string{"reason": cat.Text("memory.reason.salt_changed", nil)})
		if !strings.Contains(RenderHTML(v, cat, "/h", time.UTC), "<p>"+html.EscapeString(line)+"</p>") {
			t.Errorf("%s: the HTML lacks %q", lang, line)
		}
		if lang == "en" && !strings.Contains(RenderV2Markdown(v, cat, time.UTC), "\n"+line+"\n") {
			t.Errorf("the Markdown lacks %q", line)
		}
	}
}
