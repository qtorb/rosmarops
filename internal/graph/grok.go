package graph

import (
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// Grok Build target scopes of configured capabilities (CTO-16 Grok §4).
const (
	GrokScopeAnyPath = "any_path"
)

// grokBypassModes approve every action without asking (CTO-16 Grok §4):
// bypassPermissions, always-approve, and yolo (which the reader maps to
// always-approve).
func grokBypassModes(mode string) bool {
	switch mode {
	case "bypassPermissions", "always-approve":
		return true
	}
	return false
}

// GrokImportedHookID is the id of a Claude Code or Cursor hook as Grok Build's
// executable: keyed by path and agent, like Cursor's imported hooks.
func GrokImportedHookID(path, client string) string {
	return "res_agent_executable_" + keyHash(ResourceAgentExecutable, canonicalPath(path), client)
}

// FromGrok builds xAI Grok Build's fragment (BV-140, CTO-16 Grok §2–§5): its
// ai_client when the Grok folder exists; one agent_config resource per config
// layer read; the configured permission mode (its own [ui], or inherited from
// Claude Code's settings, which Grok reads always); the configured capabilities
// of its in-force [permission] rules (a project layer is in force only when the
// folder is trusted, else workspace_untrusted); and the hooks of Claude Code and
// Cursor as Grok executables, per the compat cells. Product defaults, the MCP
// servers and the inference edges are added by the shared pipelines. It is a
// pure function.
func FromGrok(scan platform.GrokScan, settings platform.ClaudeSettingsScan, cursor platform.CursorScan, discoveredAt time.Time) Fragment {
	b := newFragmentBuilder()
	if !scan.Exists {
		return b.fragment()
	}
	seen := discoveredAt.UTC().Format(time.RFC3339)
	client := clientID(knowledge.GrokBuildProduct, scan.ConfigDir)
	b.node(Node{
		ID: client, Kind: KindAgent, Type: AgentAIClient, Name: clientDisplayName(knowledge.GrokBuildProduct),
		Platform: knowledge.GrokBuildProduct, ConfigPath: scan.ConfigDir, DiscoveredAt: seen,
	})

	// 1. Agent_config resources and the configured permission rules of each read
	// layer. The user and managed layers are in force; a project layer only when
	// the folder is trusted (CTO-16 Grok §2).
	ownMode, ownModeRule := "", ""
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

		appliesIn, inForce, notInForce := grokLayerForce(l)
		if l.HasPermissionMode && (l.Kind == platform.GrokLayerUser || l.Kind == platform.GrokLayerManaged) {
			ownMode, ownModeRule = l.PermissionMode, "ui.permission_mode: "+l.PermissionMode
		}
		if l.Yolo && (l.Kind == platform.GrokLayerUser || l.Kind == platform.GrokLayerManaged) {
			ownMode, ownModeRule = "always-approve", "ui.yolo: true"
		}
		// A wildcard allow in [permission] approves everything (resolution.rs
		// turns it into Allow Any): emit broad allow capabilities.
		if grokHasWildcard(l.Allow) {
			for _, typ := range []string{CapWriteFiles, CapExecuteShell, CapNetwork} {
				id := grokCapID(client, l.Kind, l.Path, typ, scopeAll)
				src := "grok:" + l.Kind + ":permission.allow: *"
				b.node(Node{
					ID: id, Kind: KindCapability, Type: typ, Basis: BasisConfigured,
					InForce: inForce, NotInForceReason: notInForce,
					Approval: ApprovalAllow, TargetScope: scopeAll, AppliesIn: appliesIn, Rule: "permission.allow: *", Source: src,
				})
				b.edge(Edge{ID: edgeID(EdgeHas, client, id), Type: EdgeHas, From: client, To: id, Source: src})
			}
		}
	}

	// BV-232 §1: [sandbox] profile, from the user and managed layers (the
	// managed one wins). Not in force on native Windows, where the sandbox
	// does not exist (Landlock on Linux, Seatbelt on macOS).
	profile, profilePath := "", ""
	for _, l := range scan.Layers {
		if l.Status != platform.MCPSourceRead || l.SandboxProfile == "" {
			continue
		}
		switch {
		case l.Kind == platform.GrokLayerManaged, l.Kind == platform.GrokLayerUser && profilePath == "":
			profile, profilePath = l.SandboxProfile, l.Path
		}
	}
	if profile != "" {
		id := grokCapID(client, "sandbox", profilePath, CapExecuteShell, profile)
		src := profilePath + "#/sandbox/profile"
		n := sandboxCap(CapExecuteShell, profile)
		n.ID, n.Kind, n.Basis, n.AppliesIn, n.Rule, n.Source = id, KindCapability, BasisConfigured, platform.MCPScopeUser, ruleGrokSandboxProfile+profile, src
		b.node(n)
		b.edge(Edge{ID: edgeID(EdgeHas, client, id), Type: EdgeHas, From: client, To: id, Source: src})
	}

	// 2. The effective permission mode: Grok's own [ui] mode, else Claude Code's
	// defaultMode (Grok reads ~/.claude/settings.json always, and the project
	// one when the folder is trusted); a wildcard in a project [permission]
	// counts only when trusted. The mode capability carries where it came from.
	mode, modeRule, modeSource, modeAppliesIn, modeInForce := ownMode, ownModeRule, "grok:ui", platform.MCPScopeUser, true
	if mode == "" {
		if m, path, scope := grokInheritedClaudeMode(settings, scan); m != "" {
			mode, modeRule, modeSource, modeAppliesIn = m, "claude defaultMode: "+m, "grok:claude_settings:"+path, scope
		}
	}
	if mode != "" {
		id := grokCapID(client, "mode", scan.ConfigDir, CapPermissionMode, mode)
		b.node(Node{
			ID: id, Kind: KindCapability, Type: CapPermissionMode, Basis: BasisConfigured, InForce: modeInForce,
			Approval: ApprovalAllow, TargetScope: mode, AppliesIn: modeAppliesIn, Rule: modeRule, Source: modeSource,
		})
		b.edge(Edge{ID: edgeID(EdgeHas, client, id), Type: EdgeHas, From: client, To: id, Source: modeSource})
	}

	// 3. Claude Code's and Cursor's hooks are Grok's executables too (CTO-16 Grok
	// §5), per the compat cells: claude hooks unless compat.claude.hooks is off,
	// cursor hooks unless compat.cursor.hooks is off.
	compat := grokMergedCompat(scan)
	if grokCompatOn(compat, "claude.hooks") {
		for _, f := range settings.Files {
			if f.Status != platform.MCPSourceRead || f.Scope == platform.MCPScopeManaged {
				continue
			}
			if f.Scope == platform.MCPScopeProject && !grokTrusts(scan, f.ScopePath) {
				continue // a project's hooks need folder trust (CTO-16 Grok §5)
			}
			for _, ptr := range f.Executables {
				if strings.HasPrefix(ptr, "/hooks/") {
					b.grokExecutable(client, f.ConfigPath+"#"+ptr, seen)
				}
			}
		}
	}
	if grokCompatOn(compat, "cursor.hooks") && cursor.Exists {
		for _, f := range cursor.Files {
			if f.Kind == platform.CursorHooks && f.Status == platform.MCPSourceRead {
				b.grokExecutable(client, f.Path, seen)
			}
		}
	}
	return b.fragment()
}

// grokExecutable adds a Claude/Cursor hook as a Grok executable resource.
func (b *fragmentBuilder) grokExecutable(client, path, seen string) {
	b.node(Node{
		ID: GrokImportedHookID(path, client), Kind: KindResource, Type: ResourceAgentExecutable,
		Path: path, Configures: client, DiscoveredAt: seen,
	})
}

// grokLayerForce is a layer's applies_in, whether its capabilities are in force,
// and the reason when not: a project layer loads only when the folder is trusted
// (CTO-16 Grok §2).
func grokLayerForce(l platform.GrokLayer) (appliesIn string, inForce bool, notInForce string) {
	if l.Kind == platform.GrokLayerProject {
		appliesIn = platform.MCPScopeProject + ":" + canonicalPath(l.Root)
		if l.Trusted {
			return appliesIn, true, ""
		}
		return appliesIn, false, ReasonWorkspaceUntrusted
	}
	return platform.MCPScopeUser, true, ""
}

// grokHasWildcard reports a "*" entry in a permission list.
func grokHasWildcard(rules []string) bool {
	for _, r := range rules {
		if strings.TrimSpace(r) == "*" {
			return true
		}
	}
	return false
}

// grokInheritedClaudeMode is the Claude Code defaultMode Grok inherits when it
// sets no [ui] mode of its own (CTO-16 Grok §4): the user settings always, the
// project settings only when the folder is trusted. Returns the mode, the file
// it came from and its applies_in.
func grokInheritedClaudeMode(settings platform.ClaudeSettingsScan, scan platform.GrokScan) (mode, path, appliesIn string) {
	for _, f := range settings.Files {
		if f.Status != platform.MCPSourceRead || f.Permissions.DefaultMode == "" {
			continue
		}
		switch f.Scope {
		case platform.MCPScopeUser:
			if mode == "" {
				mode, path, appliesIn = f.Permissions.DefaultMode, f.ConfigPath, platform.MCPScopeUser
			}
		case platform.MCPScopeProject:
			if grokTrusts(scan, f.ScopePath) {
				return f.Permissions.DefaultMode, f.ConfigPath, platform.MCPScopeProject + ":" + canonicalPath(f.ScopePath)
			}
		}
	}
	return mode, path, appliesIn
}

// grokMergedCompat merges the [compat] cells and [claude_compat] imported of the
// user and managed layers into one map of "<vendor>.<surface>" → enabled, with
// an extra key "claude.imported".
func grokMergedCompat(scan platform.GrokScan) map[string]bool {
	out := map[string]bool{}
	for _, l := range scan.Layers {
		if l.Kind != platform.GrokLayerUser && l.Kind != platform.GrokLayerManaged {
			continue
		}
		for k, v := range l.Compat {
			out[k] = v
		}
		if l.ClaudeImported {
			out["claude.imported"] = true
		}
	}
	return out
}

// grokCompatOn reports whether a compat cell is on: cells default to on, so a
// cell is off only when it is set to false (CTO-16 Grok §2; an absent cell could
// be turned off by a remote setting, handled by compat_cell_remote_unknown).
func grokCompatOn(compat map[string]bool, cell string) bool {
	v, set := compat[cell]
	return !set || v
}

// grokTrusts reports whether Grok trusts a repo root.
func grokTrusts(scan platform.GrokScan, root string) bool {
	if root == "" {
		return false
	}
	if scan.FolderTrustOff {
		return true
	}
	key := canonicalPath(root)
	for _, t := range scan.Trusted {
		if t == key {
			return true
		}
	}
	return false
}

func grokCapID(client, kind, path, typ, scope string) string {
	return "cap_" + keyHash(KindCapability, BasisConfigured, client, kind, canonicalPath(path), typ, scope)
}

// grokInference holds what one Grok Build client's edges are inferred from
// (CTO-16 Grok §4–§5). Grok reads every file the user can read (read_files allow
// any_path, no folder limit), so it reaches every credential. It writes another
// agent's configuration without asking when an approve-everything mode or a
// wildcard allow is in force (bypassPermissions / always-approve / yolo /
// [permission] allow "*"); otherwise editing asks the user.
type grokInference struct {
	client      Node
	readCap     *Node    // read_files allow, in force
	writeCap    *Node    // the capability a write goes through, if any
	writeAssume []string // extra assumptions of a write edge (auto mode's classifier)

	// BV-232 §1: the [sandbox] profile in force and the project roots (the
	// working directory of a session is one of them).
	profile string
	roots   []string
}

// Grok Build's built-in sandbox profiles (docs.x.ai/build/features/sandbox):
// what each lets the agent and its children read and write.
const (
	grokProfileWorkspace = "workspace" // reads everywhere; writes the CWD, ~/.grok and temp
	grokProfileReadOnly  = "read-only" // reads everywhere; writes ~/.grok and temp only
	grokProfileStrict    = "strict"    // reads the CWD and system paths; writes the CWD, ~/.grok and temp
)

// readsOnlyProjects reports a profile that limits reads to the working
// directory (and system paths, where no credential of the person lives).
func (in grokInference) readsOnlyProjects() bool { return in.profile == grokProfileStrict }

// writable reports whether the profile lets Grok write file: every profile
// keeps its own folder (~/.grok) writable; workspace and strict add the
// project; read-only adds nothing else; off, devbox and custom profiles
// (whose rules are not read) limit nothing.
func (in grokInference) writable(file string) bool {
	switch in.profile {
	case grokProfileWorkspace, grokProfileStrict:
		return within(in.client.ConfigPath, file) || in.inProject(file)
	case grokProfileReadOnly:
		return within(in.client.ConfigPath, file)
	}
	return true
}

func (in grokInference) inProject(file string) bool {
	for _, r := range in.roots {
		if within(r, file) {
			return true
		}
	}
	return false
}

func (p prepared) grokInference(client Node, repoRoots []string) grokInference {
	in := grokInference{client: client, roots: repoRoots, profile: grokSandboxProfile(p.caps[client.ID])}
	var modeCap *Node
	bypassMode, autoMode, wildcardWrite := false, false, false
	for i := range p.caps[client.ID] {
		c := p.caps[client.ID][i]
		if !c.InForce {
			continue
		}
		if c.Type == CapReadFiles && c.Approval == ApprovalAllow && in.readCap == nil {
			in.readCap = &p.caps[client.ID][i]
		}
		if c.Type == CapPermissionMode {
			switch {
			case grokBypassModes(c.TargetScope):
				bypassMode = true
				modeCap = &p.caps[client.ID][i]
			case c.TargetScope == "auto":
				autoMode = true
				if modeCap == nil {
					modeCap = &p.caps[client.ID][i]
				}
			}
		}
		// A configured wildcard write allow (from [permission] allow "*") writes
		// without asking.
		if c.Type == CapWriteFiles && c.Approval == ApprovalAllow && c.Basis == BasisConfigured && c.TargetScope == scopeAll {
			wildcardWrite = true
			if in.writeCap == nil {
				in.writeCap = &p.caps[client.ID][i]
			}
		}
	}
	// Grok writes another agent's configuration when an approve-everything mode
	// or a wildcard allow is in force (without asking), or in auto mode, where
	// the classifier decides (CTO-16 Grok §4, like Claude Code's auto mode). A
	// bypass or auto write with no write-allow capability goes through the mode
	// capability itself.
	switch {
	case wildcardWrite:
		// in.writeCap already set to the wildcard write cap.
	case bypassMode || autoMode:
		in.writeCap = modeCap
	}
	if autoMode && !bypassMode && !wildcardWrite {
		in.writeAssume = []string{AssumeAutoClassifierPermits}
	}
	return in
}

// reaches is one reaches edge per credential Grok can read: the whole machine
// (its own auth.json and mcp_credentials.json are excused in the rules layer).
func (in grokInference) reaches(all []Node) []Edge {
	if in.readCap == nil {
		return nil
	}
	var out []Edge
	for _, cred := range all {
		if cred.Kind != KindCredential {
			continue
		}
		if in.readsOnlyProjects() && !credFileInWorkspace(cred, in.inProject) {
			continue
		}
		out = append(out, in.edge(EdgeReaches, cred.ID, *in.readCap))
	}
	return out
}

// writes is one writes_config_of edge per agent resource with a file when Grok
// writes without asking (an approve-everything mode or a wildcard allow in
// force); otherwise editing asks the user and nothing is inferred.
func (in grokInference) writes(all []Node) []Edge {
	if in.writeCap == nil {
		return nil
	}
	var out []Edge
	for _, res := range all {
		if res.Kind != KindResource || !isAgentResource(res.Type) {
			continue
		}
		file, ok := resourceFile(res.Path)
		if !ok || !in.writable(file) {
			continue
		}
		out = append(out, in.edge(EdgeWritesConfigOf, res.ID, *in.writeCap))
	}
	return out
}

func (in grokInference) edge(typ, to string, cap Node) Edge {
	assume := baseAssumptions(AssumeLaunchFlagsNotReadable)
	if typ == EdgeWritesConfigOf {
		assume = append(assume, in.writeAssume...)
	}
	e := Edge{
		ID:          "edge_" + typ + "_" + keyHash(typ, in.client.ID, to, cap.ID),
		Type:        typ,
		From:        in.client.ID,
		To:          to,
		Source:      cap.Source,
		Certainty:   CertaintyInferred,
		DerivedFrom: []string{in.client.ID, cap.ID, to},
		Assumptions: assume,
	}
	if typ == EdgeReaches {
		e.Via = cap.ID
	}
	return e
}

// GrokMCPServers converts the MCP servers Grok loads (its own [mcp_servers],
// CTO-16 Grok §5) into the shared MCP pipeline's platform.MCPServer, so the
// pipeline builds their nodes, the loads edges from Grok's ai_client and, through
// the credential pipeline, the credentials and authenticates_to edges. A project
// layer's servers load only when the folder is trusted; their loads edge is
// project-scope, so it is pending.
func GrokMCPServers(scan platform.GrokScan) []platform.MCPServer {
	if !scan.Exists {
		return nil
	}
	var out []platform.MCPServer
	for _, l := range scan.Layers {
		if l.Status != platform.MCPSourceRead {
			continue
		}
		scope := platform.MCPScopeUser
		if l.Kind == platform.GrokLayerProject {
			if !l.Trusted {
				continue // an untrusted project's MCP servers do not load
			}
			scope = platform.MCPScopeProject
		}
		for _, s := range l.MCP {
			out = append(out, platform.MCPServer{
				Client:     knowledge.GrokBuildProduct,
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
