package graph

import (
	"fmt"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/knowledge"
)

// Assumption codes of inferred edges (graph schema, assumptions).
const (
	AssumeProcessRunsAsUser      = "process_runs_as_user"
	AssumeVersionAsDocumented    = "version_behaves_as_documented"
	AssumeBashToolAvailable      = "bash_tool_available"
	AssumeNoPreToolUseHookBlocks = "no_pretooluse_hook_blocks"
	AssumeAutoClassifierPermits  = "auto_classifier_permits"
	AssumeReadDenyPartial        = "read_deny_partial"
	AssumeShellWriteUnchecked    = "shell_write_unchecked"
	AssumeEditRulePartial        = "edit_rule_partial"
	AssumeAutoModeAvailable      = "auto_mode_available"
	AssumeUIModeChoiceUnknown    = "ui_mode_choice_unknown"

	// Cursor (capacidades de Cursor, K3 with the annex).
	AssumeIDERunModeNotReadable        = "ide_run_mode_not_readable"
	AssumeThirdPartyImportsNotReadable = "third_party_imports_setting_not_readable"
	AssumeSandboxPlatformUnknown       = "sandbox_platform_unknown"
	AssumeConfigFileProtectionUnknown  = "config_file_protection_scope_unknown"
	AssumeTeamPolicyUnknown            = "team_policy_unknown"
	permissionModeAcceptEdits          = "acceptEdits"
	permissionModeDefault              = "default"
	ruleAdditionalDirectoriesPref      = "additionalDirectories="
	ruleBlockReadsOutside              = "blockReadsOutsideWorkingDirectories=true"
)

// Knowledge base entries the inference relies on.
const (
	kbShellReadonly  = "shell_readonly_bash_commands"
	kbReadInWorkDirs = "read_in_working_dirs"
	kbProtectedPaths = "protected_paths"
)

// Infer adds the reaches and writes_config_of edges of every Claude Code
// ai_client of g (decision 2026-10-23, Q5–Q7) and of every Cursor one
// (decision 2026-10-25, Cursor, K4–K8). It is a pure function over the
// built graph and runs before Encode. Other agents have no knowledge base
// and get no inferred edge. Only capabilities in force count, and ask never
// yields an edge.
//
// repoRoots are the configured repository roots; home is the user's home
// folder ("~/" in permission patterns and protected paths).
func Infer(g Graph, repoRoots []string, home string) (Graph, error) {
	p, err := prepare(g)
	if err != nil {
		return Graph{}, err
	}
	edges := map[string]Edge{}
	for _, e := range g.Edges {
		edges[e.ID] = e
	}
	for _, client := range g.Nodes {
		if client.Kind != KindAgent || client.Type != AgentAIClient {
			continue
		}
		var inferred []Edge
		switch client.Platform {
		case p.claude.Product:
			in := p.claudeInference(client, repoRoots, home)
			inferred = append(in.reaches(g.Nodes), in.writes(g.Nodes)...)
		case knowledge.CursorProduct:
			in := p.cursorInference(client, repoRoots)
			inferred = append(in.reaches(g.Nodes), in.writes(g.Nodes)...)
		case knowledge.HermesProduct:
			in := p.hermesInference(client)
			inferred = append(in.reaches(g.Nodes), in.writes(g.Nodes)...)
		}
		for _, e := range inferred {
			if _, dup := edges[e.ID]; !dup {
				edges[e.ID] = e
			}
		}
	}
	// A Claude Code hook that Cursor loads rests on Cursor's Third-Party
	// Imports setting, on by default and not readable at rest (annex).
	for id, e := range edges {
		if r, ok := p.nodes[e.To]; ok && e.Type == EdgeWritesConfigOf && r.Type == ResourceAgentExecutable && r.ID == CursorImportedHookID(r.Path, r.Configures) {
			e.Assumptions = append(e.Assumptions, AssumeThirdPartyImportsNotReadable)
			edges[id] = e
		}
	}
	g.Edges = g.Edges[:0:0]
	for _, e := range edges {
		g.Edges = append(g.Edges, e)
	}
	sort.Slice(g.Edges, func(i, j int) bool { return g.Edges[i].ID < g.Edges[j].ID })
	return g, nil
}

// prepared are the inputs every inference of a graph starts from: the
// knowledge bases, Claude Code's protected paths, the nodes by id and each
// client's capabilities. Infer and Probe build their inferences from it, so
// both see exactly the same inputs.
type prepared struct {
	claude, cursor knowledge.Base
	prot           knowledge.Entry
	nodes          map[string]Node
	caps           map[string][]Node
}

func prepare(g Graph) (prepared, error) {
	bases, err := knowledge.All()
	if err != nil {
		return prepared{}, fmt.Errorf("knowledge base: %w", err)
	}
	p := prepared{claude: bases[knowledge.ClaudeCodeProduct], cursor: bases[knowledge.CursorProduct], nodes: make(map[string]Node, len(g.Nodes)), caps: map[string][]Node{}}
	prot, ok := p.claude.Entry(kbProtectedPaths)
	if !ok {
		return prepared{}, fmt.Errorf("knowledge base: no %s entry", kbProtectedPaths)
	}
	p.prot = prot
	for _, n := range g.Nodes {
		p.nodes[n.ID] = n
	}
	for _, e := range g.Edges {
		if e.Type == EdgeHas {
			if c, ok := p.nodes[e.To]; ok && c.Kind == KindCapability {
				p.caps[e.From] = append(p.caps[e.From], c)
			}
		}
	}
	return p, nil
}

func (p prepared) claudeInference(client Node, repoRoots []string, home string) inference {
	return newInference(client, p.caps[client.ID], p.nodes, p.claude, p.prot, repoRoots, home)
}

func (p prepared) cursorInference(client Node, repoRoots []string) cursorInference {
	return newCursorInference(client, p.caps[client.ID], p.nodes, p.cursor, repoRoots)
}

// inference holds what one Claude Code client's edges are inferred from.
type inference struct {
	client    Node
	inForce   []Node // capabilities in force
	mode      string // effective permission mode
	modeCap   *Node  // the permission_mode capability of mode, if configured
	roots     []string
	home      string
	protected protectedPaths
	bashVia   *Node // product_default shell_readonly_bash_commands
	fileVia   *Node // product_default read_in_working_dirs

	launchers []knowledge.CodeLauncher // the base's code launchers (D3)
}

func newInference(client Node, caps []Node, nodes map[string]Node, base knowledge.Base, prot knowledge.Entry, repoRoots []string, home string) inference {
	in := inference{client: client, home: home, protected: protectedPaths{entry: prot, configDir: client.ConfigPath, home: home}, launchers: base.CodeLaunchers}
	for _, c := range caps {
		if c.InForce {
			in.inForce = append(in.inForce, c)
		}
	}
	sort.Slice(in.inForce, func(i, j int) bool { return in.inForce[i].ID < in.inForce[j].ID })
	in.mode, in.modeCap = effectiveMode(in.inForce)
	in.roots = workRoots(repoRoots, in.inForce, home)
	pd := func(id string) *Node {
		e, ok := base.Entry(id)
		if !ok {
			return nil
		}
		n, ok := nodes[productDefaultID(client.ID, e)]
		if !ok {
			return nil
		}
		return &n
	}
	in.bashVia, in.fileVia = pd(kbShellReadonly), pd(kbReadInWorkDirs)
	return in
}

// effectiveMode is the defaultMode of the permission_mode allow capability in
// force of highest precedence, managed over user (project and local do not
// set the global mode), and then the built-in default mode of the product
// (permission_mode allow, product_default; decision 2026-10-24, D5). A mode
// that a permission_mode deny in force disables is not effective: the mode
// is then "default", as when none is set. IDE-only layers (ide:vscode) do
// not set the terminal's mode.
func effectiveMode(inForce []Node) (string, *Node) {
	var chosen *Node
	pick := func(match func(Node) bool) {
		for i := range inForce {
			c := inForce[i]
			if chosen == nil && c.Type == CapPermissionMode && c.Approval == ApprovalAllow && match(c) {
				chosen = &inForce[i]
			}
		}
	}
	for _, layer := range []string{"managed", "user"} {
		pick(func(c Node) bool { return c.Basis == BasisConfigured && c.AppliesIn == layer })
	}
	pick(func(c Node) bool { return c.Basis == BasisProductDefault })
	if chosen == nil {
		return permissionModeDefault, nil
	}
	for _, c := range inForce {
		if c.Type == CapPermissionMode && c.Approval == ApprovalDeny && c.TargetScope == chosen.TargetScope {
			return permissionModeDefault, nil
		}
	}
	return chosen.TargetScope, chosen
}

// workRoots are the configured repository roots and the target_scope of
// every read_files allow in force from additionalDirectories. A "~/" folder
// is resolved against home; a relative one against the repository of a
// project or local layer (it is dropped in user and managed layers, which
// have no fixed directory).
func workRoots(repoRoots []string, inForce []Node, home string) []string {
	roots := append([]string{}, repoRoots...)
	for _, c := range inForce {
		if c.Type != CapReadFiles || c.Approval != ApprovalAllow || !strings.HasPrefix(c.Rule, ruleAdditionalDirectoriesPref) {
			continue
		}
		dir := c.TargetScope
		switch {
		case strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`):
			dir = home + "/" + dir[2:]
		case strings.HasPrefix(dir, "/") || strings.HasPrefix(dir, `\`) || isWindowsPath(dir) && len(dir) >= 2 && dir[1] == ':':
		default:
			root, ok := ruleScope{appliesIn: c.AppliesIn}.scopeRoot()
			if !ok {
				continue
			}
			dir = root + "/" + dir
		}
		roots = append(roots, dir)
	}
	return roots
}

// modeAssumptions are the assumptions of every way through the effective
// mode (decision 2026-10-24, D6): the mode chosen in an IDE or the desktop
// app is not readable at rest (ui_mode_choice_unknown), and the built-in
// auto mode needs a supported model and no server-side switch-off
// (auto_mode_available).
func (in inference) modeAssumptions() []string {
	a := []string{AssumeUIModeChoiceUnknown}
	if in.modeCap != nil && in.modeCap.Basis == BasisProductDefault && in.mode == permissionModeAuto {
		a = append(a, AssumeAutoModeAvailable)
	}
	return a
}

func (in inference) withinRoots(file string) bool {
	for _, r := range in.roots {
		if within(r, file) {
			return true
		}
	}
	return false
}

func (in inference) scopeOf(c Node) ruleScope {
	return ruleScope{appliesIn: c.AppliesIn, configDir: in.client.ConfigPath, home: in.home, roots: in.roots}
}

// isRule reports a rule of tool: "Tool" or "Tool(...)".
func isRule(rule, tool string) bool {
	return rule == tool || strings.HasPrefix(rule, tool+"(")
}

// via is one way an inferred edge exists.
type via struct {
	cap         Node
	assumptions []string
}

func baseAssumptions(extra ...string) []string {
	return append([]string{AssumeProcessRunsAsUser, AssumeVersionAsDocumented}, extra...)
}

// The deny and ask rules that remove or qualify a way (decision BV-29, D1):
//   - R(f): a read_files deny or ask in force (a Read rule) covers f;
//   - E(f): a write_files deny or ask in force covers f, or a read_files deny
//     in force does ("A Read deny rule also blocks the Edit and Write tools
//     on the same path", permissions, "Read and Edit");
//   - shell(tool): an execute_shell deny or ask in force for the tool
//     without a specifier (target_scope "*"): B for Bash, S for the tool of
//     a shell way.
//
// Only configured rules count: the product defaults (edit_default and
// shell_default ask "*", read_outside_working_dirs) are what the permission
// modes and allow rules override. Each returns the ids of the capabilities
// that apply, in id order.

func (in inference) readRules(f string) []string {
	var ids []string
	for _, c := range in.inForce {
		if c.Basis == BasisConfigured && c.Type == CapReadFiles && (c.Approval == ApprovalDeny || c.Approval == ApprovalAsk) && isRule(c.Rule, "Read") &&
			matchRulePath(c.TargetScope, c.Approval, in.scopeOf(c), f) {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

func (in inference) editRules(f string) []string {
	var ids []string
	for _, c := range in.inForce {
		write := c.Type == CapWriteFiles && (c.Approval == ApprovalDeny || c.Approval == ApprovalAsk)
		readDeny := c.Type == CapReadFiles && c.Approval == ApprovalDeny && isRule(c.Rule, "Read")
		if c.Basis == BasisConfigured && (write || readDeny) && matchRulePath(c.TargetScope, c.Approval, in.scopeOf(c), f) {
			ids = append(ids, c.ID)
		}
	}
	return ids
}

func (in inference) shellRules(tool string) bool {
	for _, c := range in.inForce {
		if c.Basis == BasisConfigured && c.Type == CapExecuteShell && (c.Approval == ApprovalDeny || c.Approval == ApprovalAsk) &&
			isRule(c.Rule, tool) && c.TargetScope == scopeAll {
			return true
		}
	}
	return false
}

// launcher reports a configured execute_shell allow in force, with a
// specifier, that is a code launcher (D3, knowledge.LauncherOf: the same
// detection as the rules layer's egress), its tool not denied or asked
// without specifier.
func (in inference) launcher(c Node) bool {
	if c.Type != CapExecuteShell || c.Approval != ApprovalAllow || c.Basis != BasisConfigured || c.TargetScope == scopeAll ||
		in.shellRules(shellTool(c.Rule)) {
		return false
	}
	_, ok := knowledge.LauncherOf(c.TargetScope, in.launchers)
	return ok
}

// shellTool is the shell tool of an execute_shell rule: Bash or PowerShell.
func shellTool(rule string) string {
	if isRule(rule, "PowerShell") {
		return "PowerShell"
	}
	return "Bash"
}

// union adds the ids of add missing from ids, keeping order.
func union(ids, add []string) []string {
	for _, a := range add {
		if !contains(ids, a) {
			ids = append(ids, a)
		}
	}
	return ids
}

// reaches infers, for every credential, one reaches edge per way Claude Code
// can read a file f that holds it (decision Q5, BV-28 and BV-29 D1). A way
// applies when its condition holds for at least one f:
//   - the file tools (read_in_working_dirs): f inside a working root and not
//     R(f);
//   - Bash read-only commands: not B, and f inside a working root or
//     blockReadsOutsideWorkingDirectories not in force;
//   - bypassPermissions: f inside a working root or blockReads not in force;
//   - auto: f inside a working root (the first read outside them prompts:
//     permission-modes, "How auto mode evaluates actions", step 2).
//
// On the Bash and mode ways, read_deny_partial is added when R(f) holds for
// some f the way uses, and those rules join derived_from.
func (in inference) reaches(all []Node) []Edge {
	var out []Edge
	for _, cred := range all {
		if cred.Kind != KindCredential {
			continue
		}
		for _, w := range in.readWays(credentialFiles(cred)) {
			out = append(out, in.edge(EdgeReaches, cred.ID, w.v, w.assumptions, w.rules))
		}
	}
	return out
}

// inferredWay is one way the inference finds for a file or set of files: the
// via, the assumptions of its edge and the rules it derives from beyond the
// client, the capability and the target.
type inferredWay struct {
	v                  via
	assumptions, rules []string
}

// readWays are the ways Claude Code reads one of files, as reaches describes
// (the edges of a credential holding them). Probe asks it the same.
func (in inference) readWays(files []string) []inferredWay {
	blockReads := false
	for _, c := range in.inForce {
		blockReads = blockReads || c.Rule == ruleBlockReadsOutside
	}
	// use reports whether a way uses f, and collects the rules R(f) of
	// the files it uses.
	way := func(use func(f string, inside bool) bool) (bool, []string) {
		used, rules := false, []string(nil)
		for _, f := range files {
			if use(f, in.withinRoots(f)) {
				used = true
				rules = union(rules, in.readRules(f))
			}
		}
		return used, rules
	}
	var out []inferredWay
	add := func(v via, partial bool, rules []string) {
		a := append([]string{}, v.assumptions...)
		if partial && len(rules) > 0 {
			a = append(a, AssumeReadDenyPartial)
		} else {
			rules = nil
		}
		out = append(out, inferredWay{v, a, rules})
	}
	if in.fileVia != nil {
		if ok, _ := way(func(f string, inside bool) bool { return inside && len(in.readRules(f)) == 0 }); ok {
			add(via{*in.fileVia, baseAssumptions(AssumeNoPreToolUseHookBlocks)}, false, nil)
		}
	}
	if in.bashVia != nil && !in.shellRules("Bash") {
		if ok, rules := way(func(_ string, inside bool) bool { return inside || !blockReads }); ok {
			add(via{*in.bashVia, baseAssumptions(AssumeBashToolAvailable, AssumeNoPreToolUseHookBlocks)}, true, rules)
		}
	}
	if in.modeCap != nil {
		switch in.mode {
		case permissionModeBypass:
			if ok, rules := way(func(_ string, inside bool) bool { return inside || !blockReads }); ok {
				add(via{*in.modeCap, append(baseAssumptions(), in.modeAssumptions()...)}, true, rules)
			}
		case permissionModeAuto:
			if ok, rules := way(func(_ string, inside bool) bool { return inside }); ok {
				add(via{*in.modeCap, append(baseAssumptions(AssumeAutoClassifierPermits), in.modeAssumptions()...)}, true, rules)
			}
		}
	}
	return out
}

// writes infers, for every agent_config, agent_instructions and
// agent_executable resource of any agent (Claude Code's own included), one
// writes_config_of edge per way Claude Code can write its file p without
// approval (decision Q7, BV-28 and BV-29 D1):
//   - acceptEdits: p inside a working root, not protected and not E(p);
//   - an Edit (write_files) allow in force whose pattern covers p, when p is
//     not protected and not E(p);
//   - bypassPermissions: any p;
//   - auto: any p that is not protected;
//   - an execute_shell allow in force with target_scope "*": any p, unless S
//     for its tool;
//   - a configured execute_shell allow in force that is a code launcher (D3):
//     any p, as "*", with code_launcher_rule (decision 2026-10-26,
//     lanzadores en escritura, L1).
//
// On the bypassPermissions, auto, shell and launcher ways, E(p) keeps the edge with
// edit_rule_partial and the rules in derived_from. A resource with no file
// (a registry value) gets no edge.
func (in inference) writes(all []Node) []Edge {
	var out []Edge
	for _, res := range all {
		if res.Kind != KindResource || !isAgentResource(res.Type) {
			continue
		}
		file, ok := resourceFile(res.Path)
		if !ok {
			continue
		}
		for _, w := range in.writeWays(file) {
			out = append(out, in.edge(EdgeWritesConfigOf, res.ID, w.v, w.assumptions, w.rules))
		}
	}
	return out
}

// writeWays are the ways Claude Code writes file without approval, as writes
// describes (the edges of a resource in it). Probe asks it the same.
func (in inference) writeWays(file string) []inferredWay {
	var out []inferredWay
	protected := in.protected.protected(file)
	edit := in.editRules(file)
	hook := func(extra ...string) []string {
		return baseAssumptions(append([]string{AssumeNoPreToolUseHookBlocks}, extra...)...)
	}
	// blocked: the way needs no deny or ask on p. partial: E(p) keeps
	// the way, with edit_rule_partial.
	blocked := func(v via) {
		if len(edit) == 0 {
			out = append(out, inferredWay{v, v.assumptions, nil})
		}
	}
	partial := func(v via) {
		a, rules := v.assumptions, []string(nil)
		if len(edit) > 0 {
			a, rules = append(append([]string{}, a...), AssumeEditRulePartial), edit
		}
		out = append(out, inferredWay{v, a, rules})
	}
	if in.modeCap != nil {
		switch {
		case in.mode == permissionModeAcceptEdits && in.withinRoots(file) && !protected:
			blocked(via{*in.modeCap, hook(in.modeAssumptions()...)})
		case in.mode == permissionModeBypass:
			partial(via{*in.modeCap, hook(in.modeAssumptions()...)})
		case in.mode == permissionModeAuto && !protected:
			partial(via{*in.modeCap, hook(append([]string{AssumeAutoClassifierPermits}, in.modeAssumptions()...)...)})
		}
	}
	for _, c := range in.inForce {
		switch {
		case c.Type == CapWriteFiles && c.Approval == ApprovalAllow && !protected &&
			matchRulePath(c.TargetScope, c.Approval, in.scopeOf(c), file):
			blocked(via{c, hook()})
		case c.Type == CapExecuteShell && c.Approval == ApprovalAllow && c.TargetScope == scopeAll &&
			!in.shellRules(shellTool(c.Rule)):
			partial(via{c, hook(AssumeShellWriteUnchecked)})
		case in.launcher(c):
			// A configured allow that is a code launcher runs any code, so
			// it writes like "shell *" (decision 2026-10-26, lanzadores en
			// escritura, L1).
			partial(via{c, hook(AssumeCodeLauncherRule, AssumeShellWriteUnchecked)})
		}
	}
	return out
}

// edge builds an inferred edge from the client to to through v. Its id is
// keyed by type, from, to and via.
func (in inference) edge(typ, to string, v via, assumptions, extra []string) Edge {
	e := Edge{
		ID:          "edge_" + typ + "_" + keyHash(typ, in.client.ID, to, v.cap.ID),
		Type:        typ,
		From:        in.client.ID,
		To:          to,
		Source:      v.cap.Source,
		Certainty:   CertaintyInferred,
		DerivedFrom: append([]string{in.client.ID, v.cap.ID, to}, extra...),
		Assumptions: append([]string{}, assumptions...),
	}
	if typ == EdgeReaches {
		e.Via = v.cap.ID
	}
	return e
}

func isAgentResource(t string) bool {
	return t == ResourceAgentConfig || t == ResourceAgentInstructions || t == ResourceAgentExecutable
}

// resourceFile is the file of a resource path: the part before "#". A
// "registry:" path has no file.
func resourceFile(p string) (string, bool) {
	if strings.HasPrefix(p, "registry:") {
		return "", false
	}
	file, _, _ := strings.Cut(p, "#")
	return file, file != ""
}

// credentialFiles are the distinct files of a credential's locations:
// "<file>#<pointer>" or, from the credential files collector,
// "<file>:<line>".
func credentialFiles(cred Node) []string {
	seen := map[string]bool{}
	var out []string
	for _, loc := range cred.Locations {
		file, ok := resourceFile(loc)
		if !strings.Contains(loc, "#") {
			file, ok = stripLine(loc), true
		}
		if ok && !seen[file] {
			seen[file] = true
			out = append(out, file)
		}
	}
	return out
}

// stripLine removes a final ":<line>" from a location.
func stripLine(loc string) string {
	i := strings.LastIndexByte(loc, ':')
	if i <= 0 || i == len(loc)-1 {
		return loc
	}
	for _, c := range loc[i+1:] {
		if c < '0' || c > '9' {
			return loc
		}
	}
	return loc[:i]
}
