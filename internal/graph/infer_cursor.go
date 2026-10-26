package graph

import (
	"path"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/knowledge"
)

// Knowledge base entries of Cursor the inference relies on (decision
// 2026-10-25, Cursor, §1.1).
const (
	kbCursorFileRead    = "file_tools_read"
	kbCursorFileWrite   = "file_tools_write_workspace"
	kbCursorProtected   = "sandbox_protected_paths"
	kbCursorIgnored     = "default_ignore_file_tools"
	kbCursorRunMode     = "run_mode_builtin"
	permissionModeAutoR = "auto_review"
)

// AssumeCodeLauncherRule is the rules layer's assumption of a way through
// the launcher table (D4), on Cursor's terminalAllowlist ways too.
const AssumeCodeLauncherRule = "code_launcher_rule"

// cursorInference holds what one Cursor client's edges are inferred from.
type cursorInference struct {
	client    Node
	base      knowledge.Base
	inForce   []Node
	roots     []string
	modeCap   *Node // run_mode_builtin (Auto-review), in force
	fileRead  *Node // file_tools_read
	fileWrite *Node // file_tools_write_workspace

	protected []string // sandbox_protected_paths, relative to a workspace
	ignored   []string // default_ignore_file_tools

	readBoundary bool            // readBoundary workspace for every root (annex 2 point 8)
	insecure     map[string]bool // layer → type insecure_none
	readwrite    []string        // additionalReadwritePaths
	allowlist    []Node          // terminalAllowlist entries
}

func newCursorInference(client Node, caps []Node, nodes map[string]Node, base knowledge.Base, repoRoots []string) cursorInference {
	in := cursorInference{client: client, base: base, roots: append([]string{}, repoRoots...), insecure: map[string]bool{}}
	for _, c := range caps {
		if c.InForce {
			in.inForce = append(in.inForce, c)
		}
	}
	sort.Slice(in.inForce, func(i, j int) bool { return in.inForce[i].ID < in.inForce[j].ID })
	pd := func(id string) *Node {
		e, ok := base.Entry(id)
		if !ok {
			return nil
		}
		n, ok := nodes[productDefaultID(client.ID, e)]
		if !ok || !n.InForce {
			return nil
		}
		return &n
	}
	in.modeCap, in.fileRead, in.fileWrite = pd(kbCursorRunMode), pd(kbCursorFileRead), pd(kbCursorFileWrite)
	if e, ok := base.Entry(kbCursorProtected); ok {
		in.protected = e.Paths
	}
	if e, ok := base.Entry(kbCursorIgnored); ok {
		in.ignored = e.Paths
	}
	boundary := map[string]bool{}
	for _, c := range in.inForce {
		if c.Basis != BasisConfigured {
			continue
		}
		switch {
		case c.Rule == cursorReadBoundaryRule:
			boundary[c.AppliesIn] = true
		case c.Rule == CursorInsecureNoneRule:
			in.insecure[c.AppliesIn] = true
		case strings.HasPrefix(c.Rule, cursorReadwriteRule):
			in.readwrite = append(in.readwrite, c.TargetScope)
		case strings.HasPrefix(c.Rule, cursorTerminalRule) && c.Type == CapExecuteShell && c.Approval == ApprovalAllow:
			in.allowlist = append(in.allowlist, c)
		}
	}
	// readBoundary workspace limits the shell to the roots when the user's
	// sandbox.json sets it, or every repository's does (annex 2 point 8).
	in.readBoundary = boundary["user"]
	if !in.readBoundary && len(repoRoots) > 0 {
		all := true
		for _, r := range repoRoots {
			all = all && boundary["project:"+canonicalPath(r)]
		}
		in.readBoundary = all
	}
	return in
}

// cursorBase are the base assumptions of every Cursor way (annex 2 point 9).
func cursorBase(extra ...string) []string {
	return append([]string{AssumeProcessRunsAsUser, AssumeVersionAsDocumented, AssumeTeamPolicyUnknown}, extra...)
}

// cursorModeAssumptions are the assumptions of a way through Auto-review:
// the IDE mode is not readable (K4) and the classifier decides (§2).
func cursorModeAssumptions(extra ...string) []string {
	return cursorBase(append([]string{AssumeIDERunModeNotReadable, AssumeAutoClassifierPermits}, extra...)...)
}

func (in cursorInference) rootOf(file string) (string, bool) {
	for _, r := range in.roots {
		if within(r, file) {
			return r, true
		}
	}
	return "", false
}

// isProtected reports a file inside a root that the sandbox's always
// protected paths cover (https://cursor.com/docs/reference/sandbox.md).
func (in cursorInference) isProtected(file string) bool {
	root, ok := in.rootOf(file)
	if !ok {
		return false
	}
	rel, ok := relTo(root, file)
	if !ok {
		return false
	}
	return matchesAny(in.protected, rel)
}

// matchesAny reports a relative path that one of the patterns matches.
func matchesAny(patterns []string, rel string) bool {
	segs := strings.Split(rel, "/")
	for _, p := range patterns {
		pat := strings.TrimSuffix(p, "/")
		if strings.HasSuffix(p, "/") {
			pat += "/**"
		}
		if m, ok := globSegments(strings.Split(pat, "/"), segs); ok && m {
			return true
		}
	}
	return false
}

// ignoredAnyDepth reports a relative path an ignore pattern covers, with
// gitignore semantics for these patterns: one without a slash matches a
// name at any depth; one ending in "/" matches a folder at any depth.
func ignoredAnyDepth(patterns []string, rel string) bool {
	segs := strings.Split(rel, "/")
	for _, p := range patterns {
		dir := strings.HasSuffix(p, "/")
		name := strings.TrimSuffix(p, "/")
		for i, seg := range segs {
			if dir && i == len(segs)-1 {
				break // a folder pattern: not the file itself
			}
			if m, err := path.Match(name, seg); err == nil && m {
				return true
			}
		}
	}
	return false
}

// insecureFor reports a sandbox type insecure_none in force for a file: the
// user's, or its repository's.
func (in cursorInference) insecureFor(file string) bool {
	if in.insecure["user"] {
		return true
	}
	root, ok := in.rootOf(file)
	return ok && in.insecure["project:"+canonicalPath(root)]
}

// allowlistCommand is the command of a terminalAllowlist entry, lowercase,
// without ".exe", and whether the entry has nothing after it (a bare
// prefix covers every argument; "npm:install*" names the command before
// ":"; https://cursor.com/docs/reference/permissions.md).
func allowlistCommand(entry string) (cmd, rest string) {
	e := strings.TrimSpace(entry)
	if i := strings.IndexAny(e, " :"); i >= 0 {
		cmd, rest = e[:i], strings.TrimSpace(e[i+1:])
	} else {
		cmd = e
	}
	cmd = strings.TrimSuffix(strings.ToLower(cmd), ".exe")
	if rest == "*" {
		rest = ""
	}
	return cmd, rest
}

// CursorAllowlistClass classifies a terminalAllowlist entry (annex 2 point
// 2; D3/E8 for launchers): it reads files, sends to the network, or runs
// code (a launcher whose code-taking form the prefix covers).
func CursorAllowlistClass(entry string, base knowledge.Base) (reads, network, launcher bool) {
	cmd, rest := allowlistCommand(entry)
	for _, c := range base.ReadCommands {
		reads = reads || c.Program == cmd
	}
	for _, c := range base.NetworkCommands {
		network = network || c.Program == cmd
	}
	for _, l := range base.CodeLaunchers {
		if l.Program != cmd {
			continue
		}
		first := strings.Fields(rest)
		switch {
		case rest == "":
			launcher = true // a bare prefix covers the code-taking form
		case l.Family == "second_shell" || l.Family == "inline_code":
			for _, o := range l.Options {
				launcher = launcher || strings.EqualFold(first[0], o)
			}
		case l.Family == "npx":
			launcher = launcher || l.Package != "" && first[0] == l.Package
		case l.Family == "env_launcher":
			launcher = launcher || first[0] == l.Subcommand
		}
	}
	return reads, network, launcher
}

// reaches infers, for every credential, one reaches edge per way Cursor can
// read a file that holds it (decision 2026-10-25, Cursor, §2.1, annex 2):
//   - Auto-review's shell (run_mode_builtin): any file the user can read,
//     because readBoundary defaults to system; only the roots with
//     readBoundary workspace (annex 2 point 8). Its assumptions: the mode,
//     the classifier and sandbox_platform_unknown;
//   - a terminalAllowlist entry that reads files or runs code: any file,
//     with no classifier (annex 2 point 2); a launcher adds
//     code_launcher_rule;
//   - the file tools (file_tools_read): a file inside a root not covered by
//     the default ignore list (.env*, .git/; annex 3 point 2).
func (in cursorInference) reaches(all []Node) []Edge {
	var out []Edge
	for _, cred := range all {
		if cred.Kind != KindCredential {
			continue
		}
		for _, w := range in.readWays(credentialFiles(cred)) {
			out = append(out, in.edge(EdgeReaches, cred.ID, w.cap, w.assumptions))
		}
	}
	return out
}

// cursorWay is one way the Cursor inference finds: the capability it goes
// through and the assumptions of its edge.
type cursorWay struct {
	cap         Node
	assumptions []string
}

// readWays are the ways Cursor reads one of files, as reaches describes (the
// edges of a credential holding them). Probe asks it the same.
func (in cursorInference) readWays(files []string) []cursorWay {
	var out []cursorWay
	usedBy := func(use func(f string, inside bool) bool) bool {
		for _, f := range files {
			_, inside := in.rootOf(f)
			if use(f, inside) {
				return true
			}
		}
		return false
	}
	if in.fileRead != nil && len(in.ignored) > 0 && usedBy(func(f string, inside bool) bool {
		if !inside {
			return false
		}
		root, _ := in.rootOf(f)
		rel, _ := relTo(root, f)
		return !ignoredAnyDepth(in.ignored, rel)
	}) {
		out = append(out, cursorWay{*in.fileRead, cursorBase()})
	}
	if in.modeCap != nil && usedBy(func(_ string, inside bool) bool { return inside || !in.readBoundary }) {
		out = append(out, cursorWay{*in.modeCap, cursorModeAssumptions(AssumeSandboxPlatformUnknown)})
	}
	if len(files) == 0 {
		return out
	}
	for _, c := range in.allowlist {
		reads, _, launcher := CursorAllowlistClass(c.TargetScope, in.base)
		switch {
		case reads:
			out = append(out, cursorWay{c, cursorBase()})
		case launcher:
			out = append(out, cursorWay{c, cursorBase(AssumeCodeLauncherRule)})
		}
	}
	return out
}

// writes infers, for every agent_config, agent_instructions and
// agent_executable resource with a file, one writes_config_of edge per way
// Cursor can write it without a person (decision 2026-10-25, Cursor, §2.2,
// annex 2 point 7):
//   - the file tools (file_tools_write_workspace): agent_instructions inside
//     a root and outside the protected paths, with
//     config_file_protection_scope_unknown;
//   - Auto-review's shell: inside a root outside the protected paths (or
//     with sandbox type insecure_none), or in additionalReadwritePaths, with
//     sandbox_platform_unknown; outside the roots, the retry outside the
//     sandbox the classifier reviews, with shell_write_unchecked;
//   - a terminalAllowlist launcher: any file, with code_launcher_rule and
//     shell_write_unchecked.
func (in cursorInference) writes(all []Node) []Edge {
	var out []Edge
	for _, res := range all {
		if res.Kind != KindResource || !isAgentResource(res.Type) {
			continue
		}
		file, ok := resourceFile(res.Path)
		if !ok {
			continue
		}
		for _, w := range in.writeWays(file, res.Type) {
			out = append(out, in.edge(EdgeWritesConfigOf, res.ID, w.cap, w.assumptions))
		}
	}
	return out
}

// writeWays are the ways Cursor writes file without a person, as writes
// describes for a resource of type resType (the edges of a resource in it);
// resType "" is a file that is no agent resource, which only Probe asks.
func (in cursorInference) writeWays(file, resType string) []cursorWay {
	var out []cursorWay
	_, inside := in.rootOf(file)
	protected := in.isProtected(file)
	switch {
	case in.fileWrite != nil && resType == ResourceAgentInstructions && inside && !protected:
		out = append(out, cursorWay{*in.fileWrite, cursorBase(AssumeConfigFileProtectionUnknown)})
	case in.fileWrite != nil && resType == "" && inside && !protected:
		// A file that is no agent resource (decision 2026-10-25, cómo
		// trabajan tus agentes, cálculo, annex 2, point 3): the file tools
		// "modify workspace files without approval, except for configuration
		// files". Infer has only agent resources; Probe asks with "".
		out = append(out, cursorWay{*in.fileWrite, cursorBase()})
	}
	if in.modeCap != nil {
		readwrite := false
		for _, p := range in.readwrite {
			readwrite = readwrite || within(p, file)
		}
		switch {
		case inside && (!protected || in.insecureFor(file)), !inside && readwrite:
			out = append(out, cursorWay{*in.modeCap, cursorModeAssumptions(AssumeSandboxPlatformUnknown)})
		case !inside:
			out = append(out, cursorWay{*in.modeCap, cursorModeAssumptions(AssumeShellWriteUnchecked)})
		}
	}
	for _, c := range in.allowlist {
		if _, _, launcher := CursorAllowlistClass(c.TargetScope, in.base); launcher {
			out = append(out, cursorWay{c, cursorBase(AssumeCodeLauncherRule, AssumeShellWriteUnchecked)})
		}
	}
	return out
}

// edge builds an inferred edge from the client through c, keyed as Claude
// Code's.
func (in cursorInference) edge(typ, to string, c Node, assumptions []string) Edge {
	e := Edge{
		ID:          "edge_" + typ + "_" + keyHash(typ, in.client.ID, to, c.ID),
		Type:        typ,
		From:        in.client.ID,
		To:          to,
		Source:      c.Source,
		Certainty:   CertaintyInferred,
		DerivedFrom: []string{in.client.ID, c.ID, to},
		Assumptions: append([]string{}, assumptions...),
	}
	if typ == EdgeReaches {
		e.Via = c.ID
	}
	return e
}
