package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-277: Continue is no longer maintained, so it is detected and not
// scored, with its own reason; its MCP servers are read elsewhere.
func TestOverview_ContinueDetectedNotEvaluated(t *testing.T) {
	g, _ := ovEvaluate(t, ovFixture{roots: allRoots})
	g.Nodes = append(g.Nodes,
		graph.Node{ID: "agent_client_cccccccccccccccc", Kind: graph.KindAgent, Type: graph.AgentAIClient, Platform: "continue", Name: "Continue"},
	)
	bases, _ := knowledge.All()
	res := rules.Evaluate(rules.Input{Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases, RepoRoots: allRoots, Home: home, Now: now})
	var found *rules.OverviewAgent
	for i, a := range res.AgentsOverview {
		if a.Agent == "Continue" {
			found = &res.AgentsOverview[i]
		}
	}
	if found == nil || found.Evaluated || found.NotEvaluatedReason == nil || *found.NotEvaluatedReason != rules.NotEvaluatedContinue {
		t.Fatalf("Continue: %+v", found)
	}
}

// BV-277: Claude Desktop with a desktop extension (assumed active) acts on
// the computer through MCP tools, assumed to run without asking: «nobody»,
// on the whole computer, assumed; with the managed policy turning extensions
// off, nothing.
func TestOverview_ClaudeDesktopWithExtension(t *testing.T) {
	g, _ := ovEvaluate(t, ovFixture{roots: allRoots})
	client, capID := "agent_client_dddddddddddddddd", "cap_dddddddddddddddd"
	g.Nodes = append(g.Nodes,
		graph.Node{ID: client, Kind: graph.KindAgent, Type: graph.AgentAIClient, Platform: "claude-desktop", Name: "Claude Desktop"},
		graph.Node{ID: capID, Kind: graph.KindCapability, Type: graph.CapInvokeMCPTool, Basis: graph.BasisProductDefault, InForce: true, Approval: graph.ApprovalAllow, TargetScope: "*", AppliesIn: "user", Source: "kb:claude-desktop/mcp_always_allow_assumed@" + knowledge.ClaudeDesktopVersion},
	)
	g.Edges = append(g.Edges, graph.Edge{ID: "edge_has_dddddddddddddddd", Type: graph.EdgeHas, From: client, To: capID})
	bases, _ := knowledge.All()
	desktop := func(scan platform.ClaudeDesktopScan) *rules.OverviewAgent {
		res := rules.Evaluate(rules.Input{Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases, RepoRoots: allRoots, Home: home, Now: now, ClaudeDesktop: scan})
		for i, a := range res.AgentsOverview {
			if a.Product == "claude-desktop" {
				return &res.AgentsOverview[i]
			}
		}
		t.Fatal("no Claude Desktop")
		return nil
	}
	scan := platform.ClaudeDesktopScan{Present: true, Extensions: []string{"ant.dir.filesystem"}}
	a := desktop(scan)
	want := map[string]string{rules.RowReadsFiles: rules.ScopeMachine, rules.RowWritesFiles: rules.ScopeMachine, rules.RowRunsCommands: rules.ScopeMachine, rules.RowSendsOut: rules.ScopeAnyDestination}
	for _, c := range a.Rows {
		if s, ok := want[c.Row]; ok && (c.Decider != rules.DeciderNobody || c.Scope != s || !c.Assumed) {
			t.Errorf("%s: %+v", c.Row, c)
		}
	}
	off := false
	scan.Managed = platform.ClaudeDesktopManaged{Status: platform.MCPSourceRead, Extensions: &off}
	for _, c := range desktop(scan).Rows[:4] {
		if c.Scope != rules.ScopeNone {
			t.Errorf("extensions off: %s %+v", c.Row, c)
		}
	}
}
