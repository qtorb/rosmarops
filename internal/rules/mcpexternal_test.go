package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/rules"
)

// mcp_external_action (BV-117/BV-121): from a scored agent's invoke disposition
// and a catalog server's category, the finding (or none) and the
// agents_overview.services line.

const mcpClient = "agent_client_cc"

// mcpSrv is one catalog MCP server an agent loads, with its invoke rule.
type mcpSrv struct {
	name, service, category string
	readOnly                bool
	rule                    string // "", "allow" or "deny" on mcp__<name>
	scope                   string // applies_in: "user" (default) or "project:..."
}

// mcpEval builds a one-Claude-Code-agent graph that loads the servers and
// evaluates it.
func mcpEval(t *testing.T, srvs []mcpSrv) rules.Result {
	t.Helper()
	seen := now.UTC().Format("2006-01-02T15:04:05Z07:00")
	nodes := []graph.Node{{
		ID: mcpClient, Kind: graph.KindAgent, Type: graph.AgentAIClient,
		Name: "Claude Code", Platform: knowledge.ClaudeCodeProduct, ConfigPath: home + "/.claude", DiscoveredAt: seen,
	}}
	var edges []graph.Edge
	for _, s := range srvs {
		sid := "mcp_" + s.name
		nodes = append(nodes, graph.Node{
			ID: sid, Kind: graph.KindAgent, Type: graph.AgentMCPServer, Name: s.name,
			Platform: knowledge.ClaudeCodeProduct, ConfigPath: home + "/.claude.json", DiscoveredAt: seen,
			ExternalAction: &graph.ExternalActionMatch{ID: s.name, Service: s.service, Category: s.category, Writes: true, ReadOnly: s.readOnly},
		})
		scope := s.scope
		if scope == "" {
			scope = "user"
		}
		edges = append(edges, graph.Edge{
			ID: "loads_" + s.name, Type: graph.EdgeLoads, From: mcpClient, To: sid,
			Source: home + "/.claude.json", Certainty: graph.CertaintyObserved, AppliesIn: scope,
		})
		if s.rule != "" {
			approval := graph.ApprovalAllow
			if s.rule == "deny" {
				approval = graph.ApprovalDeny
			}
			cid := "cap_" + s.name
			nodes = append(nodes, graph.Node{
				ID: cid, Kind: graph.KindCapability, Type: graph.CapInvokeMCPTool,
				Approval: approval, TargetScope: "mcp__" + s.name, InForce: true, Basis: graph.BasisConfigured, DiscoveredAt: seen,
			})
			edges = append(edges, graph.Edge{ID: "has_" + s.name, Type: graph.EdgeHas, From: mcpClient, To: cid})
		}
	}
	g := graph.Graph{Nodes: nodes, Edges: edges}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	return rules.Evaluate(rules.Input{
		Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: []string{home + "/repo"}, Home: home, Now: now,
	})
}

func mcpFindingBy(res rules.Result, service string) (rules.Finding, bool) {
	for _, f := range res.Findings {
		if f.Rule == rules.RuleMCPExternalAction && f.Params.Service == service {
			return f, true
		}
	}
	return rules.Finding{}, false
}

func mcpServices(t *testing.T, res rules.Result) []rules.OverviewService {
	t.Helper()
	for _, a := range res.AgentsOverview {
		if a.AgentID == mcpClient {
			return a.Services
		}
	}
	t.Fatal("no overview for the client")
	return nil
}

// Money without a person is red; asking a person is yellow; both are findings.
func TestMCPExternal_Money(t *testing.T) {
	res := mcpEval(t, []mcpSrv{
		{name: "stripe", service: "Stripe", category: "money", rule: "allow"}, // nobody
		{name: "paypal", service: "PayPal", category: "money"},                // default mode → asks a person
	})
	stripe, ok := mcpFindingBy(res, "Stripe")
	if !ok || stripe.Severity != rules.SeverityRed || stripe.Params.Disposition != rules.MCPApprovalNobody {
		t.Fatalf("stripe %+v ok=%v", stripe, ok)
	}
	paypal, ok := mcpFindingBy(res, "PayPal")
	if !ok || paypal.Severity != rules.SeverityYellow || paypal.Params.Disposition != rules.MCPApprovalPerson {
		t.Fatalf("paypal %+v ok=%v", paypal, ok)
	}
}

// Infrastructure and database are red without a person; messaging and the rest
// are yellow; asking a person gives no finding for any non-money class.
func TestMCPExternal_NonMoney(t *testing.T) {
	res := mcpEval(t, []mcpSrv{
		{name: "vercel", service: "Vercel", category: "infrastructure", rule: "allow"},
		{name: "supabase", service: "Supabase", category: "database", rule: "allow"},
		{name: "slack", service: "Slack", category: "messaging", rule: "allow"},
		{name: "github", service: "GitHub", category: "code_repo", rule: "allow"},
		{name: "asking", service: "Asana", category: "issue_tracker"}, // asks a person → no finding
	})
	for _, c := range []struct {
		service, sev string
	}{{"Vercel", rules.SeverityRed}, {"Supabase", rules.SeverityRed}, {"Slack", rules.SeverityYellow}, {"GitHub", rules.SeverityYellow}} {
		f, ok := mcpFindingBy(res, c.service)
		if !ok || f.Severity != c.sev {
			t.Errorf("%s: %+v ok=%v want %s", c.service, f, ok, c.sev)
		}
	}
	if _, ok := mcpFindingBy(res, "Asana"); ok {
		t.Error("Asana (asking a person, non-money): unexpected finding")
	}
}

// A denied or read-only server gives no finding but still a services line.
func TestMCPExternal_NoFindingButListed(t *testing.T) {
	res := mcpEval(t, []mcpSrv{
		{name: "github", service: "GitHub", category: "code_repo", rule: "deny"},
		{name: "supabase", service: "Supabase", category: "database", readOnly: true, rule: "allow"},
	})
	if _, ok := mcpFindingBy(res, "GitHub"); ok {
		t.Error("denied GitHub: unexpected finding")
	}
	if _, ok := mcpFindingBy(res, "Supabase"); ok {
		t.Error("read-only Supabase: unexpected finding")
	}
	svcs := mcpServices(t, res)
	byName := map[string]rules.OverviewService{}
	for _, s := range svcs {
		byName[s.Service] = s
	}
	if g := byName["GitHub"]; g.Approval != rules.MCPApprovalDeny || g.FindingID != nil || g.Access != "writes" {
		t.Errorf("GitHub service %+v", g)
	}
	if s := byName["Supabase"]; s.Access != "read_only" || s.FindingID != nil {
		t.Errorf("Supabase service %+v", s)
	}
}

// Every catalog server the agent loads is a services line, with its finding id
// when it gave one; the line carries no URL (no_external).
func TestMCPExternal_ServicesComplete(t *testing.T) {
	res := mcpEval(t, []mcpSrv{
		{name: "stripe", service: "Stripe", category: "money", rule: "allow"},
		{name: "github", service: "GitHub", category: "code_repo", rule: "allow"},
	})
	svcs := mcpServices(t, res)
	if len(svcs) != 2 {
		t.Fatalf("%d services, want 2: %+v", len(svcs), svcs)
	}
	for _, s := range svcs {
		if s.FindingID == nil {
			t.Errorf("%s: no finding id", s.Service)
		}
		if s.Scope != "user" {
			t.Errorf("%s: scope %q, want user", s.Service, s.Scope)
		}
	}
}

// A user-level server reaches every project (radius any_project); a
// project-level one does not (same_project).
func TestMCPExternal_Radius(t *testing.T) {
	res := mcpEval(t, []mcpSrv{
		{name: "stripe", service: "Stripe", category: "money", rule: "allow", scope: "user"},
		{name: "square", service: "Square", category: "money", rule: "allow", scope: "project:" + home + "/repo"},
	})
	user, _ := mcpFindingBy(res, "Stripe")
	proj, _ := mcpFindingBy(res, "Square")
	if user.Radius != rules.RadiusAnyProject {
		t.Errorf("user radius %q", user.Radius)
	}
	if proj.Radius != rules.RadiusSameProject {
		t.Errorf("project radius %q", proj.Radius)
	}
}
