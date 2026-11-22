package graph

import (
	"sort"
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// The OpenCode family in the graph (BV-276): OpenCode and Kilo Code. Each
// permission category's effective action is worked out per scope, as the
// product merges its files: the product default first, then the user's
// files, a project's files (only in that project) and the managed files,
// each overriding the one before; inside one file, "*" first and then the
// tool's own key. One capability per category and scope:
//   - the user scope (applies_in user), which every project gets unless
//     its own files change the category;
//   - a project scope (applies_in project:<root>) where they do.
// A category no file sets is the product default (basis product_default,
// source kb:<product>/<entry>@<version>); one a file sets is configured,
// with the setting as its rule and the deciding file as its source.

// openCodeCategory is one permission category and the capability it is.
type openCodeCategory struct {
	key, capType, scope, entry string
	keys                       []string // the permission keys that set it
}

// openCodeCategories are the categories RosmarOps scores. read and edit are
// the project's files; external_directory is everything outside it; bash
// runs commands; webfetch and websearch reach the network; "*" covers the
// MCP tools.
var openCodeCategories = []openCodeCategory{
	{"read", CapReadFiles, openCodeScopeWork, "read_workspace", []string{"read"}},
	{"external_read", CapReadFiles, scopeAll, "read_outside_ask", []string{"external_directory"}},
	{"edit", CapWriteFiles, openCodeScopeWork, "write_workspace", []string{"edit", "write", "patch"}},
	{"bash", CapExecuteShell, scopeAll, "", []string{"bash"}},
	{"network", CapNetwork, "any_destination", "network_allow", []string{"webfetch", "websearch"}},
	{"mcp", CapInvokeMCPTool, scopeAll, "mcp_allow", nil},
}

const openCodeScopeWork = "workspace"

// openCodeDecision is a category's effective action and where it came from.
type openCodeDecision struct {
	action, rule, source string
	configured           bool
}

// openCodeDefaults are a product's factory actions per category, from its
// knowledge base.
func openCodeDefaults(product string, base knowledge.Base) map[string]openCodeDecision {
	out := map[string]openCodeDecision{}
	byType := func(id string) (knowledge.Entry, bool) {
		for _, e := range base.Entries {
			if e.ID == id {
				return e, true
			}
		}
		return knowledge.Entry{}, false
	}
	for _, c := range openCodeCategories {
		id := c.entry
		if c.key == "bash" {
			id = "shell_allow"
			if _, ok := byType(id); !ok {
				id = "shell_ask"
			}
		}
		if e, ok := byType(id); ok {
			out[c.key] = openCodeDecision{action: e.Approval, source: "kb:" + product + "/" + e.ID + "@" + base.Version}
		}
	}
	return out
}

// applyOpenCodeLayer is a file's permission over the decisions so far.
func applyOpenCodeLayer(dec map[string]openCodeDecision, l platform.OpenCodeLayer) {
	set := func(cat, key, action string) {
		dec[cat] = openCodeDecision{action: action, rule: "permission." + key + ": " + action, source: l.Path + "#/permission/" + key, configured: true}
	}
	if r, ok := l.Permission["*"]; ok && r.Default() != "" {
		for _, c := range openCodeCategories {
			set(c.key, "*", r.Default())
		}
	}
	for _, c := range openCodeCategories {
		for _, k := range c.keys {
			if r, ok := l.Permission[k]; ok && r.Default() != "" {
				set(c.key, k, r.Default())
			}
		}
	}
}

// OpenCodeDecisions are the effective decisions of the user scope and of
// each project root whose own files change one: lowest layer first, the
// managed files last.
func OpenCodeDecisions(scan platform.OpenCodeScan, base knowledge.Base) (user map[string]openCodeDecision, projects map[string]map[string]openCodeDecision) {
	read := func(kind string, root string) []platform.OpenCodeLayer {
		var out []platform.OpenCodeLayer
		for _, l := range scan.Layers {
			if l.Status == platform.MCPSourceRead && l.Kind == kind && (root == "" || canonicalPath(l.Root) == root) {
				out = append(out, l)
			}
		}
		return out
	}
	build := func(root string) map[string]openCodeDecision {
		dec := openCodeDefaults(scan.Product, base)
		layers := read(platform.OpenCodeLayerUser, "")
		if root != "" {
			layers = append(layers, read(platform.OpenCodeLayerProject, root)...)
		}
		layers = append(layers, read(platform.OpenCodeLayerManaged, "")...)
		for _, l := range layers {
			applyOpenCodeLayer(dec, l)
		}
		return dec
	}
	user = build("")
	projects = map[string]map[string]openCodeDecision{}
	for _, l := range scan.Layers {
		if l.Kind != platform.OpenCodeLayerProject || l.Status != platform.MCPSourceRead || len(l.Permission) == 0 {
			continue
		}
		root := canonicalPath(l.Root)
		if _, done := projects[root]; !done {
			projects[root] = build(root)
		}
	}
	return user, projects
}

// FromOpenCode builds an OpenCode-family product's fragment: its ai_client
// when present, an agent_config resource per file read, and one capability
// per category and scope. Its MCP servers come through the shared pipeline
// (OpenCodeMCPServers). It is a pure function.
func FromOpenCode(scan platform.OpenCodeScan, base knowledge.Base, discoveredAt time.Time) Fragment {
	b := newFragmentBuilder()
	if !scan.Present {
		return b.fragment()
	}
	seen := discoveredAt.UTC().Format(time.RFC3339)
	client := clientID(scan.Product, scan.ConfigDir)
	b.node(Node{
		ID: client, Kind: KindAgent, Type: AgentAIClient, Name: clientDisplayName(scan.Product),
		Platform: scan.Product, ConfigPath: scan.ConfigDir, DiscoveredAt: seen,
	})
	for _, l := range scan.Layers {
		switch l.Status {
		case platform.MCPSourceFailed:
			b.failed = append(b.failed, SourceFailed{Path: l.Path, Reason: l.Reason})
			continue
		case platform.MCPSourceMissing:
			continue
		}
		b.read[canonicalPath(l.Path)] = l.Path
		b.node(Node{ID: configResourceID(l.Path), Kind: KindResource, Type: ResourceAgentConfig, Path: l.Path, Configures: client, DiscoveredAt: seen})
	}
	user, projects := OpenCodeDecisions(scan, base)
	emit := func(c openCodeCategory, d openCodeDecision, appliesIn string) {
		basis, rule := BasisProductDefault, ""
		if d.configured {
			basis, rule = BasisConfigured, d.rule
		}
		id := "cap_" + keyHash(KindCapability, basis, client, c.capType, c.scope, appliesIn, d.source)
		b.node(Node{
			ID: id, Kind: KindCapability, Type: c.capType, Basis: basis, InForce: true,
			Approval: d.action, TargetScope: c.scope, AppliesIn: appliesIn, Rule: rule, Source: d.source,
		})
		b.edge(Edge{ID: edgeID(EdgeHas, client, id), Type: EdgeHas, From: client, To: id, Source: d.source})
		// Commands that run without asking read the whole computer
		// (BV-245, b-i): the terminal-read capability, also from the factory
		// default (graph.v2 allows a rule on a product_default capability
		// whose source is kb:, BV-276). OpenCode asks before the file
		// commands it recognises when their paths leave the project
		// (external_directory, tool/shell.ts:262-279, 392-403 at v1.19.6),
		// but any other program reads anywhere without asking.
		if c.key == "bash" && d.action == ApprovalAllow {
			tid := "cap_" + keyHash(KindCapability, basis, client, CapReadFiles, terminalReadScope, appliesIn, d.source)
			trule := TerminalReadRulePrefix + "bash: allow"
			if d.configured {
				trule = TerminalReadRulePrefix + d.rule
			}
			b.node(Node{
				ID: tid, Kind: KindCapability, Type: CapReadFiles, Basis: basis, InForce: true,
				Approval: ApprovalAllow, TargetScope: terminalReadScope, AppliesIn: appliesIn, Rule: trule, Source: d.source,
			})
			b.edge(Edge{ID: edgeID(EdgeHas, client, tid), Type: EdgeHas, From: client, To: tid, Source: d.source})
		}
	}
	for _, c := range openCodeCategories {
		emit(c, user[c.key], platform.MCPScopeUser)
	}
	roots := make([]string, 0, len(projects))
	for r := range projects {
		roots = append(roots, r)
	}
	sort.Strings(roots)
	for _, r := range roots {
		for _, c := range openCodeCategories {
			if d := projects[r][c.key]; d != user[c.key] {
				emit(c, d, platform.MCPScopeProject+":"+r)
			}
		}
	}
	return b.fragment()
}

// OpenCodeMCPServers are the scan's MCP servers for the shared pipeline.
func OpenCodeMCPServers(scan platform.OpenCodeScan) []platform.MCPServer {
	if !scan.Present {
		return nil
	}
	return scan.Servers
}

// openCodeInference is an OpenCode-family product's reaches and writes: per
// repository root, the project scope's capability where there is one, else
// the user scope's; reading the project reaches its credentials, reading
// outside it or the terminal reaches every credential, and editing the
// project writes the configuration of the agents inside it.
type openCodeInference struct {
	client Node
	roots  []string
	caps   []Node
}

func (p prepared) openCodeInference(client Node, repoRoots []string) openCodeInference {
	return openCodeInference{client: client, roots: append([]string{}, repoRoots...), caps: p.caps[client.ID]}
}

// capFor is the in-force capability of a type and target scope that
// applies in root ("" for the user scope).
func (in openCodeInference) capFor(typ, scope, root string) *Node {
	var user *Node
	for i := range in.caps {
		c := &in.caps[i]
		if !c.InForce || c.Type != typ || c.TargetScope != scope {
			continue
		}
		if root != "" && c.AppliesIn == platform.MCPScopeProject+":"+canonicalPath(root) {
			return c
		}
		if c.AppliesIn == platform.MCPScopeUser {
			user = c
		}
	}
	return user
}

func allowed(c *Node) bool { return c != nil && c.Approval == ApprovalAllow }

func (in openCodeInference) reaches(all []Node) []Edge {
	var out []Edge
	everywhere := in.capFor(CapReadFiles, scopeAll, "")
	terminal := in.capFor(CapReadFiles, terminalReadScope, "")
	for _, cred := range all {
		if cred.Kind != KindCredential {
			continue
		}
		switch {
		case allowed(terminal):
			out = append(out, in.edge(EdgeReaches, cred.ID, *terminal))
			continue
		case allowed(everywhere):
			out = append(out, in.edge(EdgeReaches, cred.ID, *everywhere))
			continue
		}
		for _, r := range in.roots {
			if c := in.capFor(CapReadFiles, openCodeScopeWork, r); allowed(c) && in.credUnder(cred, r) {
				out = append(out, in.edge(EdgeReaches, cred.ID, *c))
				break
			}
			if t := in.capFor(CapReadFiles, terminalReadScope, r); allowed(t) {
				out = append(out, in.edge(EdgeReaches, cred.ID, *t))
				break
			}
		}
	}
	return out
}

func (in openCodeInference) credUnder(cred Node, root string) bool {
	for _, loc := range cred.Locations {
		if within(root, ParseLocation(loc)) {
			return true
		}
	}
	return false
}

func (in openCodeInference) writes(all []Node) []Edge {
	var out []Edge
	for _, res := range all {
		if res.Kind != KindResource || !isAgentResource(res.Type) {
			continue
		}
		file, ok := resourceFile(res.Path)
		if !ok {
			continue
		}
		for _, r := range in.roots {
			if !within(r, file) {
				continue
			}
			if c := in.capFor(CapWriteFiles, openCodeScopeWork, r); allowed(c) {
				out = append(out, in.edge(EdgeWritesConfigOf, res.ID, *c))
			}
			break
		}
	}
	return out
}

func (in openCodeInference) edge(typ, to string, capNode Node) Edge {
	e := Edge{
		ID:          "edge_" + typ + "_" + keyHash(typ, in.client.ID, to, capNode.ID),
		Type:        typ,
		From:        in.client.ID,
		To:          to,
		Source:      capNode.Source,
		Certainty:   CertaintyInferred,
		DerivedFrom: []string{in.client.ID, capNode.ID, to},
		Assumptions: baseAssumptions(AssumeLaunchFlagsNotReadable),
	}
	if typ == EdgeReaches {
		e.Via = capNode.ID
	}
	return e
}
