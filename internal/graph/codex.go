package graph

import (
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// Codex target scopes of configured capabilities (CTO-16 §4). The overview
// cells (codexCells) read them: any_path is the whole machine, working_directory
// the project it works in, workspace_sandbox a command the sandbox runs without
// asking, any_destination the network.
const (
	CodexScopeAnyPath        = "any_path"
	CodexScopeWorkingDir     = "working_directory"
	CodexScopeSandboxShell   = "workspace_sandbox"
	CodexScopeNetwork        = "any_destination"
	CodexScopeOutsideWorkdir = "outside_working_directory"
)

// FromCodex builds OpenAI Codex's fragment (BV-140, CTO-16 §4–§5): its
// ai_client when CODEX_HOME exists; one agent_config resource per config layer
// read; and the configured capabilities of each in-force layer (the user config
// and the trusted project configs) from sandbox_mode / default_permissions and
// approval_policy. Product-default capabilities come from the knowledge base
// (built where every evaluated agent's are), MCP servers and credentials from
// the shared MCP pipeline; this fragment holds only what is Codex-specific. It
// is a pure function.
func FromCodex(scan platform.CodexScan, discoveredAt time.Time) Fragment {
	b := newFragmentBuilder()
	if !scan.Exists {
		return b.fragment()
	}
	seen := discoveredAt.UTC().Format(time.RFC3339)
	client := clientID(knowledge.CodexProduct, scan.ConfigDir)
	b.node(Node{
		ID: client, Kind: KindAgent, Type: AgentAIClient, Name: clientDisplayName(knowledge.CodexProduct),
		Platform: knowledge.CodexProduct, ConfigPath: scan.ConfigDir, DiscoveredAt: seen,
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
		// Every read layer file is an agent_config resource of Codex, so that
		// another agent writing it counts against Codex (own_config_writable).
		b.node(Node{ID: configResourceID(l.Path), Kind: KindResource, Type: ResourceAgentConfig, Path: l.Path, Configures: client, DiscoveredAt: seen})

		// Only the user config and the project configs contribute capabilities.
		// Profiles are chosen with --profile at launch (not readable at rest),
		// and requirements restrict rather than add; both are kept as resources
		// but add no capability node (CTO-16 §2).
		appliesIn, inForce, notInForce, caps := "", false, "", false
		switch l.Kind {
		case platform.CodexLayerUser:
			appliesIn, inForce, caps = platform.MCPScopeUser, true, true
		case platform.CodexLayerProject:
			appliesIn, caps = platform.MCPScopeProject+":"+canonicalPath(l.Root), true
			if l.Trusted {
				inForce = true
			} else {
				notInForce = ReasonWorkspaceUntrusted // CTO-16 §2: an untrusted project's config does not load
			}
		}
		if !caps {
			continue
		}
		for _, c := range codexConfiguredCaps(l) {
			id := codexCapID(client, l.Kind, l.Path, c.typ, c.scope)
			src := "codex:" + l.Kind + ":" + c.rule
			b.node(Node{
				ID: id, Kind: KindCapability, Type: c.typ, Basis: BasisConfigured,
				InForce: inForce, NotInForceReason: notInForce,
				Approval: c.approval, TargetScope: c.scope, AppliesIn: appliesIn, Rule: c.rule, Source: src,
			})
			b.edge(Edge{ID: edgeID(EdgeHas, client, id), Type: EdgeHas, From: client, To: id, Source: src})
		}
	}
	codexComputerUseCaps(b, client, scan)
	return b.fragment()
}

// codexComputerUseCaps emits one operate_app capability per app Computer Use may
// drive without asking (BV-193, CTO-22 Computer Use §6): a target_scope per app
// id, or "*" for default_app_access = allow. The admin requirements turn the
// capability not-in-force: mode_disabled (the feature is off) or
// mode_not_allowed_in_scope (allow_persistent_approval = false removes "Always
// allow"). The user config's [computer_use] applies; a trusted project's too.
func codexComputerUseCaps(b *fragmentBuilder, client string, scan platform.CodexScan) {
	featureDisabled, persistentDisabled := false, false
	for _, l := range scan.Layers {
		featureDisabled = featureDisabled || l.CUFeatureDisabled
		persistentDisabled = persistentDisabled || l.CUPersistentApprovalDisabled
	}
	for _, l := range scan.Layers {
		if l.ComputerUse == nil {
			continue
		}
		if l.Kind == platform.CodexLayerProject && !l.Trusted {
			continue
		}
		apps := append([]string{}, l.ComputerUse.Allowed...)
		if l.ComputerUse.DefaultAppAccess == "allow" {
			apps = append(apps, scopeAll)
		}
		inForce, reason := true, ""
		switch {
		case featureDisabled:
			inForce, reason = false, ReasonModeDisabled
		case persistentDisabled:
			inForce, reason = false, ReasonModeNotAllowedInScope
		}
		src := "codex:computer_use:" + l.Kind
		for _, app := range apps {
			id := codexCapID(client, l.Kind+":cu", l.Path, CapOperateApp, app)
			b.node(Node{
				ID: id, Kind: KindCapability, Type: CapOperateApp, Basis: BasisConfigured,
				InForce: inForce, NotInForceReason: reason,
				Approval: ApprovalAllow, TargetScope: app, AppliesIn: platform.MCPScopeUser, Rule: "computer_use", Source: src,
			})
			b.edge(Edge{ID: edgeID(EdgeHas, client, id), Type: EdgeHas, From: client, To: id, Source: src})
		}
	}
}

// CodexMCPServers converts the MCP servers of Codex's in-force layers (the user
// config and the trusted project configs, CTO-16 §2) into the shared MCP
// pipeline's platform.MCPServer, so that the pipeline builds their mcp_server
// nodes, the loads edges from Codex's ai_client, and — through the credential
// pipeline reading their env and http_headers — the credentials and
// authenticates_to edges (CTO-16 §3, §5). The servers of an untrusted project do
// not load, so they are left out. The client id matches FromCodex's because the
// ConfigDir is the CODEX_HOME folder.
func CodexMCPServers(scan platform.CodexScan) []platform.MCPServer {
	if !scan.Exists {
		return nil
	}
	var out []platform.MCPServer
	for _, l := range scan.Layers {
		if l.Status != platform.MCPSourceRead {
			continue
		}
		inForce := l.Kind == platform.CodexLayerUser || (l.Kind == platform.CodexLayerProject && l.Trusted)
		if !inForce {
			continue
		}
		scope := platform.MCPScopeUser
		if l.Kind == platform.CodexLayerProject {
			scope = platform.MCPScopeProject
		}
		for _, s := range l.MCP {
			out = append(out, platform.MCPServer{
				Client:     knowledge.CodexProduct,
				Name:       s.ID,
				Command:    s.Command,
				Args:       s.Args,
				EnvKeys:    s.EnvKeys,
				Env:        s.Env,
				Headers:    s.Headers,
				ConfigPath: l.Path,
				ConfigDir:  scan.ConfigDir,
				Pointer:    "/mcp_servers/" + s.ID,
				LoadsMCP:   true,
				Scope:      scope,
				ScopePath:  l.Root,
			})
		}
	}
	return out
}

// codexCap is one configured capability to emit.
type codexCap struct {
	typ      string
	approval string
	scope    string
	rule     string
}

// codexConfiguredCaps derives a layer's configured capabilities from its
// sandbox_mode / default_permissions and approval_policy (CTO-16 §4). A layer
// that sets neither contributes no capability.
func codexConfiguredCaps(l platform.CodexLayer) []codexCap {
	var out []codexCap
	switch sandbox := codexEffectiveSandbox(l); sandbox {
	case "danger-full-access":
		out = append(out,
			codexCap{CapReadFiles, ApprovalAllow, CodexScopeAnyPath, "sandbox_mode: danger-full-access"},
			codexCap{CapWriteFiles, ApprovalAllow, CodexScopeAnyPath, "sandbox_mode: danger-full-access"},
			codexCap{CapExecuteShell, ApprovalAllow, scopeAll, "sandbox_mode: danger-full-access"},
			codexCap{CapNetwork, ApprovalAllow, CodexScopeNetwork, "sandbox_mode: danger-full-access"})
	case "workspace-write":
		net := ApprovalAsk
		if l.NetworkAccess != nil && *l.NetworkAccess {
			net = ApprovalAllow
		}
		out = append(out,
			codexCap{CapReadFiles, ApprovalAllow, CodexScopeAnyPath, "sandbox_mode: workspace-write"},
			codexCap{CapWriteFiles, ApprovalAllow, CodexScopeWorkingDir, "sandbox_mode: workspace-write"},
			codexCap{CapWriteFiles, ApprovalAsk, CodexScopeOutsideWorkdir, "sandbox_mode: workspace-write"},
			codexCap{CapExecuteShell, ApprovalAllow, CodexScopeSandboxShell, "sandbox_mode: workspace-write"},
			codexCap{CapNetwork, net, CodexScopeNetwork, "sandbox_mode: workspace-write"})
	case "read-only":
		out = append(out,
			codexCap{CapReadFiles, ApprovalAllow, CodexScopeAnyPath, "sandbox_mode: read-only"},
			codexCap{CapWriteFiles, ApprovalAsk, scopeAll, "sandbox_mode: read-only"},
			codexCap{CapExecuteShell, ApprovalAsk, scopeAll, "sandbox_mode: read-only"},
			codexCap{CapNetwork, ApprovalAsk, CodexScopeNetwork, "sandbox_mode: read-only"})
	}
	if l.HasApprovalPolicy && l.ApprovalPolicy != "" {
		rule := "approval_policy: " + l.ApprovalPolicy
		if l.ApprovalsReviewer != "" {
			rule += "; approvals_reviewer: " + l.ApprovalsReviewer
		}
		out = append(out, codexCap{CapPermissionMode, ApprovalAllow, l.ApprovalPolicy, rule})
	}
	return out
}

// codexEffectiveSandbox is the layer's sandbox mode: sandbox_mode, else the
// mode named by default_permissions (:read-only | :workspace |
// :danger-full-access), else "" (CTO-16 §4).
func codexEffectiveSandbox(l platform.CodexLayer) string {
	if l.SandboxMode != "" {
		return l.SandboxMode
	}
	switch l.DefaultPermissions {
	case ":read-only":
		return "read-only"
	case ":workspace":
		return "workspace-write"
	case ":danger-full-access":
		return "danger-full-access"
	}
	return ""
}

func codexCapID(client, kind, path, typ, scope string) string {
	return "cap_" + keyHash(KindCapability, BasisConfigured, client, kind, canonicalPath(path), typ, scope)
}

// AssumeLaunchFlagsNotReadable marks every Codex edge: a launch flag such as
// --yolo, an environment variable or a session choice is not visible at rest
// (CTO-16 §4, point 7). Its value matches the rules-layer constant of the same
// name, which validates and renders it.
const AssumeLaunchFlagsNotReadable = "launch_flags_not_readable"

// kbCodexProtected is the knowledge-base entry id of Codex's protected paths
// (.git, .agents, .codex, .aws), never written inside a writable root.
const kbCodexProtected = "protected_paths"

// codexInference holds what one Codex client's edges are inferred from
// (CTO-16 §5). Codex reads every file the user can read (read_files allow
// any_path, always in force), so it reaches every credential. It writes
// another agent's configuration without asking inside the workspace, outside
// the protected paths (sandbox_mode workspace-write); or anywhere with
// danger-full-access.
type codexInference struct {
	client    Node
	roots     []string
	protected []string        // kbCodexProtected, relative to a workspace root
	untrusted map[string]bool // explicitly-untrusted roots: Codex writes nothing there
	readCap   *Node           // read_files allow, in force (any_path)
	writeCap  *Node           // write_files allow, in force; widest scope
	writeAny  bool            // writeCap reaches any path (danger-full-access)
}

// MarkCodexUntrusted records on the Codex ai_client, after Build, the repo
// roots the user config marks trust_level = "untrusted" (BV-140, CTO-16 §5): in
// those projects Codex loads nothing and asks before commands, so the inference
// writes nothing inside them. Called like CrossIntel, on the built graph.
func MarkCodexUntrusted(g *Graph, scan platform.CodexScan) {
	if !scan.Exists {
		return
	}
	var roots []string
	for _, l := range scan.Layers {
		if l.Kind == platform.CodexLayerProject && l.TrustLevel == "untrusted" && l.Root != "" {
			roots = append(roots, canonicalPath(l.Root))
		}
	}
	if len(roots) == 0 {
		return
	}
	client := clientID(knowledge.CodexProduct, scan.ConfigDir)
	for i := range g.Nodes {
		if g.Nodes[i].ID == client {
			g.Nodes[i].CodexUntrustedRoots = roots
		}
	}
}

func (p prepared) codexInference(client Node, repoRoots []string) codexInference {
	in := codexInference{client: client, roots: append([]string{}, repoRoots...), untrusted: map[string]bool{}}
	for _, r := range client.CodexUntrustedRoots {
		in.untrusted[r] = true
	}
	if e, ok := p.codex.Entry(kbCodexProtected); ok {
		in.protected = e.Paths
	}
	for i := range p.caps[client.ID] {
		c := p.caps[client.ID][i]
		if !c.InForce || c.Approval != ApprovalAllow {
			continue
		}
		switch c.Type {
		case CapReadFiles:
			if in.readCap == nil || c.TargetScope == CodexScopeAnyPath {
				in.readCap = &p.caps[client.ID][i]
			}
		case CapWriteFiles:
			// The widest write-allow in force governs: any_path
			// (danger-full-access) over working_directory (workspace-write).
			if in.writeCap == nil || (!in.writeAny && c.TargetScope == CodexScopeAnyPath) {
				in.writeCap = &p.caps[client.ID][i]
			}
			if c.TargetScope == CodexScopeAnyPath {
				in.writeAny = true
			}
		}
	}
	return in
}

// reaches is one reaches edge per credential Codex can read: the whole machine
// (its own auth.json is excused in the rules layer, not here).
func (in codexInference) reaches(all []Node) []Edge {
	if in.readCap == nil {
		return nil
	}
	var out []Edge
	for _, cred := range all {
		if cred.Kind != KindCredential {
			continue
		}
		out = append(out, in.edge(EdgeReaches, cred.ID, *in.readCap))
	}
	return out
}

// writes is one writes_config_of edge per agent resource with a file Codex can
// write without a person: any resource with danger-full-access; a resource
// inside a workspace root and outside the protected paths with workspace-write.
func (in codexInference) writes(all []Node) []Edge {
	if in.writeCap == nil {
		return nil
	}
	var out []Edge
	for _, res := range all {
		if res.Kind != KindResource || !isAgentResource(res.Type) {
			continue
		}
		file, ok := resourceFile(res.Path)
		if !ok {
			continue
		}
		if in.inUntrusted(file) {
			continue // an explicitly-untrusted project is read-only for Codex (CTO-16 §5)
		}
		if !in.writeAny && !in.writableWorkspace(file) {
			continue
		}
		out = append(out, in.edge(EdgeWritesConfigOf, res.ID, *in.writeCap))
	}
	return out
}

// inUntrusted reports a file inside a repo root the user marked
// trust_level = "untrusted": Codex loads nothing and writes nothing there.
func (in codexInference) inUntrusted(file string) bool {
	for r := range in.untrusted {
		if within(r, file) {
			return true
		}
	}
	return false
}

// writableWorkspace reports a file inside a workspace root and outside the
// protected paths (.git, .agents, .codex, .aws), which workspace-write edits
// without asking.
func (in codexInference) writableWorkspace(file string) bool {
	for _, r := range in.roots {
		if !within(r, file) {
			continue
		}
		rel, ok := relTo(r, file)
		if ok && !matchesAny(in.protected, rel) {
			return true
		}
	}
	return false
}

func (in codexInference) edge(typ, to string, cap Node) Edge {
	e := Edge{
		ID:          "edge_" + typ + "_" + keyHash(typ, in.client.ID, to, cap.ID),
		Type:        typ,
		From:        in.client.ID,
		To:          to,
		Source:      cap.Source,
		Certainty:   CertaintyInferred,
		DerivedFrom: []string{in.client.ID, cap.ID, to},
		Assumptions: baseAssumptions(AssumeLaunchFlagsNotReadable),
	}
	if typ == EdgeReaches {
		e.Via = cap.ID
	}
	return e
}
