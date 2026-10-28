package graph

import (
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/intel"
	"github.com/qtorb/rosmarops/internal/platform"
)

// Threat-intel cross (BV-100, F0a): a local, no-network pass that marks graph
// nodes whose package coordinates match the embedded threat-data snapshot.
// For MCP servers the coordinate is parsed from Command+Args in memory (the
// arguments are never written anywhere); for Python and npm packages the
// caller passes coordinates it read at rest. A match is carried on the node's
// in-memory Malicious field; the rules layer emits package_known_malicious.

// PackageObservation is a package read at rest to cross: its ecosystem, name,
// exact version, and the path of the resource that represents it (the
// *.dist-info folder, the artifact, or the global package.json). Only pinned
// versions are crossed.
type PackageObservation struct {
	Ecosystem string
	Name      string
	Version   string
	Path      string
}

// CrossIntel marks graph nodes that match the snapshot. It mutates g in place
// and is a no-op when m is nil. It appends one file resource node per matching
// package observation (deduplicated by node id) and sets Malicious on the
// mcp_server node of every server whose invocation names a malicious release.
func CrossIntel(g *Graph, servers []platform.MCPServer, pkgs []PackageObservation, m *intel.Matcher, discoveredAt time.Time) {
	if g == nil || m == nil {
		return
	}
	seen := discoveredAt.UTC().Format(time.RFC3339)

	// MCP: match from Command+Args, set the match on the existing mcp_server node.
	byID := make(map[string]int, len(g.Nodes))
	for i, n := range g.Nodes {
		byID[n.ID] = i
	}
	for _, srv := range servers {
		idx, ok := byID[serverID(srv)]
		if !ok {
			continue
		}
		for _, c := range mcpCandidates(srv) {
			if mt, ok := m.Match(c.Ecosystem, c.Name, c.Version); ok {
				match := mt
				g.Nodes[idx].Malicious = &match
				break
			}
		}
	}

	// Python and npm packages read at rest: one file resource node per match.
	// byID already maps every node id to its index; reuse it so that a match on
	// a path already in the graph marks that node instead of adding a duplicate.
	for _, p := range pkgs {
		mt, ok := m.Match(p.Ecosystem, p.Name, p.Version)
		if !ok {
			continue
		}
		match := mt
		id := fileResourceID(p.Path)
		if idx, exists := byID[id]; exists {
			g.Nodes[idx].Malicious = &match
			continue
		}
		byID[id] = len(g.Nodes)
		g.Nodes = append(g.Nodes, Node{
			ID: id, Kind: KindResource, Type: ResourceFile, Path: p.Path,
			DiscoveredAt: seen, Malicious: &match,
		})
	}
}

// fileResourceID derives a file resource id from its path.
func fileResourceID(path string) string {
	return "res_file_" + keyHash(ResourceFile, canonicalPath(path))
}

// mcpCandidate is a package coordinate parsed from an MCP invocation.
type mcpCandidate struct {
	Ecosystem string
	Name      string
	Version   string
}

// mcpCandidates parses the package coordinates an MCP server's Command+Args
// name, for the runners that fetch and run a package: npx, bunx, pnpm dlx
// (npm); uvx, pipx run (pypi). Only a pinned <pkg>@<ver> / <pkg>==<ver>
// (including @scope/pkg@ver) yields a candidate; an unpinned invocation does
// not. The arguments are read but never stored.
func mcpCandidates(srv platform.MCPServer) []mcpCandidate {
	cmd := strings.ToLower(strings.TrimSpace(baseName(srv.Command)))
	args := srv.Args

	var ecosystem string
	switch cmd {
	case "npx", "bunx":
		ecosystem = intel.EcosystemNPM
	case "uvx":
		ecosystem = intel.EcosystemPyPI
	case "pnpm":
		if len(args) > 0 && strings.EqualFold(args[0], "dlx") {
			ecosystem, args = intel.EcosystemNPM, args[1:]
		}
	case "pipx":
		if len(args) > 0 && strings.EqualFold(args[0], "run") {
			ecosystem, args = intel.EcosystemPyPI, args[1:]
		}
	}
	if ecosystem == "" {
		return nil
	}

	var out []mcpCandidate
	for _, a := range args {
		if a == "" || strings.HasPrefix(a, "-") {
			continue // a flag (e.g. -y, --yes)
		}
		name, version, ok := splitNameVersion(a)
		if !ok {
			// A bare package name with no pinned version: not a candidate, and
			// no later positional is the package. Stop at the first positional.
			break
		}
		out = append(out, mcpCandidate{Ecosystem: ecosystem, Name: name, Version: version})
		break // the first positional is the package spec
	}
	return out
}

// baseName is the last path element of a command, without directories.
func baseName(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if i := strings.LastIndexAny(cmd, `/\`); i >= 0 {
		cmd = cmd[i+1:]
	}
	return cmd
}

// splitNameVersion splits a package spec into name and pinned version. It
// handles <pkg>==<ver> and <pkg>@<ver>, including @scope/pkg@ver. ok is false
// when there is no pinned version, or the spec looks like a path or URL.
func splitNameVersion(spec string) (name, version string, ok bool) {
	spec = strings.TrimSpace(spec)
	if spec == "" || strings.Contains(spec, "://") || strings.HasPrefix(spec, ".") || strings.HasPrefix(spec, "/") {
		return "", "", false
	}
	if i := strings.Index(spec, "=="); i > 0 {
		name, version = spec[:i], spec[i+2:]
	} else if i := strings.LastIndexByte(spec, '@'); i > 0 { // >0 keeps a leading @scope
		name, version = spec[:i], spec[i+1:]
	} else {
		return "", "", false
	}
	version = strings.TrimSpace(version)
	// A version never holds a path separator; guard against @scope with no ver.
	if name == "" || version == "" || strings.ContainsAny(version, `/\`) {
		return "", "", false
	}
	return name, version, true
}
