package graph

import (
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// Gemini CLI effective approval modes and scopes (BV-193, CTO-22 Gemini §4).
const (
	geminiModeAutoEdit = "auto_edit"
	geminiModeYolo     = "yolo"
	geminiScopeWork    = "workspace"
	geminiScopeSystem  = "system" // applies_in for the system settings layer (CTO-22 Gemini §2, G5)
)

// FromGeminiCLI builds Google Gemini CLI's fragment (BV-193, CTO-22 Gemini
// §2–§5): its ai_client when present; an agent_config resource per settings
// layer read; the product-default read capability (reads the workspace without
// asking); and the configured capabilities of each layer — auto_edit writes
// without asking, tools.allowed run_shell_command runs any command, a trusted
// MCP server runs without asking, and the permission mode. A project layer's
// configured capabilities are in force only when the folder is trusted
// (trustedFolders.json); otherwise they carry workspace_untrusted. Its MCP
// servers and credentials come through the shared pipelines. It is a pure
// function.
func FromGeminiCLI(scan platform.GeminiScan, discoveredAt time.Time) Fragment {
	b := newFragmentBuilder()
	if !scan.Present {
		return b.fragment()
	}
	seen := discoveredAt.UTC().Format(time.RFC3339)
	client := clientID(knowledge.GeminiCLIProduct, scan.ConfigDir)
	b.node(Node{
		ID: client, Kind: KindAgent, Type: AgentAIClient, Name: clientDisplayName(knowledge.GeminiCLIProduct),
		Platform: knowledge.GeminiCLIProduct, ConfigPath: scan.ConfigDir, DiscoveredAt: seen,
	})

	// Product default: reads the workspace without asking (read_file is confined
	// to the workspace, CTO-22 Gemini §4). This is the read capability the
	// inference reaches credentials with.
	readID := "cap_" + keyHash(KindCapability, BasisProductDefault, client, CapReadFiles, geminiScopeWork)
	readSrc := "kb:" + knowledge.GeminiCLIProduct + "/read_workspace@" + knowledge.GeminiCLIVersion
	b.node(Node{
		ID: readID, Kind: KindCapability, Type: CapReadFiles, Basis: BasisProductDefault, InForce: true,
		Approval: ApprovalAllow, TargetScope: geminiScopeWork, Source: readSrc,
	})
	b.edge(Edge{ID: edgeID(EdgeHas, client, readID), Type: EdgeHas, From: client, To: readID, Source: readSrc})

	for _, l := range scan.Layers {
		switch l.Status {
		case platform.MCPSourceFailed:
			b.failed = append(b.failed, SourceFailed{Path: l.Path, Reason: l.Reason})
			continue
		case platform.MCPSourceMissing:
			continue
		}
		// A system file that failed Gemini's security permission check is
		// ignored by the CLI (CTO-22 Gemini §2): read, but not applied, and
		// surfaced in coverage rather than as capabilities.
		if l.Kind == platform.GeminiLayerSystem && l.SecurePermError {
			b.failed = append(b.failed, SourceFailed{Path: l.Path, Reason: Reason(ReasonManagedRulesOnly, "system settings failed the secure-permission check")})
			continue
		}
		b.read[canonicalPath(l.Path)] = l.Path
		b.node(Node{ID: configResourceID(l.Path), Kind: KindResource, Type: ResourceAgentConfig, Path: l.Path, Configures: client, DiscoveredAt: seen})

		appliesIn, inForce, notInForce := geminiLayerForce(scan, l)
		for _, c := range geminiConfiguredCaps(l) {
			id := geminiCapID(client, l.Kind, l.Path, c.typ, c.scope)
			src := "gemini:" + l.Kind + ":" + c.rule
			cInForce := inForce && c.force
			cReason := notInForce
			if !c.force { // a cap the setting disables (disableYoloMode)
				cInForce, cReason = false, c.reason
			}
			b.node(Node{
				ID: id, Kind: KindCapability, Type: c.typ, Basis: BasisConfigured,
				InForce: cInForce, NotInForceReason: cReason,
				Approval: c.approval, TargetScope: c.scope, AppliesIn: appliesIn, Rule: c.rule, Source: src,
			})
			b.edge(Edge{ID: edgeID(EdgeHas, client, id), Type: EdgeHas, From: client, To: id, Source: src})
		}
	}
	geminiPolicyCaps(b, client, scan)
	geminiHookResources(b, client, scan, seen)
	return b.fragment()
}

// geminiPolicyCaps emits the execute_shell capabilities from Gemini CLI's admin
// and user TOML policy tiers and its extensions' policies (CTO-22 Gemini §6): a
// rule allowing run_shell_command with no command prefix runs any command
// without asking (nobody); with a prefix it is a rule decider. Project policies
// are disabled in the CLI, so the reader never provides them. Admin wins over
// user, but both are in force, so both are emitted.
func geminiPolicyCaps(b *fragmentBuilder, client string, scan platform.GeminiScan) {
	emit := func(tier string, rules []platform.GeminiPolicyRule) {
		for _, r := range rules {
			if r.Decision != "allow" {
				continue
			}
			for _, tool := range r.ToolNames {
				if tool != "run_shell_command" {
					continue
				}
				approval, scope := ApprovalAllow, scopeAll
				if len(r.CommandPrefix) > 0 { // specific commands, not any command
					scope = strings.Join(r.CommandPrefix, ",")
				}
				id := geminiCapID(client, "policy:"+tier, r.Path, CapExecuteShell, scope)
				src := "gemini:policy:" + tier
				b.node(Node{
					ID: id, Kind: KindCapability, Type: CapExecuteShell, Basis: BasisConfigured, InForce: true,
					Approval: approval, TargetScope: scope, AppliesIn: platform.MCPScopeUser, Rule: "policy." + tier, Source: src,
				})
				b.edge(Edge{ID: edgeID(EdgeHas, client, id), Type: EdgeHas, From: client, To: id, Source: src})
			}
		}
	}
	for _, p := range scan.Policies {
		emit(p.Kind, p.Rules)
	}
	for _, ext := range scan.Extensions {
		emit("extension", ext.Policies)
	}
}

// geminiHookResources emits an agent_executable resource for each project-layer
// hook whose command names a repo script (CTO-22 Gemini §5): the rules layer
// turns it into project_hook_runs_repo_script. Only when the layer's hooks are
// enabled and the project folder is trusted (the CLI runs project hooks only
// then).
func geminiHookResources(b *fragmentBuilder, client string, scan platform.GeminiScan, seen string) {
	for _, l := range scan.Layers {
		if l.Kind != platform.GeminiLayerProject || l.HooksDisabled || !GeminiTrusted(scan, l.Root) {
			continue
		}
		for _, h := range l.Hooks {
			if h.ScriptPath == "" {
				continue
			}
			b.read[canonicalPath(h.Path)] = h.Path
			b.node(Node{
				ID: AntigravityHookID(h.Path, client), Kind: KindResource, Type: ResourceAgentExecutable,
				Path: h.Path, Configures: client, DiscoveredAt: seen, ScriptPath: h.ScriptPath, ScriptSize: h.ScriptSize,
			})
		}
	}
}

// geminiLayerForce is a layer's applies_in, whether its configured capabilities
// are in force, and the reason when not (CTO-22 Gemini §2). A project layer is
// in force only when its folder is trusted.
func geminiLayerForce(scan platform.GeminiScan, l platform.GeminiLayer) (appliesIn string, inForce bool, notInForce string) {
	switch l.Kind {
	case platform.GeminiLayerProject:
		appliesIn = platform.MCPScopeProject + ":" + canonicalPath(l.Root)
		if GeminiTrusted(scan, l.Root) {
			return appliesIn, true, ""
		}
		return appliesIn, false, ReasonWorkspaceUntrusted
	case platform.GeminiLayerSystem:
		return geminiScopeSystem, true, "" // CTO-22 Gemini §2: the system layer wins
	default: // user, system-defaults
		return platform.MCPScopeUser, true, ""
	}
}

// geminiCap is one configured capability to emit. force=false means the setting
// turns a mode off (disableYoloMode): the node is emitted not-in-force with
// reason.
type geminiCap struct {
	typ      string
	approval string
	scope    string
	rule     string
	force    bool
	reason   string
}

// geminiConfiguredCaps derives a layer's configured capabilities from its
// defaultApprovalMode, tools.allowed, security.disableYoloMode and the trusted
// MCP servers (CTO-22 Gemini §4). yolo in the file is ignored, so it adds no
// capability.
func geminiConfiguredCaps(l platform.GeminiLayer) []geminiCap {
	var out []geminiCap
	if l.DefaultApprovalMode == geminiModeAutoEdit {
		out = append(out, geminiCap{CapWriteFiles, ApprovalAllow, geminiScopeWork, "general.defaultApprovalMode: auto_edit", true, ""})
		out = append(out, geminiCap{CapPermissionMode, ApprovalAllow, geminiModeAutoEdit, "general.defaultApprovalMode: auto_edit", true, ""})
	}
	for _, a := range l.ToolsAllowed {
		if a == "run_shell_command" { // no prefix: any command runs without asking
			out = append(out, geminiCap{CapExecuteShell, ApprovalAllow, scopeAll, "tools.allowed: run_shell_command", true, ""})
			break
		}
	}
	if l.DisableYoloMode {
		out = append(out, geminiCap{CapPermissionMode, ApprovalDeny, geminiModeYolo, "security.disableYoloMode", false, ReasonModeDisabled})
	}
	for _, s := range l.MCP {
		if s.Trust {
			out = append(out, geminiCap{CapInvokeMCPTool, ApprovalAllow, scopeAll, "mcpServers." + s.ID + ".trust", true, ""})
			break
		}
	}
	return out
}

func geminiCapID(client, kind, path, typ, scope string) string {
	return "cap_" + keyHash(KindCapability, BasisConfigured, client, kind, canonicalPath(path), typ, scope)
}

// GeminiTrusted reports whether a repo root is a trusted folder for Gemini CLI
// (CTO-22 Gemini §2): trust is off (everything trusted), or trustedFolders.json
// lists the folder TRUST_FOLDER or an ancestor TRUST_PARENT. A folder not
// listed, or DO_NOT_TRUST, is untrusted.
func GeminiTrusted(scan platform.GeminiScan, root string) bool {
	if !scan.FolderTrustEnabled {
		return true
	}
	rc := canonicalPath(root)
	for p, v := range scan.TrustedFolders {
		pc := canonicalPath(p)
		switch v {
		case "TRUST_FOLDER":
			if pc == rc {
				return true
			}
		case "TRUST_PARENT":
			if pc == rc || within(pc, rc) {
				return true
			}
		}
	}
	return false
}

// GeminiMCPServers converts the MCP servers of Gemini's in-force layers (the
// user and system layers, and the trusted project layers) into the shared MCP
// pipeline's platform.MCPServer, so the pipeline builds their mcp_server nodes,
// the loads edges from Gemini's ai_client and, through the credential pipeline,
// their credentials. An untrusted project's servers do not load (CTO-22 Gemini
// §2), so they are left out.
func GeminiMCPServers(scan platform.GeminiScan) []platform.MCPServer {
	if !scan.Present {
		return nil
	}
	var out []platform.MCPServer
	for _, l := range scan.Layers {
		if l.Status != platform.MCPSourceRead {
			continue
		}
		scope := platform.MCPScopeUser
		if l.Kind == platform.GeminiLayerProject {
			if !GeminiTrusted(scan, l.Root) {
				continue
			}
			scope = platform.MCPScopeProject
		}
		for _, s := range l.MCP {
			out = append(out, platform.MCPServer{
				Client: knowledge.GeminiCLIProduct, Name: s.ID, Command: s.Command, Args: s.Args,
				URL: s.URL, EnvKeys: s.EnvKeys, Env: s.Env, Headers: s.Headers,
				ConfigPath: l.Path, ConfigDir: scan.ConfigDir, Pointer: "/mcpServers/" + s.ID,
				LoadsMCP: true, Scope: scope, ScopePath: l.Root,
			})
		}
	}
	// Extensions (CTO-22 Gemini §5): each extension's mcpServers load like the
	// user layer's, with the extension's own config_path.
	for _, ext := range scan.Extensions {
		if ext.Status != platform.MCPSourceRead {
			continue
		}
		for _, s := range ext.MCP {
			out = append(out, platform.MCPServer{
				Client: knowledge.GeminiCLIProduct, Name: s.ID, Command: s.Command, Args: s.Args,
				URL: s.URL, EnvKeys: s.EnvKeys, Env: s.Env, Headers: s.Headers,
				ConfigPath: ext.ConfigPath, ConfigDir: scan.ConfigDir, Pointer: "/mcpServers/" + s.ID,
				LoadsMCP: true, Scope: platform.MCPScopeUser,
			})
		}
	}
	return out
}

// geminiInference is Gemini CLI's reaches and writes (CTO-22 Gemini §4–§5): it
// reads the workspace without asking (reaches every credential inside a trusted
// repo root), and with auto_edit it writes another agent's configuration in the
// workspace without asking.
type geminiInference struct {
	client    Node
	roots     []string
	untrusted map[string]bool
	envDenied map[string]bool
	readCap   *Node
	writeCap  *Node
}

func (p prepared) geminiInference(client Node, repoRoots []string) geminiInference {
	in := geminiInference{client: client, roots: append([]string{}, repoRoots...), untrusted: map[string]bool{}, envDenied: map[string]bool{}}
	for _, r := range client.GeminiUntrustedRoots {
		in.untrusted[r] = true
	}
	for _, loc := range client.GeminiEnvDenied {
		in.envDenied[loc] = true
	}
	for i := range p.caps[client.ID] {
		c := p.caps[client.ID][i]
		if !c.InForce || c.Approval != ApprovalAllow {
			continue
		}
		switch c.Type {
		case CapReadFiles:
			if in.readCap == nil {
				in.readCap = &p.caps[client.ID][i]
			}
		case CapWriteFiles:
			if in.writeCap == nil {
				in.writeCap = &p.caps[client.ID][i]
			}
		}
	}
	return in
}

// inWorkspace reports a file under a repo root. trustedOnly excludes roots that
// are not trusted folders: read_file is not gated by folder trust (Gemini reads
// the workspace to let the user review before trusting), but writing without
// asking (auto_edit) reverts to the asking mode in an untrusted folder, so
// writes only reach the trusted roots (CTO-22 Gemini §4).
func (in geminiInference) inWorkspace(file string, trustedOnly bool) bool {
	for _, r := range in.roots {
		if trustedOnly && in.untrusted[canonicalPath(r)] {
			continue
		}
		if within(r, file) {
			return true
		}
	}
	return false
}

func (in geminiInference) reaches(all []Node) []Edge {
	if in.readCap == nil {
		return nil
	}
	var out []Edge
	for _, cred := range all {
		if cred.Kind != KindCredential {
			continue
		}
		if !in.credInWorkspace(cred) {
			continue
		}
		out = append(out, in.edge(EdgeReaches, cred.ID, *in.readCap))
	}
	return out
}

// credInWorkspace reports a credential whose file is under a repo root. A
// credential node carries its file in Locations as "<path>:<line>" (Path is
// empty); reads are not gated by folder trust, so every root counts.
func (in geminiInference) credInWorkspace(cred Node) bool {
	for _, loc := range cred.Locations {
		if in.envDenied[loc] { // a .env location Gemini does not load (CTO-22 Gemini §3)
			continue
		}
		file := loc
		if i := strings.LastIndex(file, ":"); i > 0 {
			file = file[:i]
		}
		if in.inWorkspace(file, false) {
			return true
		}
	}
	return false
}

func (in geminiInference) writes(all []Node) []Edge {
	if in.writeCap == nil {
		return nil
	}
	var out []Edge
	for _, res := range all {
		if res.Kind != KindResource || !isAgentResource(res.Type) {
			continue
		}
		file, ok := resourceFile(res.Path)
		if !ok || !in.inWorkspace(file, true) {
			continue
		}
		out = append(out, in.edge(EdgeWritesConfigOf, res.ID, *in.writeCap))
	}
	return out
}

func (in geminiInference) edge(typ, to string, capNode Node) Edge {
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

// MarkGeminiUntrusted records on the Gemini ai_client, after Build, the repo
// roots that are not trusted folders (CTO-22 Gemini §2): Gemini reaches no
// credential and writes no config inside them.
func MarkGeminiUntrusted(g *Graph, scan platform.GeminiScan, repoRoots []string) {
	if !scan.Present {
		return
	}
	var roots []string
	for _, r := range repoRoots {
		if !GeminiTrusted(scan, r) {
			roots = append(roots, canonicalPath(r))
		}
	}
	if len(roots) == 0 && len(scan.EnvDenied) == 0 {
		return
	}
	client := clientID(knowledge.GeminiCLIProduct, scan.ConfigDir)
	for i := range g.Nodes {
		if g.Nodes[i].ID == client {
			g.Nodes[i].GeminiUntrustedRoots = roots
			g.Nodes[i].GeminiEnvDenied = scan.EnvDenied
		}
	}
}
