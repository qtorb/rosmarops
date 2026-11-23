package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
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
