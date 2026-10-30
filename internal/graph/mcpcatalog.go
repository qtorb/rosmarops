package graph

import (
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// MCP catalog cross (BV-117/BV-121): a local, no-network pass that marks every
// mcp_server node whose invocation matches a known server that acts outside the
// machine (AP-10 Anexo 1). Recognition is from the raw server (URL, command and
// arguments, never persisted): by exact remote URL — in the url field or as an
// argument of a local wrapper such as `npx mcp-remote <url>` — by package or
// image in command/args, or by a URL path suffix (Shopify Checkout). A server
// is read-only (still listed, no finding) with --read-only, read_only=true in
// the URL, a /readonly path, or DISALLOW_WRITES in its environment. A sandbox
// URL never matches. The match is carried on the node's in-memory
// ExternalAction field; the rules layer reads it. net/url is forbidden (a
// network package); URLs are parsed with the in-house parseMCPURL (see
// mcpdest.go).

// CrossMCPCatalog marks the graph's mcp_server nodes against the catalog. It
// mutates g in place and is a no-op when g is nil.
func CrossMCPCatalog(g *Graph, servers []platform.MCPServer, catalog knowledge.MCPCatalog, _ time.Time) {
	if g == nil {
		return
	}
	byID := make(map[string]int, len(g.Nodes))
	for i, n := range g.Nodes {
		byID[n.ID] = i
	}
	sandbox := sandboxURLs(catalog)
	for _, srv := range servers {
		idx, ok := byID[serverID(srv)]
		if !ok {
			continue
		}
		e, readonly, ok := catalogMatch(srv, catalog, sandbox)
		if !ok {
			continue
		}
		g.Nodes[idx].ExternalAction = &ExternalActionMatch{
			ID: e.ID, Service: e.Service, Category: e.Category, Writes: e.Writes, ReadOnly: readonly,
		}
	}
}

// sandboxURLs is the set of normalised sandbox URLs across the catalog: a
// server reaching one of them never matches (no real money moves).
func sandboxURLs(catalog knowledge.MCPCatalog) map[string]bool {
	out := map[string]bool{}
	for _, e := range catalog.Entries {
		for _, u := range e.SandboxURLs {
			if n := normalizeURL(u); n != "" {
				out[n] = true
			}
		}
	}
	return out
}

// catalogMatch returns the catalog entry an MCP server matches, whether the
// local configuration makes it read-only, and whether there was a match.
func catalogMatch(srv platform.MCPServer, catalog knowledge.MCPCatalog, sandbox map[string]bool) (knowledge.MCPCatalogEntry, bool, bool) {
	raw := candidateURLs(srv)
	var urls []string // normalised scheme://host/path, sandbox URLs dropped
	for _, u := range raw {
		n := normalizeURL(u)
		if n != "" && !sandbox[n] {
			urls = append(urls, n)
		}
	}
	readonly := serverReadOnly(srv, raw)
	for _, e := range catalog.Entries {
		if matchEntry(e, srv, urls, raw) {
			return e, readonly, true
		}
	}
	return knowledge.MCPCatalogEntry{}, false, false
}

// matchEntry reports whether a server matches one catalog entry: by exact
// remote URL, by package or image in the invocation, or by URL path suffix.
func matchEntry(e knowledge.MCPCatalogEntry, srv platform.MCPServer, urls, raw []string) bool {
	for _, ru := range e.RemoteURLs {
		n := normalizeURL(ru)
		for _, u := range urls {
			if u == n {
				return true
			}
		}
	}
	for _, p := range e.Packages {
		if packageInInvocation(srv, p) {
			return true
		}
	}
	for _, img := range e.Images {
		if imageInInvocation(srv, img) {
			return true
		}
	}
	if e.URLSuffix != "" {
		for _, u := range raw {
			if p := urlPath(u); p != "" && strings.HasSuffix(p, e.URLSuffix) {
				return true
			}
		}
	}
	return false
}

// candidateURLs are the server's remote URL and every argument that is a URL (a
// local wrapper such as mcp-remote passes the real endpoint as an argument).
func candidateURLs(srv platform.MCPServer) []string {
	var out []string
	if u := strings.TrimSpace(srv.URL); u != "" {
		out = append(out, u)
	}
	for _, a := range srv.Args {
		a = strings.TrimSpace(a)
		if strings.HasPrefix(strings.ToLower(a), "http://") || strings.HasPrefix(strings.ToLower(a), "https://") {
			out = append(out, a)
		}
	}
	return out
}

// normalizeURL returns scheme://host[:port]/path with the scheme and host
// lowercased, a single trailing slash on the path removed, and no query or
// fragment. "" when the string does not parse as an absolute http(s) URL.
func normalizeURL(s string) string {
	u, ok := parseMCPURL(s)
	if !ok {
		return ""
	}
	if u.scheme != "http" && u.scheme != "https" {
		return ""
	}
	out := u.scheme + "://" + u.host
	if u.port != "" {
		out += ":" + u.port
	}
	return out + strings.TrimRight(u.path, "/")
}

// urlPath is a URL's path with a single trailing slash removed; "" when it
// does not parse.
func urlPath(s string) string {
	u, ok := parseMCPURL(s)
	if !ok {
		return ""
	}
	return strings.TrimRight(u.path, "/")
}

// packageInInvocation reports whether the server runs package p: its command's
// base name is p, or an argument is p or p@<version>.
func packageInInvocation(srv platform.MCPServer, p string) bool {
	if strings.EqualFold(baseName(srv.Command), p) {
		return true
	}
	for _, a := range srv.Args {
		a = strings.TrimSpace(a)
		if strings.EqualFold(a, p) || strings.HasPrefix(a, p+"@") {
			return true
		}
	}
	return false
}

// imageInInvocation reports whether the server runs container image img: the
// command or an argument is img or img:<tag>.
func imageInInvocation(srv platform.MCPServer, img string) bool {
	if is(srv.Command, img) {
		return true
	}
	for _, a := range srv.Args {
		if is(a, img) {
			return true
		}
	}
	return false
}

func is(s, img string) bool {
	s = strings.TrimSpace(s)
	return s == img || strings.HasPrefix(s, img+":")
}

// serverReadOnly reports whether the local configuration makes the server
// read-only: --read-only in the arguments, read_only=true in a URL query, a
// /readonly path, or DISALLOW_WRITES in the environment.
func serverReadOnly(srv platform.MCPServer, raw []string) bool {
	for _, a := range srv.Args {
		if strings.EqualFold(strings.TrimSpace(a), "--read-only") {
			return true
		}
	}
	for _, k := range srv.EnvKeys {
		if k == "DISALLOW_WRITES" {
			return true
		}
	}
	for _, u := range raw {
		p, ok := parseMCPURL(u)
		if !ok {
			continue
		}
		if strings.Contains(strings.ToLower(p.path), "/readonly") {
			return true
		}
		if queryParam(p.query, "read_only") == "true" {
			return true
		}
	}
	return false
}

// queryParam returns the value of key in a raw URL query string (a&b=c&…),
// lowercased; "" when it is absent. Enough for the flags the catalog reads; no
// percent-decoding is needed for them.
func queryParam(query, key string) string {
	for _, part := range strings.Split(query, "&") {
		k, v, _ := strings.Cut(part, "=")
		if strings.EqualFold(k, key) {
			return strings.ToLower(v)
		}
	}
	return ""
}
