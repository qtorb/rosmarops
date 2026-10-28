package graph

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// Capability types (graph schema, capability.type).
const (
	CapExecuteShell   = "execute_shell"
	CapReadFiles      = "read_files"
	CapWriteFiles     = "write_files"
	CapNetwork        = "network"
	CapInvokeMCPTool  = "invoke_mcp_tool"
	CapPermissionMode = "permission_mode"
)

// Capability bases and approvals.
const (
	BasisConfigured     = "configured"
	BasisProductDefault = "product_default"

	ApprovalAllow = "allow"
	ApprovalAsk   = "ask"
	ApprovalDeny  = "deny"
)

// Reasons a capability is not in force (graph schema, not_in_force_reason).
const (
	ReasonRuleNeverMatches      = "rule_never_matches"
	ReasonModeNotAllowedInScope = "mode_not_allowed_in_scope"
	ReasonWorkspaceUntrusted    = "workspace_untrusted"
	ReasonManagedRulesOnly      = "managed_rules_only"
	ReasonModeDisabled          = "mode_disabled"
)

// Settings vocabulary of Claude Code (https://code.claude.com/docs/en/settings-reference).
const (
	permissionModeAuto           = "auto"
	permissionModeBypass         = "bypassPermissions"
	disableValue                 = "disable"
	scopeAll                     = "*"
	outsideWorkingDirectoryScope = "outside_working_directories"
)

// PermissionStats counts the rules FromClaudeSettings did not turn into a
// capability, so none is dropped silently.
type PermissionStats struct {
	// Unmodeled counts rules per tool name for tools with no capability
	// mapping (decision 2026-10-23, Q3; e.g. MultiEdit, Agent, Cd).
	Unmodeled map[string]int

	// Malformed counts rules that are not "Tool" or "Tool(specifier)".
	Malformed int

	// NestedParentheses counts modeled rules whose specifier contains
	// parentheses. They are kept: parentheses inside a specifier are literal
	// (https://code.claude.com/docs/en/permissions#permission-rule-syntax).
	NestedParentheses int
}

// FromClaudeSettings builds the fragment for Claude Code's permission
// settings: one agent_config resource per settings file read, configuring
// Claude Code's ai_client (created here when no MCP source created it), one
// configured capability per written rule and setting, with a has edge from
// the client. It is a pure function.
//
// Rules are redacted (secrets.RedactMatches) before anything else, so no
// secret reaches the rule, target_scope or id.
func FromClaudeSettings(scan platform.ClaudeSettingsScan, discoveredAt time.Time) (Fragment, PermissionStats) {
	return fromClaudeSettings(scan, resolveManaged(scan.Managed), discoveredAt)
}

// LegacyManagedReason is the coverage detail of the legacy Windows managed
// settings path (https://code.claude.com/docs/en/managed-settings#where-each-mechanism-stores-the-policy).
const LegacyManagedReason = "legacy managed settings path, not read by Claude Code"

func fromClaudeSettings(scan platform.ClaudeSettingsScan, policy managedPolicy, discoveredAt time.Time) (Fragment, PermissionStats) {
	seen := discoveredAt.UTC().Format(time.RFC3339)
	b := newFragmentBuilder()
	stats := PermissionStats{Unmodeled: map[string]int{}}
	patterns := secrets.BuiltinPatterns()

	// Every settings document read is an agent_config of Claude Code,
	// effective or not; failures go to coverage.
	ensure := func(file platform.ClaudeSettingsFile) string {
		client := clientID(knowledge.ClaudeCodeProduct, file.ConfigDir)
		b.node(Node{
			ID: client, Kind: KindAgent, Type: AgentAIClient,
			Name: clientDisplayName(knowledge.ClaudeCodeProduct), Platform: knowledge.ClaudeCodeProduct,
			ConfigPath: file.ConfigDir, DiscoveredAt: seen,
		})
		b.node(Node{
			ID: configResourceID(file.ConfigPath), Kind: KindResource, Type: ResourceAgentConfig,
			Path: file.ConfigPath, Configures: client, DiscoveredAt: seen,
		})
		b.read[canonicalPath(file.ConfigPath)] = file.ConfigPath
		return client
	}
	all := append([]platform.ClaudeSettingsFile{}, scan.Files...)
	for _, src := range scan.Managed {
		all = append(all, src.Files...)
	}
	if scan.ManagedMCP != nil {
		all = append(all, *scan.ManagedMCP)
	}
	var layered []platform.ClaudeSettingsFile
	for _, file := range all {
		switch file.Status {
		case platform.MCPSourceFailed:
			b.failed = append(b.failed, SourceFailed{Path: file.ConfigPath, Reason: file.Reason})
		case platform.MCPSourceRead:
			ensure(file)
		}
	}
	for _, file := range scan.Files {
		if file.Status == platform.MCPSourceRead {
			layered = append(layered, file)
		}
	}
	layered = append(layered, policy.docs...)
	if v := scan.VSCode; v != nil {
		switch {
		case v.Status == platform.MCPSourceFailed:
			b.failed = append(b.failed, SourceFailed{Path: v.Path, Reason: v.Reason})
		case v.Status == platform.MCPSourceRead && v.Mode != "":
			// The extension's starting mode (decision 2026-10-24, D7): a
			// configured capability of layer ide:vscode, which the terminal's
			// effective mode ignores.
			client := ensure(platform.ClaudeSettingsFile{ConfigPath: v.Path, ConfigDir: v.ConfigDir})
			c := capContext{b: b, client: client, file: v.Path, appliesIn: vscodeLayer}
			c.add(Node{Type: CapPermissionMode, Approval: ApprovalAllow, TargetScope: v.Mode, InForce: true},
				"claudeCode.initialPermissionMode="+v.Mode, "/claudeCode.initialPermissionMode", patterns)
		}
	}
	if scan.LegacyManagedPath != "" {
		b.failed = append(b.failed, SourceFailed{Path: scan.LegacyManagedPath, Reason: Reason(ReasonSourceUnsupported, LegacyManagedReason)})
	}

	for _, file := range layered {
		client := ensure(file)
		c := capContext{
			b: b, client: client, file: file.ConfigPath,
			appliesIn:   settingsLayer(file),
			untrusted:   file.Scope == platform.MCPScopeProject && !trusted(scan.Trust, file.ScopePath),
			managedOnly: policy.rulesOnly && file.Scope != platform.MCPScopeManaged,
		}
		for _, ptr := range file.Executables {
			path := file.ConfigPath + "#" + ptr
			b.node(Node{
				ID:   "res_agent_executable_" + keyHash(ResourceAgentExecutable, canonicalPath(file.ConfigPath)+"#"+ptr),
				Kind: KindResource, Type: ResourceAgentExecutable,
				Path: path, Configures: client, DiscoveredAt: seen,
			})
		}
		p := file.Permissions
		for _, list := range []struct {
			name  string
			rules []string
		}{{ApprovalAllow, p.Allow}, {ApprovalAsk, p.Ask}, {ApprovalDeny, p.Deny}} {
			for i, raw := range list.rules {
				c.rule(raw, list.name, i, patterns, &stats)
			}
		}

		if p.DefaultMode != "" {
			cp := Node{Type: CapPermissionMode, Approval: ApprovalAllow, TargetScope: p.DefaultMode, InForce: true}
			projectLayer := file.Scope == platform.MCPScopeProject || file.Scope == platform.MCPScopeLocal
			if projectLayer && (p.DefaultMode == permissionModeAuto || p.DefaultMode == permissionModeBypass) {
				// auto and bypassPermissions do not take effect from project or
				// local settings (settings-reference, permissions.defaultMode).
				cp.InForce, cp.NotInForceReason = false, ReasonModeNotAllowedInScope
			}
			c.add(cp, "defaultMode="+p.DefaultMode, "/permissions/defaultMode", patterns)
		}
		if p.DisableBypassPermissionsMode == disableValue {
			c.add(Node{Type: CapPermissionMode, Approval: ApprovalDeny, TargetScope: permissionModeBypass, InForce: true},
				"disableBypassPermissionsMode=disable", "/permissions/disableBypassPermissionsMode", patterns)
		}
		if p.DisableAutoMode == disableValue {
			c.add(Node{Type: CapPermissionMode, Approval: ApprovalDeny, TargetScope: permissionModeAuto, InForce: true},
				"disableAutoMode=disable", p.DisableAutoModePointer, patterns)
		}
		for i, dir := range p.AdditionalDirectories {
			cp := Node{Type: CapReadFiles, Approval: ApprovalAllow, TargetScope: secrets.RedactMatches(dir, patterns), InForce: true}
			if c.untrusted {
				cp.InForce, cp.NotInForceReason = false, ReasonWorkspaceUntrusted
			}
			c.add(cp, "additionalDirectories="+dir, "/permissions/additionalDirectories/"+strconv.Itoa(i), patterns)
		}
		if p.BlockReadsOutsideWorkingDirectories {
			c.add(Node{Type: CapReadFiles, Approval: ApprovalDeny, TargetScope: outsideWorkingDirectoryScope, InForce: true},
				"blockReadsOutsideWorkingDirectories=true", "/permissions/blockReadsOutsideWorkingDirectories", patterns)
		}
	}
	return b.fragment(), stats
}

// ProductDefaults adds one product_default capability per knowledge-base
// entry, with a has edge, for every Claude Code ai_client in nodes.
func ProductDefaults(nodes []Node, base knowledge.Base) Fragment {
	b := newFragmentBuilder()
	for _, n := range nodes {
		if n.Kind != KindAgent || n.Type != AgentAIClient || n.Platform != base.Product {
			continue
		}
		for _, e := range base.Entries {
			if !e.IsCapability() {
				continue
			}
			source := "kb:" + base.Product + "/" + e.ID + "@" + base.Version
			id := productDefaultID(n.ID, e)
			b.node(Node{
				ID: id, Kind: KindCapability, Type: e.Type, Basis: BasisProductDefault, InForce: true,
				Approval: e.Approval, TargetScope: e.TargetScope, AppliesIn: "", Source: source,
			})
			b.edge(Edge{ID: edgeID(EdgeHas, n.ID, id), Type: EdgeHas, From: n.ID, To: id, Source: source})
		}
	}
	return b.fragment()
}

// productDefaultID derives the id of a product_default capability from its
// natural key: kind + basis + client + type + entry id.
func productDefaultID(client string, e knowledge.Entry) string {
	return "cap_" + keyHash(KindCapability, BasisProductDefault, client, e.Type, e.ID)
}

// capContext carries what every capability of one settings file shares.
type capContext struct {
	b         *fragmentBuilder
	client    string
	file      string
	appliesIn string
	untrusted bool

	// managedOnly: allowManagedPermissionRulesOnly is in force and this is
	// not a managed document, so its allow, ask and deny rules do not apply
	// (https://code.claude.com/docs/en/settings-reference#allowmanagedpermissionrulesonly).
	managedOnly bool
}

// rule turns one permission rule into a capability, or counts it in stats.
func (c capContext) rule(raw, list string, index int, patterns []secrets.Pattern, stats *PermissionStats) {
	rule := secrets.RedactMatches(raw, patterns)
	tool, spec, parens, ok := splitRule(rule)
	if !ok {
		stats.Malformed++
		return
	}
	typ, neverMatches, modeled := mapTool(tool, parens)
	if !modeled {
		stats.Unmodeled[tool]++
		return
	}
	if parens && strings.ContainsAny(spec, "()") {
		stats.NestedParentheses++
	}
	target := scopeAll
	switch {
	case typ == CapInvokeMCPTool:
		target = rule
	case parens:
		target = spec
	}
	cp := Node{Type: typ, Approval: list, TargetScope: target, InForce: true}
	switch {
	case neverMatches:
		cp.InForce, cp.NotInForceReason = false, ReasonRuleNeverMatches
	case c.managedOnly:
		cp.InForce, cp.NotInForceReason = false, ReasonManagedRulesOnly
	case c.untrusted && list == ApprovalAllow:
		cp.InForce, cp.NotInForceReason = false, ReasonWorkspaceUntrusted
	}
	c.add(cp, rule, "/permissions/"+list+"/"+strconv.Itoa(index), patterns)
}

// add completes a configured capability (id, rule, source, applies_in) and
// adds it with its has edge. The first occurrence of an id wins, so source
// points at the first place the rule is written.
func (c capContext) add(cp Node, rule, pointer string, patterns []secrets.Pattern) {
	rule = secrets.RedactMatches(rule, patterns)
	cp.Kind, cp.Basis = KindCapability, BasisConfigured
	cp.AppliesIn, cp.Rule = c.appliesIn, rule
	cp.Source = c.file + "#" + pointer
	cp.ID = "cap_" + keyHash(KindCapability, BasisConfigured, c.client, c.appliesIn, cp.Approval, canonicalRule(rule))
	if c.b.node(cp) {
		c.b.edge(Edge{ID: edgeID(EdgeHas, c.client, cp.ID), Type: EdgeHas, From: c.client, To: cp.ID, Source: cp.Source})
	}
}

// splitRule splits "Tool" or "Tool(specifier)". Parentheses inside the
// specifier are literal: the specifier runs from the first "(" to the final
// ")". ok is false for anything else (an empty rule or tool, or a "(" with no
// closing ")" at the end).
func splitRule(rule string) (tool, spec string, parens, ok bool) {
	rule = strings.TrimSpace(rule)
	open := strings.Index(rule, "(")
	if open < 0 {
		return rule, "", false, rule != "" && !strings.Contains(rule, ")")
	}
	if !strings.HasSuffix(rule, ")") || open == 0 {
		return "", "", false, false
	}
	return strings.TrimSpace(rule[:open]), rule[open+1 : len(rule)-1], true, true
}

// mapTool maps a rule's tool to a capability type (decision 2026-10-23, Q3,
// as signed in BV-22). neverMatches marks rules Claude Code accepts but never
// matches: Write, NotebookEdit and Glob with a path
// (https://code.claude.com/docs/en/permissions#writepath-notebookeditpath-and-globpath-rules-never-match).
func mapTool(tool string, parens bool) (typ string, neverMatches, modeled bool) {
	switch {
	case tool == "Bash" || tool == "PowerShell":
		return CapExecuteShell, false, true
	case tool == "Read":
		return CapReadFiles, false, true
	case tool == "Edit":
		return CapWriteFiles, false, true
	case tool == "Write" || tool == "NotebookEdit":
		return CapWriteFiles, parens, true
	case tool == "Glob":
		return CapReadFiles, parens, true
	case tool == "WebFetch" || tool == "WebSearch":
		return CapNetwork, false, true
	case strings.HasPrefix(tool, "mcp__"):
		return CapInvokeMCPTool, false, true
	default:
		return "", false, false
	}
}

// canonicalRule trims a rule and collapses runs of internal whitespace, so
// formatting differences do not change the capability id.
func canonicalRule(rule string) string {
	return strings.Join(strings.Fields(rule), " ")
}

// settingsLayer is the applies_in of a settings file: "user", "managed",
// "project:<canonical repo root>" or "local:<canonical repo root>".
func settingsLayer(f platform.ClaudeSettingsFile) string {
	if f.Scope == platform.MCPScopeUser || f.Scope == platform.MCPScopeManaged {
		return f.Scope
	}
	return f.Scope + ":" + canonicalPath(f.ScopePath)
}

// trusted reports whether the workspace at root is trusted: SOME project
// entry of ~/.claude.json whose canonical path equals the canonical root has
// hasTrustDialogAccepted true. "Some" is deliberate: a real ~/.claude.json
// can hold several entries for one repository (spellings of the same path)
// with different values, and this choice shows as in force every rule that
// could be. No matching entry means untrusted.
func trusted(trust []platform.ProjectTrust, root string) bool {
	want := strings.TrimSuffix(canonicalPath(root), "/")
	for _, t := range trust {
		if t.Accepted && strings.TrimSuffix(canonicalPath(t.Path), "/") == want {
			return true
		}
	}
	return false
}

// fragmentBuilder collects nodes and edges by id (first one wins) and the
// coverage of a collector, and emits them sorted.
type fragmentBuilder struct {
	nodes  map[string]Node
	edges  map[string]Edge
	read   map[string]string // canonical path → path as read
	failed []SourceFailed
}

func newFragmentBuilder() *fragmentBuilder {
	return &fragmentBuilder{nodes: map[string]Node{}, edges: map[string]Edge{}, read: map[string]string{}}
}

// node adds n unless a node with its id exists; reports whether it was added.
func (b *fragmentBuilder) node(n Node) bool {
	if _, ok := b.nodes[n.ID]; ok {
		return false
	}
	b.nodes[n.ID] = n
	return true
}

func (b *fragmentBuilder) edge(e Edge) {
	if _, ok := b.edges[e.ID]; !ok {
		b.edges[e.ID] = e
	}
}

func (b *fragmentBuilder) fragment() Fragment {
	f := Fragment{Coverage: Coverage{
		SourcesRead:       []string{},
		SourcesFailed:     append([]SourceFailed{}, b.failed...),
		AgentsUnsupported: []AgentUnsupported{},
	}}
	for _, n := range b.nodes {
		f.Nodes = append(f.Nodes, n)
	}
	for _, e := range b.edges {
		f.Edges = append(f.Edges, e)
	}
	for _, p := range b.read {
		f.Coverage.SourcesRead = append(f.Coverage.SourcesRead, p)
	}
	sortFragment(&f)
	return f
}

// Merge combines fragments: nodes and edges by id (the first fragment's copy
// wins), coverage paths deduplicated, everything sorted.
func Merge(frags ...Fragment) Fragment {
	b := newFragmentBuilder()
	failed := map[string]bool{}
	for _, f := range frags {
		for _, n := range f.Nodes {
			b.node(n)
		}
		for _, e := range f.Edges {
			b.edge(e)
		}
		for _, p := range f.Coverage.SourcesRead {
			b.read[canonicalPath(p)] = p
		}
		for _, sf := range f.Coverage.SourcesFailed {
			if key := canonicalPath(sf.Path); !failed[key] {
				failed[key] = true
				b.failed = append(b.failed, sf)
			}
		}
	}
	return b.fragment()
}

func sortFragment(f *Fragment) {
	sort.Slice(f.Nodes, func(i, j int) bool { return f.Nodes[i].ID < f.Nodes[j].ID })
	sort.Slice(f.Edges, func(i, j int) bool { return f.Edges[i].ID < f.Edges[j].ID })
	sort.Strings(f.Coverage.SourcesRead)
	sort.SliceStable(f.Coverage.SourcesFailed, func(i, j int) bool {
		return f.Coverage.SourcesFailed[i].Path < f.Coverage.SourcesFailed[j].Path
	})
}

// FromScan builds the complete fragment of one run: MCP sources (see
// FromMCPScan), Claude Code settings and managed settings
// (FromClaudeSettings), credentials from every settings env block applied,
// the approval state of every project-scope (.mcp.json) server, and the
// product defaults of every Claude Code client found.
func FromScan(mcp platform.MCPScan, settings platform.ClaudeSettingsScan, salt []byte, discoveredAt time.Time) (Fragment, PermissionStats, error) {
	return FromScans(mcp, settings, platform.CredentialFileScan{}, salt, discoveredAt)
}

// FromScans is FromScan with the credential files collector's scan: its
// secrets become credentials (one per fingerprint, shared with the MCP and
// settings ones), the files read go to sources_read and the files not read
// to sources_failed.
func FromScans(mcp platform.MCPScan, settings platform.ClaudeSettingsScan, credFiles platform.CredentialFileScan, salt []byte, discoveredAt time.Time) (Fragment, PermissionStats, error) {
	return FromScansWithCursor(mcp, settings, platform.CursorScan{}, credFiles, salt, discoveredAt)
}

// FromScansWithCursor is FromScans with Cursor's files (decision 2026-10-25,
// Cursor, K10): its fragment, and the product_default capabilities of every
// ai_client whose product has a knowledge base.
func FromScansWithCursor(mcp platform.MCPScan, settings platform.ClaudeSettingsScan, cursor platform.CursorScan, credFiles platform.CredentialFileScan, salt []byte, discoveredAt time.Time) (Fragment, PermissionStats, error) {
	return FromScansWithRepos(mcp, settings, cursor, credFiles, Repos{}, salt, discoveredAt)
}

// Repos is what the repository scan gives the graph (decision 2026-10-24,
// memoria, §2.2): its findings, whose secrets carry their salted
// references, and the families of the repositories (root → root of the
// family's first repository).
type Repos struct {
	Findings []finding.Finding
	Families map[string]string
}

// FromScansWithRepos is FromScansWithCursor with the secrets committed in
// the repositories: credentials (the same node as a collector credential
// with the same fingerprint), git_repository resources and committed_in
// edges; a tracked file is also a location of its credential.
func FromScansWithRepos(mcp platform.MCPScan, settings platform.ClaudeSettingsScan, cursor platform.CursorScan, credFiles platform.CredentialFileScan, repos Repos, salt []byte, discoveredAt time.Time) (Fragment, PermissionStats, error) {
	policy := resolveManaged(settings.Managed)
	extra := append(settingsCredentialInputs(settings, policy), credentialFileInputsFor(credFiles, mcp.Servers)...)
	extra = append(extra, committedInputs(repos.Findings)...) // last: the collector's identity of a key wins
	mcpFrag, err := fromMCPScan(mcp, salt, discoveredAt, extra, repos.Families)
	if err != nil {
		return Fragment{}, PermissionStats{}, err
	}
	setFrag, stats := fromClaudeSettings(settings, policy, discoveredAt)
	bases, err := knowledge.All()
	if err != nil {
		return Fragment{}, PermissionStats{}, fmt.Errorf("knowledge base: %w", err)
	}
	base := bases[knowledge.ClaudeCodeProduct]
	files := newFragmentBuilder()
	for _, p := range credFiles.Read {
		files.read[canonicalPath(p)] = p
	}
	for _, f := range credFiles.Failed {
		files.failed = append(files.failed, SourceFailed{Path: f.Path, Reason: f.Reason})
	}
	merged := Merge(mcpFrag, setFrag, files.fragment(), FromCursor(cursor, settings, discoveredAt))
	applyApprovals(&merged, mcp, settings, policy)
	defaults := []Fragment{merged}
	for _, product := range []string{knowledge.ClaudeCodeProduct, knowledge.CursorProduct} {
		defaults = append(defaults, ProductDefaults(merged.Nodes, bases[product]))
	}
	return withDisabledBuiltinMode(Merge(defaults...), base), stats, nil
}

// vscodeLayer is the applies_in of the VS Code extension's settings.
const vscodeLayer = "ide:vscode"

// kbBuiltinDefaultMode is the knowledge-base entry of the built-in starting
// permission mode (decision 2026-10-24, D5).
const kbBuiltinDefaultMode = "builtin_default_mode"

// withDisabledBuiltinMode marks, for every Claude Code client with a
// disableAutoMode "disable" in force, the built-in default mode capability
// in_force false with reason mode_disabled (decision 2026-10-24, D5; schema
// enum signed in BV-34): Claude Code then starts in default
// (permission-modes, "Which mode a session starts in": "Any settings file
// sets disableAutoMode to "disable" | default"). effectiveMode counts only
// capabilities in force, so it ignores it.
func withDisabledBuiltinMode(f Fragment, base knowledge.Base) Fragment {
	e, ok := base.Entry(kbBuiltinDefaultMode)
	if !ok {
		return f
	}
	nodes := map[string]Node{}
	for _, n := range f.Nodes {
		nodes[n.ID] = n
	}
	disabled := map[string]bool{}
	for _, edge := range f.Edges {
		c, ok := nodes[edge.To]
		if edge.Type == EdgeHas && ok && c.Type == CapPermissionMode && c.Approval == ApprovalDeny &&
			c.TargetScope == permissionModeAuto && c.InForce {
			disabled[productDefaultID(edge.From, e)] = true
		}
	}
	for i, n := range f.Nodes {
		if disabled[n.ID] {
			f.Nodes[i].InForce, f.Nodes[i].NotInForceReason = false, ReasonModeDisabled
		}
	}
	return f
}

// settingsCredentialInputs lists the env entries of every settings document
// read (user, project, local) and of the managed policy, received by Claude
// Code's client. Location: "<file>#/env/<KEY>".
func settingsCredentialInputs(scan platform.ClaudeSettingsScan, policy managedPolicy) []credentialInput {
	var out []credentialInput
	add := func(path, dir string, ev platform.EnvVar) {
		out = append(out, credentialInput{
			to:       clientID(knowledge.ClaudeCodeProduct, dir),
			location: path + "#/env/" + pointerEscape(ev.Key),
			EnvVar:   ev,
		})
	}
	for _, f := range scan.Files {
		if f.Status != platform.MCPSourceRead {
			continue
		}
		for _, ev := range f.Env {
			add(f.ConfigPath, f.ConfigDir, ev)
		}
	}
	for _, e := range policy.env {
		add(e.configPath, e.configDir, e.EnvVar)
	}
	return out
}

// .mcp.json approval states (graph schema, edgeLoads.approval_state).
const (
	ApprovalApproved = "approved"
	ApprovalRejected = "rejected"
	ApprovalPending  = "pending"
)

// applyApprovals sets approval_state and certainty on the loads edge of every
// project-scope server: approved and rejected are observed, pending is
// inferred (decision 2026-10-23, S5).
func applyApprovals(f *Fragment, mcp platform.MCPScan, scan platform.ClaudeSettingsScan, policy managedPolicy) {
	index := map[string]int{}
	for i, e := range f.Edges {
		index[e.ID] = i
	}
	for _, srv := range mcp.Servers {
		if !srv.LoadsMCP || srv.Scope != platform.MCPScopeProject || productOf(srv.Client) != knowledge.ClaudeCodeProduct {
			// Claude Code's approvals of .mcp.json are its own: not
			// Antigravity's, nor Cursor's (decision 2026-10-27, blindaje,
			// B10). Those servers keep approval_state unknown, inferred.
			continue
		}
		id := edgeID(EdgeLoads, clientID(productOf(srv.Client), srv.ConfigDir), serverID(srv))
		i, ok := index[id]
		if !ok {
			continue
		}
		state := approvalState(scan, policy, srv.ScopePath, srv.Name)
		f.Edges[i].ApprovalState = state
		f.Edges[i].Certainty = CertaintyObserved
		if state == ApprovalPending {
			f.Edges[i].Certainty = CertaintyInferred
		}
	}
}

// approvalState is the approval of the .mcp.json server name in the
// repository root, as documented in
// https://code.claude.com/docs/en/settings-reference#disabledmcpjsonservers,
// #enabledmcpjsonservers, #enableallprojectmcpservers and
// https://code.claude.com/docs/en/mcp#project-server-approvals-and-workspace-trust:
//   - rejected: disabledMcpjsonServers names it in any settings file; a
//     rejection takes precedence over both approvals;
//   - approved: enabledMcpjsonServers in an honored file names it, or
//     enableAllProjectMcpServers is in force;
//   - pending otherwise.
//
// Honored files: in a trusted folder, managed, local, project and user
// settings; in an untrusted one, managed and user only (the project's own
// files, and the local file, whose git status Claude Code checks only once
// the folder is trusted, do not count). enableAllProjectMcpServers: in a
// trusted folder, the highest-precedence file that sets it decides (managed,
// local, project, user: settings#settings-precedence); in an untrusted one, a
// true in any honored file is enough. In a trusted folder the local file is
// taken as untracked: RosmarOps does not run git.
func approvalState(scan platform.ClaudeSettingsScan, policy managedPolicy, root, name string) string {
	var user, project, local []platform.ClaudeSettingsFile
	want := strings.TrimSuffix(canonicalPath(root), "/")
	for _, f := range scan.Files {
		if f.Status != platform.MCPSourceRead {
			continue
		}
		sameRoot := strings.TrimSuffix(canonicalPath(f.ScopePath), "/") == want
		switch {
		case f.Scope == platform.MCPScopeUser:
			user = append(user, f)
		case f.Scope == platform.MCPScopeProject && sameRoot:
			project = append(project, f)
		case f.Scope == platform.MCPScopeLocal && sameRoot:
			local = append(local, f)
		}
	}
	every := concat(policy.docs, local, project, user)
	for _, f := range every {
		if contains(f.DisabledMcpjsonServers, name) {
			return ApprovalRejected
		}
	}
	honored := concat(policy.docs, user)
	isTrusted := trusted(scan.Trust, root)
	if isTrusted {
		honored = every // already in precedence order
	}
	for _, f := range honored {
		if contains(f.EnabledMcpjsonServers, name) {
			return ApprovalApproved
		}
	}
	for _, f := range honored {
		if f.EnableAllProjectMcpServers == nil {
			continue
		}
		if isTrusted {
			if *f.EnableAllProjectMcpServers {
				return ApprovalApproved
			}
			break // a false in a higher-precedence file overrides a lower true
		}
		if *f.EnableAllProjectMcpServers {
			return ApprovalApproved
		}
	}
	return ApprovalPending
}

func concat(lists ...[]platform.ClaudeSettingsFile) []platform.ClaudeSettingsFile {
	var out []platform.ClaudeSettingsFile
	for _, l := range lists {
		out = append(out, l...)
	}
	return out
}
