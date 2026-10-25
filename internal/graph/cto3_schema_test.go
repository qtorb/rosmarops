package graph_test

// Schema cases for the CTO-3 changes (decision 2026-10-23, S1–S5, S8, S9),
// run through the embedded schema on the fixture.

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
)

func fixtureEdge(t *testing.T, doc map[string]any, id string) map[string]any {
	t.Helper()
	for _, e := range doc["edges"].([]any) {
		edge := e.(map[string]any)
		if edge["id"] == id {
			return edge
		}
	}
	t.Fatalf("fixture has no edge %q", id)
	return nil
}

const (
	capConfigured = "cap_cursor_shell"
	capDefault    = "cap_claude_code_sandbox_network"
	loadsUnknown  = "edge_claude_code_loads_repo_tools"
	loadsUser     = "edge_cursor_loads_postgres"
)

func TestSchemaCTO3_Rejected(t *testing.T) {
	cases := map[string]func(t *testing.T, doc map[string]any){
		"S1 write_agent_config type": func(t *testing.T, doc map[string]any) {
			fixtureNode(t, doc, "cap_cursor_write")["type"] = "write_agent_config"
		},
		"S2 capability without basis": func(t *testing.T, doc map[string]any) {
			delete(fixtureNode(t, doc, capConfigured), "basis")
		},
		"S2 unknown basis": func(t *testing.T, doc map[string]any) {
			fixtureNode(t, doc, capConfigured)["basis"] = "guessed"
		},
		"S3 capability without in_force": func(t *testing.T, doc map[string]any) {
			delete(fixtureNode(t, doc, capConfigured), "in_force")
		},
		"S3 not_in_force_reason with in_force true": func(t *testing.T, doc map[string]any) {
			fixtureNode(t, doc, capConfigured)["not_in_force_reason"] = "rule_never_matches"
		},
		"S3 in_force false without reason": func(t *testing.T, doc map[string]any) {
			delete(fixtureNode(t, doc, capDefault), "not_in_force_reason")
		},
		"S3 unknown reason": func(t *testing.T, doc map[string]any) {
			fixtureNode(t, doc, capDefault)["not_in_force_reason"] = "because"
		},
		"S4 rule with product_default": func(t *testing.T, doc map[string]any) {
			fixtureNode(t, doc, capDefault)["rule"] = "Bash(*)"
		},
		"S4 configured without rule": func(t *testing.T, doc map[string]any) {
			delete(fixtureNode(t, doc, capConfigured), "rule")
		},
		"S5 loads unknown + observed": func(t *testing.T, doc map[string]any) {
			fixtureEdge(t, doc, loadsUnknown)["certainty"] = "observed"
		},
		"S5 loads pending + observed": func(t *testing.T, doc map[string]any) {
			e := fixtureEdge(t, doc, loadsUnknown)
			e["approval_state"], e["certainty"] = "pending", "observed"
		},
		"S5 unknown approval_state": func(t *testing.T, doc map[string]any) {
			fixtureEdge(t, doc, loadsUnknown)["approval_state"] = "maybe"
		},
		"S8 assumption with spaces": func(t *testing.T, doc map[string]any) {
			fixtureEdge(t, doc, "edge_cursor_reaches_db_servicerole")["assumptions"] = []any{"shell is not sandboxed"}
		},
		"S9 provenance without knowledge_version": func(t *testing.T, doc map[string]any) {
			delete(doc["provenance"].(map[string]any), "knowledge_version")
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeFixtureVariant(t, func(doc map[string]any) { mutate(t, doc) })
			if _, err := graph.LoadGraph(path); err == nil || !strings.Contains(err.Error(), "schema") {
				t.Errorf("got %v, want schema error", err)
			}
		})
	}
}

func TestSchemaCTO3_Accepted(t *testing.T) {
	cases := map[string]func(t *testing.T, doc map[string]any){
		"fixture as is": func(*testing.T, map[string]any) {},
		"S1 invoke_mcp_tool": func(t *testing.T, doc map[string]any) {
			fixtureNode(t, doc, capConfigured)["type"] = "invoke_mcp_tool"
		},
		"S1 permission_mode": func(t *testing.T, doc map[string]any) {
			fixtureNode(t, doc, capConfigured)["type"] = "permission_mode"
		},
		"S3 configured rule not in force, with reason": func(t *testing.T, doc map[string]any) {
			n := fixtureNode(t, doc, capConfigured)
			n["in_force"], n["not_in_force_reason"] = false, "workspace_untrusted"
		},
		"S5 loads approved + observed": func(t *testing.T, doc map[string]any) {
			e := fixtureEdge(t, doc, loadsUnknown)
			e["approval_state"], e["certainty"] = "approved", "observed"
		},
		"S5 loads without applies_in or approval_state": func(t *testing.T, doc map[string]any) {
			delete(fixtureEdge(t, doc, loadsUser), "applies_in")
		},
		"S8 assumption code": func(t *testing.T, doc map[string]any) {
			fixtureEdge(t, doc, "edge_cursor_reaches_db_servicerole")["assumptions"] = []any{"read_deny_partial"}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			path := writeFixtureVariant(t, func(doc map[string]any) { mutate(t, doc) })
			if _, err := graph.LoadGraph(path); err != nil {
				t.Errorf("got %v, want valid", err)
			}
		})
	}
}

// TestEncode_CapabilityAndLoadsFieldsRoundTrip: the new capability and loads
// fields survive decode and encode, and empty optional ones are omitted.
func TestEncode_CapabilityAndLoadsFieldsRoundTrip(t *testing.T) {
	g := loadFixture(t)
	def := nodeByID(t, &g, capDefault)
	if def.Basis != "product_default" || def.InForce || def.NotInForceReason != "sandbox_unsupported_on_platform" || def.Rule != "" {
		t.Errorf("product_default capability decoded as %+v", *def)
	}
	conf := nodeByID(t, &g, capConfigured)
	if conf.Basis != "configured" || !conf.InForce || conf.Rule != "Shell(*)" || conf.NotInForceReason != "" {
		t.Errorf("configured capability decoded as %+v", *conf)
	}
	e := edgeByID(t, &g, loadsUnknown)
	if e.AppliesIn != "project:/src/app" || e.ApprovalState != "unknown" || e.Certainty != "inferred" {
		t.Errorf("loads edge decoded as %+v", *e)
	}
	if g.Provenance.KnowledgeVersion != "0" {
		t.Errorf("knowledge_version: %q", g.Provenance.KnowledgeVersion)
	}
	out, err := graph.Encode(g)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for _, absent := range []string{`"not_in_force_reason": ""`, `"rule": ""`, `"approval_state": ""`, `"applies_in": ""`} {
		if strings.Contains(string(out), absent) {
			t.Errorf("encoded graph carries empty optional field %s", absent)
		}
	}
}
