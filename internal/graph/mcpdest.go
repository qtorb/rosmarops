package graph

import (
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// Where each MCP server sends data (BV-91, D1/D2 and the E1–E3 amendment).
// net/url is forbidden (a network package), so the URL is parsed by a small
// in-house parser limited to what config files hold. Nothing here does I/O: the
// remote URL and the local program's digest are read by the adapter and passed
// in on platform.MCPServer.

// defaultPorts are the ports omitted from a canonical origin.
var defaultPorts = map[string]string{"http": "80", "https": "443", "ws": "80", "wss": "443"}

// originSchemes are the schemes a remote origin (external_service) may have
// (D1: ^(https?|wss?)://…). A URL with any other scheme is remote with no
// origin node.
var originSchemes = map[string]bool{"http": true, "https": true, "ws": true, "wss": true}

// parsedURL is the structure of a URL, each part as read.
type parsedURL struct {
	scheme   string // lowercased
	userinfo string // without the '@'
	host     string // lowercased; IPv6 keeps its brackets
	port     string // without the ':'
	path     string // including the leading '/'
	query    string // without the '?'
	fragment string // without the '#'
}

// parseMCPURL parses scheme://[userinfo@]host[:port][/path][?query][#fragment],
// with an IPv6 host in brackets. It returns ok=false when there is no
// "scheme://" or no host. It never fails on the rest: a query or fragment is
// taken verbatim.
func parseMCPURL(raw string) (parsedURL, bool) {
	raw = strings.TrimSpace(raw)
	i := strings.Index(raw, "://")
	if i <= 0 {
		return parsedURL{}, false
	}
	u := parsedURL{scheme: strings.ToLower(raw[:i])}
	rest := raw[i+3:]

	// Split off the fragment, then the query, then the path, leaving the
	// authority.
	if h := strings.IndexByte(rest, '#'); h >= 0 {
		u.fragment, rest = rest[h+1:], rest[:h]
	}
	if q := strings.IndexByte(rest, '?'); q >= 0 {
		u.query, rest = rest[q+1:], rest[:q]
	}
	if s := strings.IndexByte(rest, '/'); s >= 0 {
		u.path, rest = rest[s:], rest[:s]
	}
	authority := rest

	// userinfo is before the first '@'.
	if at := strings.IndexByte(authority, '@'); at >= 0 {
		u.userinfo, authority = authority[:at], authority[at+1:]
	}
	// host[:port], with an IPv6 host in brackets.
	if strings.HasPrefix(authority, "[") {
		if end := strings.IndexByte(authority, ']'); end >= 0 {
			u.host = strings.ToLower(authority[:end+1])
			if rem := authority[end+1:]; strings.HasPrefix(rem, ":") {
				u.port = rem[1:]
			}
		} else {
			u.host = strings.ToLower(authority) // malformed; keep as-is
		}
	} else if c := strings.LastIndexByte(authority, ':'); c >= 0 && isAllDigits(authority[c+1:]) {
		u.host, u.port = strings.ToLower(authority[:c]), authority[c+1:]
	} else {
		u.host = strings.ToLower(authority)
	}
	if u.host == "" {
		return parsedURL{}, false
	}
	return u, true
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// canonicalOrigin returns scheme://host[:port] for a URL whose scheme is one of
// http/https/ws/wss and whose host has none of / ? # @ or whitespace (D1's
// pattern). The scheme and host are lowercased and a default port is dropped.
// ok is false for any other URL, which then gets no external_service node.
func canonicalOrigin(u parsedURL) (string, bool) {
	if !originSchemes[u.scheme] || u.host == "" || strings.ContainsAny(u.host, "/?#@ \t\r\n") {
		return "", false
	}
	origin := u.scheme + "://" + u.host
	if u.port != "" && u.port != defaultPorts[u.scheme] {
		origin += ":" + u.port
	}
	return origin, true
}

// normalizedFullURL is the whole URL normalized (scheme and host lowercased,
// default port dropped), with path, query and fragment kept, so that any change
// of destination is seen. The userinfo (user:password@) is dropped: rotating a
// credential embedded in the URL is not a change of destination, the same as
// E2 for env and headers. It never leaves the device in clear: it is only
// HMAC'd into destination_ref.
func normalizedFullURL(u parsedURL) string {
	var b strings.Builder
	b.WriteString(u.scheme)
	b.WriteString("://")
	b.WriteString(u.host)
	if u.port != "" && u.port != defaultPorts[u.scheme] {
		b.WriteByte(':')
		b.WriteString(u.port)
	}
	b.WriteString(u.path)
	if u.query != "" {
		b.WriteByte('?')
		b.WriteString(u.query)
	}
	if u.fragment != "" {
		b.WriteByte('#')
		b.WriteString(u.fragment)
	}
	return b.String()
}

// mcpOriginID derives an external_service id for an MCP origin from its natural
// key: type + "mcp_origin" + origin (E1). It is deliberately distinct from
// serviceResourceID so an origin node is never merged with a credential's
// service node.
func mcpOriginID(origin string) string {
	return "res_mcp_origin_" + keyHash(ResourceExternalService, "mcp_origin", origin)
}

// destinationRef is the salted HMAC of a server's full canonical destination
// (D1 with E1–E3): the normalized full URL for a remote server; the command and
// its arguments and the local program's digest for a stdio server; and, for
// both, the names of secret env/header entries and the names and values of the
// non-secret ones (E2). Rotating a secret does not change it; changing the URL,
// the program, the arguments or a non-secret redirection variable does. Returns
// "" when there is nothing to key (no URL and no command).
func destinationRef(salt []byte, srv platform.MCPServer, patterns []secrets.Pattern) string {
	material := destinationMaterial(srv, patterns)
	if material == "" {
		return ""
	}
	return "hmac-sha256:" + hash.Keyed(salt, []byte(material))
}

// DestinationRef is the destination_ref of srv (see destinationRef), keyed
// with salt, with the built-in secret patterns: the same value the server's
// node carries. The v1 MCP scanner ties allow_list entries to it (decision
// 2026-10-27, blindaje, B9).
func DestinationRef(salt []byte, srv platform.MCPServer) string {
	return destinationRef(salt, srv, secrets.BuiltinPatterns())
}

// destinationMaterial builds the canonical, deterministic byte string that
// destinationRef hashes. Fields are tagged and NUL-separated so no two servers
// collide.
func destinationMaterial(srv platform.MCPServer, patterns []secrets.Pattern) string {
	var b strings.Builder
	if srv.URL != "" {
		if u, ok := parseMCPURL(srv.URL); ok {
			b.WriteString("url\x00" + normalizedFullURL(u) + "\x00")
		} else {
			// Unparseable: the raw URL, so a change still flips the ref.
			b.WriteString("url_raw\x00" + strings.TrimSpace(srv.URL) + "\x00")
		}
	}
	if srv.Command != "" {
		b.WriteString("command\x00" + srv.Command + "\x00")
		for _, a := range srv.Args {
			b.WriteString("arg\x00" + a + "\x00")
		}
	}
	if srv.CommandFileDigest != "" {
		b.WriteString("file\x00" + srv.CommandFileDigest + "\x00")
	}
	writeEnvMaterial(&b, "env", srv.Env, patterns)
	writeEnvMaterial(&b, "hdr", srv.Headers, patterns)
	return b.String()
}

// writeEnvMaterial adds env or header entries to the destination material
// (E2): a secret entry by its key name only; a non-secret one by key and value,
// so changing an API_URL or BASE_URL is a redirection but rotating a key is not.
func writeEnvMaterial(b *strings.Builder, section string, vars []platform.EnvVar, patterns []secrets.Pattern) {
	sorted := append([]platform.EnvVar(nil), vars...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Key < sorted[j].Key })
	for _, v := range sorted {
		if _, _, secret := secrets.ClassifyEnv(v.Key, v.Value, patterns); secret {
			b.WriteString(section + "_name\x00" + v.Key + "\x00")
		} else {
			b.WriteString(section + "\x00" + v.Key + "=" + v.Value.Reveal() + "\x00")
		}
	}
}

// addMCPDestinations enriches the MCP server nodes of f with their transport,
// command and destination_ref, and adds, for every server with a parseable
// remote origin, one external_service node and a sends_to edge to it (BV-91,
// D1/E1). salt keys destination_ref; discoveredAt dates new nodes.
func addMCPDestinations(f Fragment, servers []platform.MCPServer, salt []byte, discoveredAt time.Time) Fragment {
	seen := discoveredAt.UTC().Format(time.RFC3339)
	patterns := secrets.BuiltinPatterns()

	byID := make(map[string]int, len(f.Nodes))
	for i, n := range f.Nodes {
		byID[n.ID] = i
	}
	origins := make(map[string]Node)
	var edges []Edge

	for _, srv := range servers {
		id := serverID(srv)
		idx, ok := byID[id]
		if !ok {
			continue // a server with no node (should not happen)
		}
		f.Nodes[idx].Transport = srv.Transport
		if srv.Command != "" {
			f.Nodes[idx].Command = srv.Command
		}
		f.Nodes[idx].DestinationRef = destinationRef(salt, srv, patterns)

		if srv.URL == "" {
			continue
		}
		u, ok := parseMCPURL(srv.URL)
		if !ok {
			slog.Warn("mcp: could not parse server URL; no origin recorded", "server", srv.Name, "config", srv.ConfigPath)
			continue
		}
		origin, ok := canonicalOrigin(u)
		if !ok {
			slog.Warn("mcp: server URL has no usable origin; none recorded", "server", srv.Name, "scheme", u.scheme, "config", srv.ConfigPath)
			continue
		}
		oid := mcpOriginID(origin)
		if _, ok := origins[oid]; !ok {
			origins[oid] = Node{ID: oid, Kind: KindResource, Type: ResourceExternalService, Path: origin, DiscoveredAt: seen}
		}
		eid := edgeID(EdgeSendsTo, id, oid)
		edges = append(edges, Edge{ID: eid, Type: EdgeSendsTo, From: id, To: oid, Source: srv.ConfigPath, Certainty: CertaintyObserved})
	}

	for _, n := range origins {
		f.Nodes = append(f.Nodes, n)
	}
	f.Edges = append(f.Edges, edges...)
	sort.Slice(f.Nodes, func(i, j int) bool { return f.Nodes[i].ID < f.Nodes[j].ID })
	sort.Slice(f.Edges, func(i, j int) bool { return f.Edges[i].ID < f.Edges[j].ID })
	return f
}
