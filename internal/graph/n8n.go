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
// already reads for every agent:
//
//   - package/command: the invocation runs the `n8n` package (e.g. `npx n8n`),
//     signal "package";
//   - port: a URL (the server url, or a URL argument of a local wrapper) on
//     n8n's fixed default port 5678, signal "port";
//   - endpoint: a URL whose path is the MCP Server Trigger's test endpoint
//     (`…/mcp-test/…`, distinctive to n8n), signal "endpoint".
//
// Port 5678 and the `n8n` package are the reliable signals; the endpoint path
// is a supplementary one. The two n8n MCP endpoints are the instance MCP server
// (https://docs.n8n.io/advanced-ai/accessing-n8n-mcp-server/) and the MCP Server
// Trigger node (https://docs.n8n.io/integrations/builtin/core-nodes/n8n-nodes-langchain.mcptrigger/).
// net/url is forbidden (a network package); URLs are parsed with the in-house
// parseMCPURL (see mcpdest.go). The match is carried on the node's in-memory
// N8N field; the rules layer reads it.

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
		if sig, ok := n8nSignal(srv); ok {
			g.Nodes[idx].N8N = &N8NMatch{Signal: sig}
		}
	}
}

// n8nSignal reports whether an MCP server is an n8n instance and, if so, the
// signal that recognised it.
func n8nSignal(srv platform.MCPServer) (string, bool) {
	if packageInInvocation(srv, "n8n") {
		return "package", true
	}
	for _, raw := range candidateURLs(srv) {
		u, ok := parseMCPURL(raw)
		if !ok {
			continue
		}
		if u.port == "5678" {
			return "port", true
		}
	}
	for _, raw := range candidateURLs(srv) {
		u, ok := parseMCPURL(raw)
		if !ok {
			continue
		}
		if strings.Contains(strings.ToLower(u.path), "/mcp-test/") {
			return "endpoint", true
		}
	}
	return "", false
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
