package rules_test

import (
	"slices"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-233 (CTO-48 §1): who decides on a catalog MCP server's write tools for
// Claude Code in auto mode and Cursor with Auto-review.

// mcpAgentEval evaluates one agent of product that loads Stripe at user level,
// with the given extra capabilities (a mode, an allow).
func mcpAgentEval(t *testing.T, product string, caps ...graph.Node) rules.Finding {
	t.Helper()
	seen := now.UTC().Format("2006-01-02T15:04:05Z07:00")
	client := "agent_client_" + product
	nodes := []graph.Node{
		{ID: client, Kind: graph.KindAgent, Type: graph.AgentAIClient, Name: product, Platform: product, ConfigPath: home + "/." + product, DiscoveredAt: seen},
		{
			ID: "mcp_stripe", Kind: graph.KindAgent, Type: graph.AgentMCPServer, Name: "stripe", Platform: product, ConfigPath: home + "/mcp.json", DiscoveredAt: seen,
			ExternalAction: &graph.ExternalActionMatch{ID: "stripe", Service: "Stripe", Category: "money", Writes: true},
		},
	}
	edges := []graph.Edge{{ID: "loads_stripe", Type: graph.EdgeLoads, From: client, To: "mcp_stripe", Source: home + "/mcp.json", Certainty: graph.CertaintyObserved, AppliesIn: "user"}}
	for i, c := range caps {
		c.ID, c.Kind, c.InForce, c.DiscoveredAt = "cap_"+string(rune('a'+i)), graph.KindCapability, true, seen
		nodes = append(nodes, c)
		edges = append(edges, graph.Edge{ID: "has_" + c.ID, Type: graph.EdgeHas, From: client, To: c.ID})
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	res := rules.Evaluate(rules.Input{
		Graph: graph.Graph{Nodes: nodes, Edges: edges}, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: []string{home + "/repo"}, Home: home, Now: now,
	})
	f, ok := mcpFindingBy(res, "Stripe")
	if !ok {
		t.Fatalf("%s: no Stripe finding", product)
	}
	return f
}

func modeCap(mode string, factory bool) graph.Node {
	c := graph.Node{Type: graph.CapPermissionMode, Approval: graph.ApprovalAllow, TargetScope: mode, AppliesIn: "user", Basis: graph.BasisConfigured, Rule: "defaultMode=" + mode}
	if factory {
		c.Basis, c.Rule, c.AppliesIn = graph.BasisProductDefault, "", ""
	}
	return c
}

func TestBV233_ClaudeCodeAutoMCP(t *testing.T) {
	// The factory mode (auto) and an auto set by the person: the classifier
	// decides; money is red; auto_classifier_permits.
	for _, factory := range []bool{true, false} {
		f := mcpAgentEval(t, knowledge.ClaudeCodeProduct, modeCap("auto", factory))
		if f.Params.Disposition != rules.MCPApprovalAnotherModel || f.Severity != rules.SeverityRed ||
			!slices.Contains(f.Evidence.Assumptions, graph.AssumeAutoClassifierPermits) {
			t.Errorf("auto (factory %v): %s %s %v", factory, f.Params.Disposition, f.Severity, f.Evidence.Assumptions)
		}
	}
	// default, acceptEdits and plan still ask a person.
	for _, mode := range []string{"default", "acceptEdits", "plan"} {
		if f := mcpAgentEval(t, knowledge.ClaudeCodeProduct, modeCap(mode, false)); f.Params.Disposition != rules.MCPApprovalPerson || f.Severity != rules.SeverityYellow {
			t.Errorf("%s: %s %s", mode, f.Params.Disposition, f.Severity)
		}
	}
	// A server-wide allow still means nobody, in auto too.
	allow := graph.Node{Type: graph.CapInvokeMCPTool, Approval: graph.ApprovalAllow, TargetScope: "mcp__stripe", Basis: graph.BasisConfigured, Rule: "mcp__stripe"}
	if f := mcpAgentEval(t, knowledge.ClaudeCodeProduct, modeCap("auto", true), allow); f.Params.Disposition != rules.MCPApprovalNobody {
		t.Errorf("auto + allow: %s", f.Params.Disposition)
	}
}

func TestBV233_CursorAutoReviewMCP(t *testing.T) {
	f := mcpAgentEval(t, knowledge.CursorProduct)
	if f.Params.Disposition != rules.MCPApprovalAnotherModel || f.Severity != rules.SeverityRed ||
		!slices.Contains(f.Evidence.Assumptions, graph.AssumeIDERunModeNotReadable) ||
		!slices.Contains(f.Evidence.Assumptions, graph.AssumeAutoClassifierPermits) {
		t.Errorf("no allowlist: %s %s %v", f.Params.Disposition, f.Severity, f.Evidence.Assumptions)
	}
	list := graph.Node{Type: graph.CapInvokeMCPTool, Approval: graph.ApprovalAllow, TargetScope: "stripe:*", Basis: graph.BasisConfigured, Rule: "stripe:*"}
	if f := mcpAgentEval(t, knowledge.CursorProduct, list); f.Params.Disposition != rules.MCPApprovalNobody {
		t.Errorf("allowlist: %s", f.Params.Disposition)
	}
}
