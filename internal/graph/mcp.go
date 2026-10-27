package graph

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/platform"
)

// Agent node types produced from MCP configs.
const (
	AgentAIClient  = "ai_client"
	AgentMCPServer = "mcp_server"
	AgentExtension = "extension" // an agent extension of an editor (extensions.go)
)

// clientDisplayNames maps a client product to the name shown in reports.
// Products not listed fall back to the product label itself.
var clientDisplayNames = map[string]string{
	"claude-code": "Claude Code",
	"cline":       "Cline",
	"cursor":      "Cursor",

	// Detected and not evaluated (decision 2026-10-27, agentes nuevos, N1).
	agentdirs.Codex:         "Codex",
	agentdirs.Antigravity:   "Antigravity",
	agentdirs.Hermes:        "Hermes Agent",
	agentdirs.ClaudeDesktop: "Claude Desktop",
}

// clientDisplayName returns the display name for a client product, never
// empty for a non-empty label.
func clientDisplayName(product string) string {
	if name, ok := clientDisplayNames[product]; ok {
		return name
	}
	return product
}

// productOf maps the scanner's client label to the product it belongs to.
// "claude-code-project" is Claude Code read from a repository; the scope is
// recorded in applies_in, not in the platform (decision 2026-10-23, S7).
func productOf(label string) string {
	if label == "claude-code-project" {
		return "claude-code"
	}
	return label
}

// CertaintyObserved marks a fact read as-is from a config file.
const CertaintyObserved = "observed"

// ApprovalUnknown is the approval_state of a loads edge whose server needs an
// approval that RosmarOps cannot read yet (project-scope .mcp.json).
const ApprovalUnknown = "unknown"

// Fragment is a partial graph produced by one collector: nodes, edges and the
// coverage of what that collector read.
type Fragment struct {
	Nodes    []Node
	Edges    []Edge
	Coverage Coverage
}

// ClientSource is an MCP client config file that existed and was read, with
// or without servers in it.
type ClientSource struct {
	// Platform is the scanner's client label: "claude-code",
	// "claude-code-project", "cline" or "cursor".
	Platform string

	// ConfigPath is the path of the config file as read.
	ConfigPath string

	// ConfigDir is the client's configuration folder (platform.MCPSource).
	ConfigDir string
}

// FromMCP builds the agent nodes, agent_config resources and loads edges for
// the MCP servers the platform adapter read. It is a pure function with no
// filesystem or OS access.
//
//   - one ai_client agent per product and configuration folder (decision
//     2026-10-23, S7), whatever the number of files it was read from;
//   - one agent_config resource per config file read, configuring its client;
//   - one mcp_server agent per server;
//   - one loads edge from the product's client to each server, with
//     applies_in set from the server's scope. Project-scope servers
//     (<repo>/.mcp.json) need an approval RosmarOps cannot read yet:
//     approval_state unknown, certainty inferred. Every other loads edge is
//     observed. The adapter has no line numbers, so source is the config path.
func FromMCP(clients []ClientSource, servers []platform.MCPServer, discoveredAt time.Time) Fragment {
	seen := discoveredAt.UTC().Format(time.RFC3339)

	nodes := make(map[string]Node)
	edges := make(map[string]Edge)
	read := make(map[string]string) // canonical path → path as read

	addClient := func(label, configDir, configPath string) string {
		product := productOf(label)
		id := clientID(product, configDir)
		if _, ok := nodes[id]; !ok {
			nodes[id] = Node{
				ID:           id,
				Kind:         KindAgent,
				Type:         AgentAIClient,
				Name:         clientDisplayName(product),
				Platform:     product,
				ConfigPath:   configDir,
				DiscoveredAt: seen,
			}
		}
		res := configResourceID(configPath)
		if _, ok := nodes[res]; !ok {
			nodes[res] = Node{
				ID:           res,
				Kind:         KindResource,
				Type:         ResourceAgentConfig,
				Path:         configPath,
				Configures:   id,
				DiscoveredAt: seen,
			}
		}
		read[canonicalPath(configPath)] = configPath
		return id
	}

	for _, c := range clients {
		addClient(c.Platform, c.ConfigDir, c.ConfigPath)
	}

	for _, srv := range servers {
		from := addClient(srv.Client, srv.ConfigDir, srv.ConfigPath)
		to := serverID(srv)
		if _, ok := nodes[to]; !ok {
			nodes[to] = Node{
				ID:           to,
				Kind:         KindAgent,
				Type:         AgentMCPServer,
				Name:         srv.Name,
				Platform:     productOf(srv.Client),
				ConfigPath:   srv.ConfigPath,
				DiscoveredAt: seen,
			}
		}
		id := edgeID(EdgeLoads, from, to)
		e := Edge{
			ID:        id,
			Type:      EdgeLoads,
			From:      from,
			To:        to,
			Source:    srv.ConfigPath,
			Certainty: CertaintyObserved,
			AppliesIn: appliesIn(srv),
		}
		if srv.Scope == platform.MCPScopeProject {
			e.Certainty, e.ApprovalState = CertaintyInferred, ApprovalUnknown
		}
		edges[id] = e
		// The command of a stdio server is an executable of its client
		// (decision 2026-10-24, memoria, §2.1, the agentic form of
		// mcp_server_user_writable_command): writes_config_of covers
		// another agent replacing it.
		if p, ok := commandPath(srv); ok {
			rid := "res_agent_executable_" + keyHash(ResourceAgentExecutable, canonicalPath(p), from)
			nodes[rid] = Node{ID: rid, Kind: KindResource, Type: ResourceAgentExecutable, Path: p, Configures: from, DiscoveredAt: seen}
		}
	}

	f := Fragment{Coverage: Coverage{
		SourcesRead:       []string{},
		SourcesFailed:     []SourceFailed{},
		AgentsUnsupported: []AgentUnsupported{},
	}}
	for _, n := range nodes {
		f.Nodes = append(f.Nodes, n)
	}
	for _, e := range edges {
		f.Edges = append(f.Edges, e)
	}
	for _, p := range read {
		f.Coverage.SourcesRead = append(f.Coverage.SourcesRead, p)
	}

	// Map iteration order is random; sort so equal input gives equal output.
	sort.Slice(f.Nodes, func(i, j int) bool { return f.Nodes[i].ID < f.Nodes[j].ID })
	sort.Slice(f.Edges, func(i, j int) bool { return f.Edges[i].ID < f.Edges[j].ID })
	sort.Strings(f.Coverage.SourcesRead)
	return f
}

// commandPath is the file a stdio server's command names: an absolute
// path, or one relative to the repository of a project-scope server. A
// bare name (npx, node) is looked up in PATH when the client starts it,
// which cannot be read at rest: no file.
func commandPath(srv platform.MCPServer) (string, bool) {
	c := strings.TrimSpace(srv.Command)
	if c == "" || len(srv.Headers) > 0 || !strings.ContainsAny(c, `/\`) {
		return "", false
	}
	if filepath.IsAbs(c) || isWindowsPath(c) && len(c) > 2 && c[1] == ':' || strings.HasPrefix(c, `\\`) {
		return c, true
	}
	if srv.Scope == platform.MCPScopeProject && srv.ScopePath != "" {
		return filepath.Join(srv.ScopePath, filepath.FromSlash(c)), true
	}
	return "", false
}

// appliesIn is the config layer of a server: "user", "local:<canonical
// project path>" or "project:<canonical repository root>". Empty when the
// adapter set no scope.
func appliesIn(srv platform.MCPServer) string {
	switch srv.Scope {
	case platform.MCPScopeLocal, platform.MCPScopeProject:
		return srv.Scope + ":" + canonicalPath(srv.ScopePath)
	default:
		return srv.Scope
	}
}

// ClientsFromSources splits the adapter's source report into the client
// config files that were read (with or without servers) and the ones that
// exist but could not be read. Missing files are neither: no client is there,
// and nothing was left unread.
func ClientsFromSources(sources []platform.MCPSource) ([]ClientSource, []SourceFailed) {
	var clients []ClientSource
	var failed []SourceFailed
	for _, s := range sources {
		switch s.Status {
		case platform.MCPSourceRead:
			clients = append(clients, ClientSource{Platform: s.Platform, ConfigPath: s.ConfigPath, ConfigDir: s.ConfigDir})
		case platform.MCPSourceFailed:
			failed = append(failed, SourceFailed{Path: s.ConfigPath, Reason: s.Reason})
		}
	}
	return clients, failed
}

// FromMCPScan builds the MCP fragment from a full adapter scan: client and
// server agents with their loads edges, plus credential nodes and receives
// edges for the secrets in each server's env (see CredentialsFromMCP). salt is
// the machine-local salt that keys credential fingerprints; without it no
// fragment is built.
//
// Sources the client does not load MCP servers from (LoadsMCP false, set by
// the adapter) are ignored entirely: they yield no node, no edge and no
// coverage entry.
//
// Coverage records, in sources_failed, every source the adapter could not
// read (read_error, parse_error, size_limit_exceeded), sorted by path so the
// output is deterministic.
func FromMCPScan(scan platform.MCPScan, salt []byte, discoveredAt time.Time) (Fragment, error) {
	return fromMCPScan(scan, salt, discoveredAt, nil, nil)
}

// fromMCPScan is FromMCPScan with extra credential inputs (settings env
// blocks), fingerprinted together with the servers' so that one secret in
// both places is one credential node with every location.
func fromMCPScan(scan platform.MCPScan, salt []byte, discoveredAt time.Time, extra []credentialInput, families map[string]string) (Fragment, error) {
	scan = loadedOnly(scan)
	for _, s := range scan.Sources {
		if s.Status == platform.MCPSourceRead && s.ConfigDir == "" {
			return Fragment{}, fmt.Errorf("mcp source %s: no configuration folder", s.ConfigPath)
		}
	}
	for _, s := range scan.Servers {
		if s.ConfigDir == "" {
			return Fragment{}, fmt.Errorf("mcp server %s in %s: no configuration folder", s.Name, s.ConfigPath)
		}
	}
	creds, receives, err := credentialsFromRepos(append(mcpCredentialInputs(scan.Servers), extra...), families, salt, discoveredAt)
	if err != nil {
		return Fragment{}, err
	}

	clients, failed := ClientsFromSources(scan.Sources)
	f := FromMCP(clients, scan.Servers, discoveredAt)
	f.Nodes = append(f.Nodes, presentClients(f.Nodes, scan.Present, discoveredAt)...)
	f.Coverage.SourcesFailed = append(failed, f.Coverage.SourcesFailed...)
	sort.SliceStable(f.Coverage.SourcesFailed, func(i, j int) bool {
		return f.Coverage.SourcesFailed[i].Path < f.Coverage.SourcesFailed[j].Path
	})

	f.Nodes = append(f.Nodes, creds...)
	f.Edges = append(f.Edges, receives...)
	sort.Slice(f.Nodes, func(i, j int) bool { return f.Nodes[i].ID < f.Nodes[j].ID })
	sort.Slice(f.Edges, func(i, j int) bool { return f.Edges[i].ID < f.Edges[j].ID })
	return f, nil
}

// presentClients are the ai_client nodes of the agents detected by the
// existence of their configuration (platform.AgentPresence) that nodes does
// not hold yet: no agent_config resource, since no file of theirs was read.
func presentClients(nodes []Node, present []platform.AgentPresence, discoveredAt time.Time) []Node {
	seen := discoveredAt.UTC().Format(time.RFC3339)
	have := map[string]bool{}
	for _, n := range nodes {
		have[n.ID] = true
	}
	var out []Node
	for _, p := range present {
		id := clientID(p.Platform, p.ConfigDir)
		if have[id] || p.ConfigDir == "" {
			continue
		}
		have[id] = true
		out = append(out, Node{
			ID: id, Kind: KindAgent, Type: AgentAIClient, Name: clientDisplayName(p.Platform),
			Platform: p.Platform, ConfigPath: p.ConfigDir, DiscoveredAt: seen,
		})
	}
	return out
}

// canonicalPath normalises a config path so that it always produces the same
// node id: backslashes become forward slashes and, for Windows paths (drive
// letter, UNC prefix or any backslash), the path is lowercased because Windows
// paths are case-insensitive. Unix paths keep their case. This is the same
// normalisation the v1 scanner applies in isUserWritableCommand.
func canonicalPath(p string) string {
	windows := strings.Contains(p, `\`) ||
		(len(p) >= 2 && p[1] == ':' && isASCIILetter(p[0]))
	p = strings.ReplaceAll(p, `\`, "/")
	if windows {
		p = strings.ToLower(p)
	}
	return p
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// clientID derives an ai_client id from its natural key:
// type + product + canonical configuration folder.
func clientID(product, configDir string) string {
	return "agent_client_" + keyHash(AgentAIClient, product, canonicalPath(configDir))
}

// configResourceID derives an agent_config resource id from its natural key:
// type + canonical path.
func configResourceID(path string) string {
	return "res_agent_config_" + keyHash(ResourceAgentConfig, canonicalPath(path))
}

// serverID derives an mcp_server id from its natural key:
// type + product + canonical config_path + server name, plus the server's
// JSON Pointer when it is not the default "/mcpServers/<name>". The name keeps
// two servers of one file apart; the pointer keeps apart two local-scope
// servers with the same name in different projects of ~/.claude.json
// ("/projects/<path>/mcpServers/<name>"). Top-level servers omit the pointer,
// so their ids are unchanged from earlier versions.
func serverID(srv platform.MCPServer) string {
	parts := []string{AgentMCPServer, productOf(srv.Client), canonicalPath(srv.ConfigPath), srv.Name}
	if p := serverPointer(srv); p != defaultPointer(srv.Name) {
		parts = append(parts, p)
	}
	return "agent_mcp_" + keyHash(parts...)
}

// serverPointer is the JSON Pointer of a server entry in its config file.
func serverPointer(srv platform.MCPServer) string {
	if srv.Pointer != "" {
		return srv.Pointer
	}
	return defaultPointer(srv.Name)
}

func defaultPointer(name string) string {
	return "/mcpServers/" + pointerEscape(name)
}

// edgeID derives an edge id from its type and endpoints.
func edgeID(edgeType, from, to string) string {
	return "edge_" + edgeType + "_" + keyHash(edgeType, from, to)
}

// keyHash returns the first 16 hex characters of the SHA-256 of the
// NUL-joined key parts.
func keyHash(parts ...string) string {
	return hash.Bytes([]byte(strings.Join(parts, "\x00")))[:16]
}

// loadedOnly keeps only the sources, and the servers, that the client really
// loads MCP servers from (LoadsMCP, set by the adapter per source).
func loadedOnly(scan platform.MCPScan) platform.MCPScan {
	out := platform.MCPScan{Present: scan.Present}
	for _, s := range scan.Sources {
		if s.LoadsMCP {
			out.Sources = append(out.Sources, s)
		}
	}
	for _, s := range scan.Servers {
		if s.LoadsMCP {
			out.Servers = append(out.Servers, s)
		}
	}
	return out
}
