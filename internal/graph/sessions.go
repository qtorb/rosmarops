package graph

import (
	"runtime"
	"sort"
	"strings"
)

// HostOS is the system the scanned machine runs (runtime.GOOS). The sandboxes
// of Claude Code and Grok Build run only on macOS, Linux and WSL2 (which is
// Linux to the program); on native Windows they do not exist (BV-232 §1).
// Tests set it to model another system.
var HostOS = runtime.GOOS

// sandboxSupported reports whether the agents' sandboxes exist on HostOS.
func sandboxSupported() bool { return HostOS != "windows" }

// Sandbox settings read at rest (BV-232 §1) are configured capabilities of
// approval "unknown": they grant and deny nothing by themselves; the inference
// and the rules layer read them to cut ways. On native Windows they are not in
// force (sandbox_unsupported_on_platform).
const (
	ruleSandboxEnabled     = "sandbox.enabled="
	ruleSandboxFail        = "sandbox.failIfUnavailable="
	ruleSandboxUnsandboxed = "sandbox.allowUnsandboxedCommands="
	ruleSandboxDomain      = "sandbox.network.allowedDomains="
	ruleSandboxDenyRead    = "sandbox.filesystem.denyRead="
	ruleGrokSandboxProfile = "sandbox.profile = "

	approvalSetting = "unknown"
)

// sandboxCap is the capability of one sandbox setting: a boolean key (an
// execute_shell setting with its value as target), an allowed domain (a
// network setting) or a denied read path (a read_files setting).
func sandboxCap(typ, target string) Node {
	n := Node{Type: typ, Approval: approvalSetting, TargetScope: target, InForce: true}
	if !sandboxSupported() {
		n.InForce, n.NotInForceReason = false, ReasonSandboxUnsupportedOnPlatform
	}
	return n
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// isSandboxSetting reports a sandbox setting capability.
func isSandboxSetting(c Node) bool {
	return c.Approval == approvalSetting && c.Basis == BasisConfigured && strings.HasPrefix(c.Rule, "sandbox.")
}

// claudeSession is how a session started in one project runs (BV-232 §1, §4):
// its starting mode and its sandbox. root is the project; "" when there is no
// project (the user's settings alone).
type claudeSession struct {
	root    string
	mode    string
	modeCap *Node
	sb      sandboxState
}

// sandboxState is a session's sandbox from the settings it loads.
type sandboxState struct {
	// sure: enabled, and failIfUnavailable, so a sandbox that cannot start
	// stops Claude Code instead of leaving commands unsandboxed.
	sure bool
	// noEscape: allowUnsandboxedCommands false, so a command cannot retry
	// outside the sandbox.
	noEscape bool
	denyRead []string
	domains  []string
}

// effective reports the sandbox contains the session's shell commands: it is
// sure, and no command can leave it without a person (the unsandboxed retry
// is closed, or the mode asks before it: bypassPermissions and auto do not).
func (s claudeSession) sandboxEffective() bool {
	if !s.sb.sure {
		return false
	}
	return s.sb.noEscape || (s.mode != permissionModeBypass && s.mode != permissionModeAuto)
}

// blocksReadsOutside reports a blockReadsOutsideWorkingDirectories in force
// in a layer this session loads (BV-237: a project's rule counts only in that
// project).
func (s claudeSession) blocksReadsOutside(inForce []Node) bool {
	for _, c := range inForce {
		if c.Rule == ruleBlockReadsOutside && appliesTo(c, s.root) {
			return true
		}
	}
	return false
}

// sandboxDenies reports f is under one of the session's sandbox denyRead
// paths (~/ resolved against home).
func (s claudeSession) sandboxDenies(f, home string) bool {
	for _, p := range s.sb.denyRead {
		if within(expandHome(p, home), f) {
			return true
		}
	}
	return false
}

// broadNetwork reports an allowed domain that opens any host.
func (s claudeSession) broadNetwork() bool {
	for _, d := range s.sb.domains {
		if d == "*" || d == "*.*" {
			return true
		}
	}
	return false
}

func expandHome(p, home string) string {
	switch {
	case p == "~":
		return home
	case strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`):
		return home + "/" + p[2:]
	}
	return p
}

// layerRoot is the project root of a project or local layer.
func layerRoot(appliesIn string) (scope, root string) {
	scope, root, _ = strings.Cut(appliesIn, ":")
	return scope, root
}

// appliesTo reports a configured capability's layer loads in a session in
// root: managed, user, and the project and local layers of root.
func appliesTo(c Node, root string) bool {
	scope, r := layerRoot(c.AppliesIn)
	switch scope {
	case "managed", "user":
		return true
	case "project", "local":
		return root != "" && r == canonicalPath(root)
	}
	return false
}

// layerRank orders the layers of one session, highest precedence first
// (settings precedence: managed, local, project, user).
func layerRank(c Node) int {
	scope, _ := layerRoot(c.AppliesIn)
	switch scope {
	case "managed":
		return 0
	case "local":
		return 1
	case "project":
		return 2
	case "user":
		return 3
	}
	return 4
}

// modePermissiveness orders the modes, most permissive last (BV-232 §4:
// «the less restrictive of the two»).
func modePermissiveness(mode string) int {
	switch mode {
	case "dontAsk":
		return 0
	case "plan":
		return 1
	case permissionModeAcceptEdits:
		return 3
	case permissionModeAuto:
		return 4
	case permissionModeBypass:
		return 5
	}
	return 2 // default, and any other value
}

// claudeSessions are the sessions of a Claude Code client, one per project
// root (BV-232 §4): in a terminal session the project's (or its local) mode
// wins over the user's for every value except auto and bypassPermissions,
// which do not take effect from a project and are not in force there; a
// managed mode outranks both. With no project, the one session of the user's
// settings. Each session's sandbox comes from the settings it loads.
func claudeSessions(inForce []Node, repoRoots []string, home string) []claudeSession {
	roots := repoRoots
	if len(roots) == 0 {
		roots = []string{""}
	}
	out := make([]claudeSession, 0, len(roots))
	for _, r := range roots {
		s := claudeSession{root: r}
		s.mode, s.modeCap = sessionMode(inForce, r)
		s.sb = sessionSandbox(inForce, r)
		out = append(out, s)
	}
	return out
}

// sessionMode is the starting mode of a session in root: the managed mode,
// else the local or project mode of root, else the user's, else the product
// default; then "default" when a deny in force disables it.
func sessionMode(inForce []Node, root string) (string, *Node) {
	var chosen *Node
	best := 99
	for i := range inForce {
		c := inForce[i]
		if c.Type != CapPermissionMode || c.Approval != ApprovalAllow || c.Basis != BasisConfigured || !appliesTo(c, root) {
			continue
		}
		if rank := layerRank(c); rank < best {
			best, chosen = rank, &inForce[i]
		}
	}
	if chosen == nil {
		for i := range inForce {
			c := inForce[i]
			if c.Type == CapPermissionMode && c.Approval == ApprovalAllow && c.Basis == BasisProductDefault {
				chosen = &inForce[i]
				break
			}
		}
	}
	if chosen == nil {
		return permissionModeDefault, nil
	}
	// A deny counts only in the sessions its layer loads (BV-237): a
	// disableAutoMode in one project's settings does not disable auto in the
	// others.
	for _, c := range inForce {
		if c.Type == CapPermissionMode && c.Approval == ApprovalDeny && c.TargetScope == chosen.TargetScope && appliesTo(c, root) {
			return permissionModeDefault, nil
		}
	}
	return chosen.TargetScope, chosen
}

// sessionSandbox is a session's sandbox: each boolean key from the layer of
// highest precedence that sets it (a managed value overrides anything a
// developer sets), the lists merged across the layers it loads.
func sessionSandbox(inForce []Node, root string) sandboxState {
	type pick struct {
		rank  int
		value bool
	}
	keys := map[string]*pick{}
	var st sandboxState
	for _, c := range inForce {
		if !isSandboxSetting(c) || !appliesTo(c, root) {
			continue
		}
		key, value, _ := strings.Cut(c.Rule, "=")
		switch key + "=" {
		case ruleSandboxEnabled, ruleSandboxFail, ruleSandboxUnsandboxed:
			if p := keys[key]; p == nil || layerRank(c) < p.rank {
				keys[key] = &pick{layerRank(c), value == "true"}
			}
		case ruleSandboxDomain:
			st.domains = append(st.domains, c.TargetScope)
		case ruleSandboxDenyRead:
			st.denyRead = append(st.denyRead, c.TargetScope)
		}
	}
	val := func(rule string, def bool) bool {
		if p := keys[strings.TrimSuffix(rule, "=")]; p != nil {
			return p.value
		}
		return def
	}
	st.sure = val(ruleSandboxEnabled, false) && val(ruleSandboxFail, false)
	st.noEscape = !val(ruleSandboxUnsandboxed, true)
	sort.Strings(st.denyRead)
	sort.Strings(st.domains)
	return st
}

// worstMode is the most permissive mode of the sessions, and its capability:
// what the per-agent rules (egress, cells) assume, since the person can start
// the agent in any of their projects.
func worstMode(sessions []claudeSession) (string, *Node) {
	mode, cap := permissionModeDefault, (*Node)(nil)
	for i, s := range sessions {
		if i == 0 || modePermissiveness(s.mode) > modePermissiveness(mode) {
			mode, cap = s.mode, s.modeCap
		}
	}
	return mode, cap
}

// MarkClaudeVSCodeModes applies BV-232 §4 to the built graph when the Claude
// Code extension for VS Code is installed: the extension's sessions do not
// read the project's starting mode, and we do not know where the person will
// open a project, so each project takes the less restrictive of its own mode
// and the user's. A project's mode that is stricter than the user's is then
// not in force (mode_not_allowed_in_scope), and the project falls back to the
// user's mode. Without the extension it is a no-op.
func MarkClaudeVSCodeModes(g *Graph, extensionInstalled bool) {
	if !extensionInstalled {
		return
	}
	idx := map[string]int{}
	for i, n := range g.Nodes {
		idx[n.ID] = i
	}
	caps := map[string][]int{}
	for _, e := range g.Edges {
		if e.Type == EdgeHas {
			if i, ok := idx[e.To]; ok && g.Nodes[i].Kind == KindCapability {
				caps[e.From] = append(caps[e.From], i)
			}
		}
	}
	for _, client := range g.Nodes {
		if client.Kind != KindAgent || client.Type != AgentAIClient || client.Platform != "claude-code" {
			continue
		}
		var inForce []Node
		for _, i := range caps[client.ID] {
			if g.Nodes[i].InForce {
				inForce = append(inForce, g.Nodes[i])
			}
		}
		user, _ := sessionMode(inForce, "")
		// Every project root that sets its own mode: the chosen project-level
		// mode, and all its project-level mode capabilities.
		type projectModes struct {
			chosen *Node
			ids    []string
		}
		byRoot := map[string]*projectModes{}
		for _, c := range inForce {
			scope, root := layerRoot(c.AppliesIn)
			if c.Type != CapPermissionMode || c.Approval != ApprovalAllow || (scope != "project" && scope != "local") {
				continue
			}
			pm := byRoot[root]
			if pm == nil {
				pm = &projectModes{}
				byRoot[root] = pm
			}
			pm.ids = append(pm.ids, c.ID)
			if pm.chosen == nil || layerRank(c) < layerRank(*pm.chosen) {
				cc := c
				pm.chosen = &cc
			}
		}
		for _, pm := range byRoot {
			if modePermissiveness(user) <= modePermissiveness(pm.chosen.TargetScope) {
				continue
			}
			for _, id := range pm.ids {
				n := &g.Nodes[idx[id]]
				n.InForce, n.NotInForceReason = false, ReasonModeNotAllowedInScope
			}
		}
	}
}

// grokSandboxProfile is the [sandbox] profile in force of a Grok Build client
// (BV-232 §1), "" when none or not in force (native Windows).
func grokSandboxProfile(caps []Node) string {
	for _, c := range caps {
		if c.InForce && isSandboxSetting(c) && strings.HasPrefix(c.Rule, ruleGrokSandboxProfile) {
			return c.TargetScope
		}
	}
	return ""
}
