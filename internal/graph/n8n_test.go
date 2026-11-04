package graph

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/intel"
	"github.com/qtorb/rosmarops/internal/platform"
)

func TestN8NSignal(t *testing.T) {
	cases := []struct {
		name     string
		srv      platform.MCPServer
		wantSig  string // "" means no match
		wantHost string
	}{
		// (a) instance MCP endpoint path on the person's own host.
		{"endpoint own host", platform.MCPServer{URL: "https://n8n.mycompany.com/mcp-server/http"}, "endpoint", "n8n.mycompany.com"},
		{"endpoint via mcp-remote arg", platform.MCPServer{Command: "npx", Args: []string{"-y", "mcp-remote", "https://ops.example.org/mcp-server/http"}}, "endpoint", "ops.example.org"},
		{"endpoint via supergateway arg", platform.MCPServer{Command: "supergateway", Args: []string{"https://ops.example.org/mcp-server/http"}}, "endpoint", "ops.example.org"},
		// (b) n8n Cloud host.
		{"cloud host", platform.MCPServer{URL: "https://acme.app.n8n.cloud/mcp-test/9f/sse"}, "cloud", "acme.app.n8n.cloud"},
		{"cloud host short", platform.MCPServer{URL: "https://acme.n8n.cloud/anything"}, "cloud", "acme.n8n.cloud"},
		// (c) local 5678.
		{"local 5678", platform.MCPServer{URL: "http://localhost:5678/mcp/abc/sse"}, "port", "localhost"},
		{"loopback 5678", platform.MCPServer{URL: "http://127.0.0.1:5678/mcp-test/9f/sse"}, "port", "127.0.0.1"},
		// (d) package/command.
		{"npx n8n package", platform.MCPServer{Command: "npx", Args: []string{"-y", "n8n"}}, "package", ""},
		{"bare command n8n", platform.MCPServer{Command: "/usr/local/bin/n8n"}, "package", ""},
		// False positives: 5678 on a REMOTE host is not n8n; a random remote MCP
		// server is not n8n; /mcp-test/ alone (no cloud host, no local port) is not.
		{"remote 5678 not n8n", platform.MCPServer{URL: "https://api.example.com:5678/mcp"}, "", ""},
		{"unrelated remote", platform.MCPServer{URL: "https://api.github.com/mcp"}, "", ""},
		{"mcp-test alone not n8n", platform.MCPServer{URL: "https://mcp.example.com/mcp-test/9f/sse"}, "", ""},
		{"unrelated stdio", platform.MCPServer{Command: "npx", Args: []string{"-y", "@modelcontextprotocol/server-github"}}, "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sig, host, ok := n8nSignal(c.srv)
			if c.wantSig == "" {
				if ok {
					t.Fatalf("want no match, got %q host=%q", sig, host)
				}
				return
			}
			if !ok || sig != c.wantSig || host != c.wantHost {
				t.Fatalf("want %q/%q, got %q/%q (ok=%v)", c.wantSig, c.wantHost, sig, host, ok)
			}
		})
	}
}

// TestN8NSignal_HostOnly: the host carried from a matched URL never includes
// the path, the query or any token embedded in them.
func TestN8NSignal_HostOnly(t *testing.T) {
	const token = "SECRET-BEARER-TOKEN-NEVER-LEAK-abcdef"
	srv := platform.MCPServer{URL: "https://acme.app.n8n.cloud/mcp-server/http?access_token=" + token}
	sig, host, ok := n8nSignal(srv)
	if !ok || host != "acme.app.n8n.cloud" {
		t.Fatalf("want match with clean host, got sig=%q host=%q ok=%v", sig, host, ok)
	}
	for _, bad := range []string{token, "mcp-server", "access_token", "?"} {
		if strings.Contains(host, bad) {
			t.Errorf("host leaked %q: %q", bad, host)
		}
	}
}

func TestCrossN8N_MarksNode(t *testing.T) {
	now := time.Now()
	servers := []platform.MCPServer{
		{Client: "claude-code", Name: "n8n", Command: "npx", Args: []string{"n8n"}, ConfigPath: "/c/.claude.json", ConfigDir: "/c", LoadsMCP: true, Scope: platform.MCPScopeUser},
		{Client: "claude-code", Name: "github", Command: "npx", Args: []string{"@modelcontextprotocol/server-github"}, ConfigPath: "/c/.claude.json", ConfigDir: "/c", LoadsMCP: true, Scope: platform.MCPScopeUser},
	}
	frag := FromMCP([]ClientSource{{Platform: "claude-code", ConfigPath: "/c/.claude.json", ConfigDir: "/c"}}, servers, now)
	g := Graph{Nodes: frag.Nodes, Edges: frag.Edges}
	CrossN8N(&g, servers, now)

	byID := map[string]Node{}
	for _, n := range g.Nodes {
		byID[n.ID] = n
	}
	n8n := byID[serverID(servers[0])]
	if n8n.N8N == nil || n8n.N8N.Signal != "package" {
		t.Fatalf("n8n server not marked: %+v", n8n.N8N)
	}
	gh := byID[serverID(servers[1])]
	if gh.N8N != nil {
		t.Fatalf("github server should not be marked n8n: %+v", gh.N8N)
	}
}

func TestAddN8NKeyNode(t *testing.T) {
	g := Graph{}
	AddN8NKeyNode(&g, "/home/me/.n8n", time.Now())
	if len(g.Nodes) != 1 {
		t.Fatalf("want 1 node, got %d", len(g.Nodes))
	}
	n := g.Nodes[0]
	if !n.N8NKey || n.Kind != KindResource || n.Type != ResourceFile || n.Path != "/home/me/.n8n" {
		t.Fatalf("unexpected node: %+v", n)
	}
	// Idempotent: a second call on the same folder does not add a duplicate.
	AddN8NKeyNode(&g, "/home/me/.n8n", time.Now())
	if len(g.Nodes) != 1 {
		t.Fatalf("second call added a duplicate: %d nodes", len(g.Nodes))
	}
	// An empty path is a no-op.
	AddN8NKeyNode(&g, "", time.Now())
	if len(g.Nodes) != 1 {
		t.Fatalf("empty path added a node: %d", len(g.Nodes))
	}
}

// TestN8NMaliciousNode checks that a community node package from the n8n folder
// is crossed against the threat data like any global npm package (BV-175 point
// 3): a matching release marks a file resource node, which the rules layer
// turns into package_known_malicious with no new text.
func TestN8NMaliciousNode(t *testing.T) {
	now := time.Now()
	m := intel.NewMatcher(&intel.Snapshot{
		Version: "t", PublishedAt: "2026-11-04T00:00:00Z",
		Packages: []intel.Package{{ID: "BV-TEST-1", Ecosystem: intel.EcosystemNPM, Package: "n8n-nodes-evil", Versions: []string{"1.0.0"}}},
	})
	path := "/home/me/.n8n/nodes/node_modules/n8n-nodes-evil/package.json"
	g := Graph{}
	CrossIntel(&g, nil, []PackageObservation{
		{Ecosystem: intel.EcosystemNPM, Name: "n8n-nodes-evil", Version: "1.0.0", Path: path},
	}, m, now)
	var found *Node
	for i := range g.Nodes {
		if g.Nodes[i].Path == path {
			found = &g.Nodes[i]
		}
	}
	if found == nil || found.Malicious == nil || found.Malicious.IntelID != "BV-TEST-1" {
		t.Fatalf("n8n node not marked malicious: %+v", found)
	}
}
