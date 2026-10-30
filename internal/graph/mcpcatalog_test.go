package graph_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// CrossMCPCatalog (BV-117/BV-121): recognition of catalog MCP servers on the
// graph's mcp_server nodes by remote URL, package, image or URL suffix, with
// sandbox and read-only handling, and nothing on servers outside the catalog.

func catalogForTest(t *testing.T) knowledge.MCPCatalog {
	t.Helper()
	c, err := knowledge.LoadMCPCatalog()
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// crossed builds a one-client graph from the servers, runs the catalog pass,
// and returns the ExternalAction of each mcp_server by its name.
func crossed(t *testing.T, servers []platform.MCPServer) map[string]*graph.ExternalActionMatch {
	t.Helper()
	f := graph.FromMCP(nil, servers, scanTime)
	g := graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: "k", HostID: "host_x"}, scanTime)
	graph.CrossMCPCatalog(&g, servers, catalogForTest(t), scanTime)
	out := map[string]*graph.ExternalActionMatch{}
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentMCPServer {
			out[n.Name] = n.ExternalAction
		}
	}
	return out
}

func cc(name, command, url string, args []string) platform.MCPServer {
	return platform.MCPServer{
		Client: "claude-code", Name: name, Command: command, Args: args, URL: url,
		ConfigPath: `C:\Users\a\.claude.json`, ConfigDir: `C:\Users\a\.claude`, Scope: platform.MCPScopeUser,
	}
}

func TestCrossMCPCatalog_Recognition(t *testing.T) {
	servers := []platform.MCPServer{
		cc("stripe", "", "https://mcp.stripe.com", nil),                                 // remote URL
		cc("pay", "npx", "", []string{"@paypal/mcp"}),                                   // package
		cc("gh", "", "", []string{"docker", "run", "ghcr.io/github/github-mcp-server"}), // image in args
		cc("shop", "", "https://shop.example.com/api/ucp/mcp", nil),                     // URL suffix
		cc("lin", "npx", "", []string{"mcp-remote", "https://mcp.linear.app/mcp"}),      // wrapped URL arg
		cc("sbx", "npx", "", []string{"@paypal/mcp-x"}),                                 // not the package
		cc("other", "", "https://example.com/mcp", nil),                                 // outside the catalog
		cc("paysbx", "", "https://mcp.sandbox.paypal.com", nil),                         // sandbox: no match
	}
	got := crossed(t, servers)
	want := map[string]struct {
		id, service, category string
	}{
		"stripe": {"stripe", "Stripe", "money"},
		"pay":    {"paypal", "PayPal", "money"},
		"gh":     {"github", "GitHub", "code_repo"},
		"shop":   {"shopify_checkout", "Shopify Checkout", "money"},
		"lin":    {"linear", "Linear", "issue_tracker"},
	}
	for name, w := range want {
		ea := got[name]
		if ea == nil {
			t.Errorf("%s: no match, want %s", name, w.id)
			continue
		}
		if ea.ID != w.id || ea.Service != w.service || ea.Category != w.category || !ea.Writes || ea.ReadOnly {
			t.Errorf("%s: %+v, want id=%s service=%s cat=%s writes=true readonly=false", name, ea, w.id, w.service, w.category)
		}
	}
	for _, name := range []string{"sbx", "other", "paysbx"} {
		if got[name] != nil {
			t.Errorf("%s: matched %+v, want none", name, got[name])
		}
	}
}

func TestCrossMCPCatalog_ReadOnly(t *testing.T) {
	servers := []platform.MCPServer{
		cc("ghro", "", "", []string{"docker", "run", "ghcr.io/github/github-mcp-server", "--read-only"}), // --read-only
		cc("sqro", "npx", "", []string{"square-mcp-server"}),                                             // DISALLOW_WRITES below
		cc("sbro", "", "https://mcp.supabase.com/mcp?read_only=true", nil),                               // query flag
		cc("linro", "", "https://mcp.linear.app/mcp/readonly", nil),                                      // /readonly path
	}
	servers[1].EnvKeys = []string{"DISALLOW_WRITES"}
	got := crossed(t, servers)
	for _, name := range []string{"ghro", "sqro", "sbro", "linro"} {
		ea := got[name]
		if ea == nil {
			t.Fatalf("%s: no match", name)
		}
		if !ea.ReadOnly {
			t.Errorf("%s: read_only=false, want true (%+v)", name, ea)
		}
	}
}
