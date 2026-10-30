package report_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/report"
)

// mcp_external_action end to end (BV-117/BV-121): the services list reaches the
// report.v2 JSON (schema valid) and the HTML, with read-only and blocked
// servers listed, servers outside the catalog excluded, and no URL on the
// service name (no_external).

// mcpV2 builds a V2 whose one Claude Code agent loads the given servers.
func mcpV2(t *testing.T, srvs []struct {
	name, service, category, rule string
	readOnly                      bool
},
) report.V2 {
	t.Helper()
	client := "agent_client_cc"
	nodes := []graph.Node{{
		ID: client, Kind: graph.KindAgent, Type: graph.AgentAIClient,
		Name: "Claude Code", Platform: knowledge.ClaudeCodeProduct, ConfigPath: "/home/a/.claude", DiscoveredAt: v2Now.UTC().Format("2006-01-02T15:04:05Z07:00"),
	}}
	var edges []graph.Edge
	for _, s := range srvs {
		sid := "mcp_" + s.name
		var ea *graph.ExternalActionMatch
		if s.service != "" {
			ea = &graph.ExternalActionMatch{ID: s.name, Service: s.service, Category: s.category, Writes: true, ReadOnly: s.readOnly}
		}
		nodes = append(nodes, graph.Node{
			ID: sid, Kind: graph.KindAgent, Type: graph.AgentMCPServer, Name: s.name,
			Platform: knowledge.ClaudeCodeProduct, ConfigPath: "/home/a/.claude.json", ExternalAction: ea,
		})
		edges = append(edges, graph.Edge{ID: "loads_" + s.name, Type: graph.EdgeLoads, From: client, To: sid, Source: "/home/a/.claude.json", Certainty: graph.CertaintyObserved, AppliesIn: "user"})
		if s.rule != "" {
			cid := "cap_" + s.name
			ap := graph.ApprovalAllow
			if s.rule == "deny" {
				ap = graph.ApprovalDeny
			}
			nodes = append(nodes, graph.Node{ID: cid, Kind: graph.KindCapability, Type: graph.CapInvokeMCPTool, Approval: ap, TargetScope: "mcp__" + s.name, InForce: true, Basis: graph.BasisConfigured})
			edges = append(edges, graph.Edge{ID: "has_" + s.name, Type: graph.EdgeHas, From: client, To: cid})
		}
	}
	g := graph.Graph{Nodes: nodes, Edges: edges}
	return buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
}

var mcpScenario = []struct {
	name, service, category, rule string
	readOnly                      bool
}{
	{"stripe", "Stripe", "money", "allow", false},     // nobody → red money, has finding
	{"github", "GitHub", "code_repo", "deny", false},  // blocked → no finding, listed
	{"gmail", "Gmail", "email_drafts", "allow", true}, // read-only → no finding, listed
	{"local", "", "", "", false},                      // not in the catalog → excluded
}

func TestMCPExternal_JSONValidatesWithServices(t *testing.T) {
	v := mcpV2(t, mcpScenario)
	// RenderV2JSON validates against the report.v2 schema before returning.
	js, err := report.RenderV2JSON(v)
	if err != nil {
		t.Fatal(err)
	}
	s := string(js)
	for _, want := range []string{`"services"`, `"service": "Stripe"`, `"service": "GitHub"`, `"service": "Gmail"`, `"approval": "deny"`, `"access": "read_only"`} {
		if !strings.Contains(s, want) {
			t.Errorf("report.v2 JSON lacks %s", want)
		}
	}
	if strings.Contains(s, `"service": "local"`) {
		t.Error("a server outside the catalog reached the services list")
	}
}

// BV-124: the «what it means» text (why.<category>) renders in the report
// below the title of an mcp_external_action finding, like other rules' why,
// and the action is shown too.
func TestMCPExternal_WhyAndAction(t *testing.T) {
	v := mcpV2(t, []struct {
		name, service, category, rule string
		readOnly                      bool
	}{{"stripe", "Stripe", "money", "allow", false}}) // nobody → red money, is today
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		p := map[string]string{"agent": "Claude Code", "service": "Stripe"}
		why := cat.Text("finding.mcp_external_action.why.money", p)
		action := cat.Text("finding.mcp_external_action.action", p)
		if !strings.Contains(page, htmlEscape(why)) {
			t.Errorf("%s: the money «what it means» text is not in the report", lang)
		}
		if !strings.Contains(page, htmlEscape(action)) {
			t.Errorf("%s: the «what to do» action is not in the report", lang)
		}
	}
}

func TestMCPExternal_HTMLServices(t *testing.T) {
	v := mcpV2(t, mcpScenario)
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		if !strings.Contains(page, htmlEscape(cat.Text("report.services_h", nil))) {
			t.Errorf("%s: no «What it's connected to» header", lang)
		}
		// The three catalog servers appear; the non-catalog one does not.
		for _, svc := range []string{"Stripe", "GitHub", "Gmail"} {
			if !strings.Contains(page, svc) {
				t.Errorf("%s: service %s missing from HTML", lang, svc)
			}
		}
		// The service lines carry the category and approval texts, read-only
		// shown as the approval.
		for _, want := range []string{cat.Text("category.money", nil), cat.Text("approval.nobody", nil), cat.Text("approval.deny", nil), cat.Text("approval.read_only", nil)} {
			if !strings.Contains(page, htmlEscape(want)) {
				t.Errorf("%s: HTML lacks service text %q", lang, want)
			}
		}
		// no_external: a service <li> carries no URL.
		for _, li := range regexp.MustCompile(`<li>(.*?)</li>`).FindAllStringSubmatch(page, -1) {
			if strings.Contains(li[1], "http://") || strings.Contains(li[1], "https://") {
				t.Errorf("%s: a service line carries a URL: %q", lang, li[1])
			}
		}
	}
}
