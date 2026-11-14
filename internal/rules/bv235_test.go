package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-235 (CTO-48 §3.5–§3.7, R3, R5): the five risk kinds and «Lo asumo».

// kindsEval evaluates one agent of product, loading Stripe at user level when
// stripe is set, with the given capabilities and accepted list.
func kindsEval(t *testing.T, product string, stripe bool, accepted []config.AllowEntryAccepted, caps ...graph.Node) rules.Result {
	t.Helper()
	seen := now.UTC().Format("2006-01-02T15:04:05Z07:00")
	client := "agent_client_" + product
	nodes := []graph.Node{{ID: client, Kind: graph.KindAgent, Type: graph.AgentAIClient, Name: product, Platform: product, ConfigPath: home + "/." + product, DiscoveredAt: seen}}
	var edges []graph.Edge
	if stripe {
		nodes = append(nodes, graph.Node{
			ID: "mcp_stripe", Kind: graph.KindAgent, Type: graph.AgentMCPServer, Name: "stripe", Platform: product, ConfigPath: home + "/mcp.json", DiscoveredAt: seen,
			ExternalAction: &graph.ExternalActionMatch{ID: "stripe", Service: "Stripe", Category: "money", Writes: true},
		})
		edges = append(edges, graph.Edge{ID: "loads_stripe", Type: graph.EdgeLoads, From: client, To: "mcp_stripe", Source: home + "/mcp.json", Certainty: graph.CertaintyObserved, AppliesIn: "user"})
	}
	for i, c := range caps {
		c.ID, c.Kind, c.InForce, c.DiscoveredAt = "cap_"+string(rune('a'+i)), graph.KindCapability, true, seen
		nodes = append(nodes, c)
		edges = append(edges, graph.Edge{ID: "has_" + c.ID, Type: graph.EdgeHas, From: client, To: c.ID})
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	return rules.Evaluate(rules.Input{
		Graph: graph.Graph{Nodes: nodes, Edges: edges}, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: []string{home + "/repo"}, Home: home, Now: now, Accepted: accepted,
	})
}

func kindOf(t *testing.T, res rules.Result, product, kind string) rules.RiskKindCell {
	t.Helper()
	for _, a := range res.AgentsOverview {
		if a.Product != product {
			continue
		}
		if len(a.Kinds) != len(rules.KindOrder) {
			t.Fatalf("%s: %d kinds, want %d", product, len(a.Kinds), len(rules.KindOrder))
		}
		for i, k := range a.Kinds {
			if k.Kind != rules.KindOrder[i] {
				t.Fatalf("%s: kind %d is %s, want %s", product, i, k.Kind, rules.KindOrder[i])
			}
			if k.Kind == kind {
				return k
			}
		}
	}
	t.Fatalf("%s: no %s kind", product, kind)
	return rules.RiskKindCell{}
}

// TestBV235_MoneyKind (§3.6): with a payment service, its «who decides»; with
// none, none_known; for an agent whose MCP «who decides» is not read, unknown.
func TestBV235_MoneyKind(t *testing.T) {
	cases := []struct {
		name, product string
		stripe        bool
		caps          []graph.Node
		state, source string
	}{
		{"claude auto + stripe", knowledge.ClaudeCodeProduct, true, []graph.Node{modeCap("auto", true)}, rules.StateModel, rules.KindSourceCatalog},
		{"claude default + stripe", knowledge.ClaudeCodeProduct, true, []graph.Node{modeCap("default", false)}, rules.StateYou, rules.KindSourceCatalog},
		{"claude without a payment service", knowledge.ClaudeCodeProduct, false, []graph.Node{modeCap("default", false)}, rules.StateNoneKnown, rules.KindSourceCatalog},
		{"cursor + stripe", knowledge.CursorProduct, true, nil, rules.StateModel, rules.KindSourceCatalog},
		{"gemini-cli + stripe", "gemini-cli", true, nil, rules.StateUnknown, rules.KindSourceCatalog},
		{"cline", "cline", false, nil, rules.StateUnknown, rules.KindSourceCatalog},
	}
	for _, c := range cases {
		k := kindOf(t, kindsEval(t, c.product, c.stripe, nil, c.caps...), c.product, rules.KindMoney)
		if k.State != c.state || k.Source != c.source {
			t.Errorf("%s: money %+v, want %s from %s", c.name, k, c.state, c.source)
		}
	}
	// Cursor's mode is not readable: its money state rests on an assumption.
	if k := kindOf(t, kindsEval(t, knowledge.CursorProduct, true, nil), knowledge.CursorProduct, rules.KindMoney); !k.Assumed {
		t.Errorf("cursor money: %+v, want assumed", k)
	}
}

// TestBV235_SendsOutNeverBounded (R5): sending out is never bounded.
func TestBV235_SendsOutNeverBounded(t *testing.T) {
	for _, p := range []string{knowledge.ClaudeCodeProduct, knowledge.CursorProduct, "codex", "grok-build", "hermes", "gemini-cli", "github-copilot", "cline", "antigravity-cli"} {
		res := kindsEval(t, p, true, nil)
		for _, a := range res.AgentsOverview {
			for _, k := range a.Kinds {
				if k.Kind == rules.KindSendsOut && k.State == rules.StateBounded {
					t.Errorf("%s: sends_out bounded", p)
				}
			}
		}
	}
}

// TestBV235_AcceptedKeepsCounting (R3): an accepted finding stays among the
// scored findings, with its date.
func TestBV235_AcceptedKeepsCounting(t *testing.T) {
	res := kindsEval(t, knowledge.ClaudeCodeProduct, true, nil, modeCap("auto", true))
	f, ok := mcpFindingBy(res, "Stripe")
	if !ok || f.Accepted != nil {
		t.Fatalf("stripe finding %v, accepted %v", ok, f.Accepted)
	}
	res = kindsEval(t, knowledge.ClaudeCodeProduct, true, []config.AllowEntryAccepted{{ID: f.ID, Date: "2026-11-14"}}, modeCap("auto", true))
	g, ok := mcpFindingBy(res, "Stripe")
	if !ok || g.Accepted == nil || g.Accepted.Date != "2026-11-14" {
		t.Fatalf("accepted finding: present %v, accepted %+v", ok, g.Accepted)
	}
	if len(res.Dismissed) != 0 {
		t.Errorf("an accepted finding is not dismissed: %d dismissed", len(res.Dismissed))
	}
}
