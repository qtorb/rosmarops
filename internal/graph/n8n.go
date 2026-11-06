package graph

import (
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/platform"
)

// n8n cross (BV-175): a local, no-network pass that marks every mcp_server node
// whose invocation is an n8n instance exposed over MCP. n8n is a workflow tool
// that holds the accounts a person connects to it (email, payments, databases);
// an agent that can run its workflows can use all of them. Recognition is from
// the same raw server (URL, command and arguments, never persisted) RosmarOps
// already reads for every agent. A server is n8n when ANY of these hold
// (advisor 04-11, who read the docs this build's network cannot reach):
//
//   - endpoint: its URL, or a URL in its arguments (including a local wrapper
//     such as `npx mcp-remote <URL>` or `supergateway <URL>`), has a path that
//     ends in `/mcp-server/http` — the instance MCP server's Server URL
//     (https://docs.n8n.io/advanced-ai/accessing-n8n-mcp-server/);
//   - cloud: its host ends in `.app.n8n.cloud` or `.n8n.cloud` (n8n Cloud);
//   - port: its URL is on port 5678 at a local host (localhost, 127.0.0.1 or
//     ::1) — n8n's fixed default port, counted only when the host is local;
//   - package: its command or package is `n8n` (e.g. `npx n8n`).
//
// The MCP Server Trigger node (https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-langchain.mcptrigger/)
// gives Claude Desktop a `npx mcp-remote <URL>` invocation with a random path;
// that path alone is not a signal, so `…/mcp-test/…` counts only together with
// a cloud host or a local port. net/url is forbidden (a network package); URLs
// are parsed with the in-house parseMCPURL (see mcpdest.go). The match, with the
// host of the URL that matched, is carried on the node's in-memory N8N field;
// the rules layer reads it. The path, query, headers and any token are never
// carried.

// CrossN8N marks the graph's mcp_server nodes that are n8n instances. It
// mutates g in place and is a no-op when g is nil.
func CrossN8N(g *Graph, servers []platform.MCPServer, _ time.Time) {
	if g == nil {
		return
	}
	byID := make(map[string]int, len(g.Nodes))
	for i, n := range g.Nodes {
		byID[n.ID] = i
	}
	for _, srv := range servers {
		idx, ok := byID[serverID(srv)]
		if !ok {
			continue
		}
		if sig, host, ok := n8nSignal(srv); ok {
			g.Nodes[idx].N8N = &N8NMatch{Signal: sig, Host: host}
		}
	}
}

// n8nLocalHosts are the hosts at which port 5678 counts as n8n (a local n8n).
var n8nLocalHosts = map[string]bool{
	"localhost": true, "127.0.0.1": true, "::1": true, "[::1]": true,
}

// n8nSignal reports whether an MCP server is an n8n instance, the signal that
// recognised it, and the host of the matching URL (empty for a package/stdio
// match). Only the host is returned, never the path, query or any header.
func n8nSignal(srv platform.MCPServer) (signal, host string, ok bool) {
	// URL-based signals: the server url and any URL argument (a local wrapper
	// such as `mcp-remote` or `supergateway` passes the real endpoint there).
	for _, raw := range candidateURLs(srv) {
		u, ok := parseMCPURL(raw)
		if !ok {
			continue
		}
		if path := strings.TrimRight(strings.ToLower(u.path), "/"); strings.HasSuffix(path, "/mcp-server/http") {
			return "endpoint", u.host, true
		}
		h := u.host
		if strings.HasSuffix(h, ".app.n8n.cloud") || strings.HasSuffix(h, ".n8n.cloud") {
			return "cloud", h, true
		}
		if u.port == "5678" && n8nLocalHosts[h] {
			return "port", h, true
		}
	}
	if packageInInvocation(srv, "n8n") {
		return "package", "", true
	}
	return "", "", false
}

// AddN8NKeyNode appends a file resource node for an n8n user folder that holds
// the database and the credential encryption key together (BV-175), marked
// N8NKey so the rules layer emits n8n_key_with_data. folderPath is the folder
// as read; it is never a key value (invariant 3). It is a no-op on an empty
// path or when the node already exists.
func AddN8NKeyNode(g *Graph, folderPath string, discoveredAt time.Time) {
	if g == nil || strings.TrimSpace(folderPath) == "" {
		return
	}
	id := fileResourceID(folderPath)
	for i, n := range g.Nodes {
		if n.ID == id {
			g.Nodes[i].N8NKey = true
			return
		}
	}
	g.Nodes = append(g.Nodes, Node{
		ID: id, Kind: KindResource, Type: ResourceFile, Path: folderPath,
		DiscoveredAt: discoveredAt.UTC().Format(time.RFC3339), N8NKey: true,
	})
}
