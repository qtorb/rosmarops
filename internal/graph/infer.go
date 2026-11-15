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

	// AssumeReviewerModelPermits mirrors rules.AssumeReviewerModelPermits (the
	// shared reviewer_model_permits code, BV-140 decision 6: a vendor reviewer
	// model decides the escalation — Codex auto-review, Antigravity agentMode
	// auto, Copilot Assisted). Defined here too because graph cannot import
	// rules; the string is the contract and a test keeps them equal.
	AssumeReviewerModelPermits = "reviewer_model_permits"

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
// credFileInWorkspace reports whether any file a credential node lives in is
// under a repo root (BV-198 M1). A credential carries its files in Locations as
// "<path>:<line>" or "<path>#<pointer>" (Path is empty), so filtering by
// cred.Path never matches — the bug M1 fixes in copilot.go and antigravity.go.
func credFileInWorkspace(cred Node, inWorkspace func(string) bool) bool {
	for _, loc := range cred.Locations {
		if inWorkspace(ParseLocation(loc)) {
			return true
		}
	}
	return false
}

// ParseLocation returns the file of an evidence location, which is
// "<path>#<pointer>" or "<path>:<line>" (BV-202 S4). The single parser the
// inference and the credential-file listing share: it tries "#" first, then a
// final ":" only when it is followed by a line number. A Windows drive colon
// (C:\...) is therefore never a separator, so the file of
// "C:\x\mcp.json#/s/env/T" is "C:\x\mcp.json", not "C".
func ParseLocation(loc string) string {
	if i := strings.IndexByte(loc, '#'); i >= 0 {
		return loc[:i]
	}
	if i := strings.LastIndexByte(loc, ':'); i > 0 && allDigits(loc[i+1:]) {
		return loc[:i]
	}
	return loc
}

// allDigits reports a non-empty run of ASCII digits.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

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
		case knowledge.CodexProduct:
			in := p.codexInference(client, repoRoots)
			inferred = append(in.reaches(g.Nodes), in.writes(g.Nodes)...)
		case knowledge.GrokBuildProduct:
			in := p.grokInference(client, repoRoots)
			inferred = append(in.reaches(g.Nodes), in.writes(g.Nodes)...)
		case knowledge.GitHubCopilotProduct:
			in := p.copilotInference(client, repoRoots)
			inferred = append(in.reaches(g.Nodes), in.writes(g.Nodes)...)
		case knowledge.AntigravityCLIProduct:
			in := p.antigravityInference(client, repoRoots)
			inferred = append(in.reaches(g.Nodes), in.writes(g.Nodes)...)
		case knowledge.GeminiCLIProduct:
			in := p.geminiInference(client, repoRoots)
			inferred = append(in.reaches(g.Nodes), in.writes(g.Nodes)...)
		case knowledge.ClineProduct:
			in := p.clineInference(client, repoRoots)
			inferred = append(in.reaches(g.Nodes), in.writes(g.Nodes)...)
		}
		for _, e := range inferred {
			if _, dup := edges[e.ID]; !dup {
				edges[e.ID] = e
			}
		}
	}
	// BV-208 C2 (CTO-26 §1 eje 3): Grok Build reads another product's config at
	// rest — Claude Code's settings always, and Cursor's hooks by default compat
	// (CTO-16 Grok §4–§5, modelled in FromGrok). One reads_config_of edge from
	// each Grok client to each Claude Code and Cursor client present.
	var grokClients, foreignTargets []string
	for _, n := range g.Nodes {
		if n.Kind != KindAgent || n.Type != AgentAIClient {
			continue
		}
		switch n.Platform {
		case knowledge.GrokBuildProduct:
			grokClients = append(grokClients, n.ID)
		case p.claude.Product, knowledge.CursorProduct:
			foreignTargets = append(foreignTargets, n.ID)
		}
	}
	for _, from := range grokClients {
		for _, to := range foreignTargets {
			e := Edge{
				ID: edgeID(EdgeReadsConfigOf, from, to), Type: EdgeReadsConfigOf, From: from, To: to,
				Source: "grok:reads_foreign_config", Certainty: CertaintyInferred,
				DerivedFrom: []string{from, to}, Assumptions: []string{},
			}
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
	claude, cursor, codex knowledge.Base
	prot                  knowledge.Entry
	nodes                 map[string]Node
	caps                  map[string][]Node
}

func prepare(g Graph) (prepared, error) {
	bases, err := knowledge.All()
	if err != nil {
		return prepared{}, fmt.Errorf("knowledge base: %w", err)
	}
	p := prepared{claude: bases[knowledge.ClaudeCodeProduct], cursor: bases[knowledge.CursorProduct], codex: bases[knowledge.CodexProduct], nodes: make(map[string]Node, len(g.Nodes)), caps: map[string][]Node{}}
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
	inForce   []Node          // capabilities in force
	sessions  []claudeSession // one per project (BV-232 §4): its mode and sandbox
	addDirs   []string        // additionalDirectories, a working root of every session
	roots     []string        // the project roots and addDirs
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
	in.sessions = claudeSessions(in.inForce, repoRoots, home)
	in.addDirs = workRoots(nil, in.inForce, home)
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
func modeAssumptions(s claudeSession) []string {
	a := []string{AssumeUIModeChoiceUnknown}
	if s.modeCap != nil && s.modeCap.Basis == BasisProductDefault && s.mode == permissionModeAuto {
		a = append(a, AssumeAutoModeAvailable)
	}
	return a
}

// insideSession reports file is inside one of the working roots of a session
// in s.root: the project itself and the additional directories.
func (in inference) insideSession(s claudeSession, file string) bool {
	if s.root != "" && within(s.root, file) {
		return true
	}
	for _, r := range in.addDirs {
		if within(r, file) {
			return true
		}
	}
	return false
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
// (the edges of a credential holding them). Probe asks it the same. Each way
// is evaluated in every session (BV-232 §4): a file is inside a session when
// it is in its project or an additional directory.
//
// A Read deny or ask rule covers the built-in file tools and the file
// commands Claude Code recognizes in Bash (cat, head, tail, sed, tee and
// redirections), not a program the agent launches that opens files itself
// (permissions#read-and-edit, BV-232 §2): on a covered file the read-only Bash
// way is cut, and a program way stays open where the agent can launch a
// program without a person (bypassPermissions, auto, a Bash "*" allow or a
// code launcher allow), marked read_deny_partial. An effective sandbox (only
// on macOS, Linux and WSL2, sure and closed, BV-232 §1) merges the Read deny
// rules and its own denyRead paths into what every command can read, so it
// cuts the program way too, and the Bash way on its denyRead paths.
func (in inference) readWays(files []string) []inferredWay {
	covered := map[string][]string{} // file -> the Read rules covering it
	for _, f := range files {
		if r := in.readRules(f); len(r) > 0 {
			covered[f] = r
		}
	}
	// acc gathers one way per capability across sessions: whether it is
	// used, its assumptions, and the rules of the covered files it uses.
	type acc struct {
		v       via
		used    bool
		partial bool
		extra   []string
		rules   []string
	}
	var order []string
	ways := map[string]*acc{}
	use := func(v via, partial bool, rules []string, extra ...string) {
		a := ways[v.cap.ID]
		if a == nil {
			a = &acc{v: v}
			ways[v.cap.ID] = a
			order = append(order, v.cap.ID)
		}
		a.used = true
		a.partial = a.partial || partial
		a.rules = union(a.rules, rules)
		a.extra = union(a.extra, extra)
	}
	if in.fileVia != nil {
		for _, f := range files {
			if in.withinRoots(f) && covered[f] == nil {
				use(via{*in.fileVia, baseAssumptions(AssumeNoPreToolUseHookBlocks)}, false, nil)
			}
		}
	}
	bash := in.bashVia != nil && !in.shellRules("Bash")
	for _, s := range in.sessions {
		eff := s.sandboxEffective()
		blockReads := s.blocksReadsOutside(in.inForce)
		for _, f := range files {
			inside := in.insideSession(s, f)
			cov := covered[f]
			// The read-only Bash commands: not on a covered file, nor on a
			// sandbox denyRead path of an effective sandbox.
			if bash && cov == nil && (inside || !blockReads) && !(eff && s.sandboxDenies(f, in.home)) {
				var extra []string
				// CTO-27 §3 / BV-208: in auto mode the read-only Bash commands
				// can also wait for the classifier's review.
				if s.mode == permissionModeAuto {
					extra = append(extra, AssumeAutoClassifierPermits)
				}
				use(via{*in.bashVia, baseAssumptions(AssumeBashToolAvailable, AssumeNoPreToolUseHookBlocks)}, false, nil, extra...)
			}
			// The modes: the file tools and any command, on a covered file
			// only through a program, which an effective sandbox stops.
			if s.modeCap != nil && !(cov != nil && eff) {
				switch {
				case s.mode == permissionModeBypass && (inside || !blockReads):
					use(via{*s.modeCap, append(baseAssumptions(), modeAssumptions(s)...)}, cov != nil, cov)
				case s.mode == permissionModeAuto && inside:
					use(via{*s.modeCap, append(baseAssumptions(AssumeAutoClassifierPermits), modeAssumptions(s)...)}, cov != nil, cov)
				}
			}
			// A program the agent launches without a person reads a covered
			// file, unless an effective sandbox contains it (BV-232 §2).
			if cov != nil && !eff {
				for _, c := range in.inForce {
					switch {
					case c.Type == CapExecuteShell && c.Approval == ApprovalAllow && c.Basis == BasisConfigured && c.TargetScope == scopeAll &&
						!in.shellRules(shellTool(c.Rule)):
						use(via{c, baseAssumptions(AssumeNoPreToolUseHookBlocks)}, true, cov)
					case in.launcher(c):
						use(via{c, baseAssumptions(AssumeNoPreToolUseHookBlocks, AssumeCodeLauncherRule)}, true, cov)
					}
				}
			}
		}
	}
	var out []inferredWay
	for _, id := range order {
		a := ways[id]
		assume := append(append([]string{}, a.v.assumptions...), a.extra...)
		rules := []string(nil)
		if a.partial && len(a.rules) > 0 {
			assume, rules = append(assume, AssumeReadDenyPartial), a.rules
		}
		out = append(out, inferredWay{a.v, assume, rules})
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
	// The modes, in every session (BV-232 §4), one way per mode capability.
	seen := map[string]bool{}
	for _, sess := range in.sessions {
		if sess.modeCap == nil || seen[sess.modeCap.ID] {
			continue
		}
		switch {
		case sess.mode == permissionModeAcceptEdits && in.insideSession(sess, file) && !protected:
			seen[sess.modeCap.ID] = true
			blocked(via{*sess.modeCap, hook(modeAssumptions(sess)...)})
		case sess.mode == permissionModeBypass:
			seen[sess.modeCap.ID] = true
			partial(via{*sess.modeCap, hook(modeAssumptions(sess)...)})
		case sess.mode == permissionModeAuto && !protected:
			seen[sess.modeCap.ID] = true
			partial(via{*sess.modeCap, hook(append([]string{AssumeAutoClassifierPermits}, modeAssumptions(sess)...)...)})
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
		file := ParseLocation(loc)
		if file != "" && !seen[file] {
			seen[file] = true
			out = append(out, file)
		}
	}
	return out
}
