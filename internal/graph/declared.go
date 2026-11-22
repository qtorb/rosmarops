package graph

import (
	"strings"

	"github.com/qtorb/rosmarops/internal/declared"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// BasisDeclared is a capability that comes from the person's answer about a
// setting RosmarOps cannot read at rest, or its least controlled case
// without an answer (BV-248, CTO-56; graph.v2 basis declared, Albert 16-11).
const BasisDeclared = "declared"

// The assumptions of every way that comes from a declared setting
// (CTO-56 §1.3, §4): the person's answer, or no answer.
const (
	AssumeDeclaredByYou = "declared_by_you"
	AssumeNotDeclared   = "not_declared_least_controlled"
)

// Target scopes of Cursor's declared capabilities.
const (
	// CursorScopeRunEverything: Run Everything, every way without a person
	// (CTO-56 §1.3).
	CursorScopeRunEverything = "run_everything"
	// CursorScopeAutoReview: either Auto-review mode, the knowledge base's
	// run_mode_builtin target.
	CursorScopeAutoReview = "auto_review"
	// CursorScopeExternalFiles: External-File Protection off, the file
	// tools write outside the projects (CTO-56 §1.3, C4).
	CursorScopeExternalFiles = "external_files"
	// cursorApprovalModeRule is the rule of permissions.json's approvalMode
	// (CTO-56 §1.1), read as a configured mode.
	cursorApprovalModeRule = "approvalMode="
)

// declaredSourcePrefix starts the source of a declared capability:
// "declared:<product>/<question>=<option>", with "#least" when it is the
// least controlled case and not an answer.
const (
	declaredSourcePrefix = "declared:"
	declaredLeastSuffix  = "#least"
)

func declaredSource(product, question, option string, given bool) string {
	s := declaredSourcePrefix + product + "/" + question + "=" + option
	if !given {
		s += declaredLeastSuffix
	}
	return s
}

// DeclaredOf reads a declared capability's product, question and option, and
// whether the person gave it; ok is false for any other node.
func DeclaredOf(n Node) (product, question, option string, given, ok bool) {
	if n.Basis != BasisDeclared || !strings.HasPrefix(n.Source, declaredSourcePrefix) {
		return "", "", "", false, false
	}
	s := strings.TrimPrefix(n.Source, declaredSourcePrefix)
	given = !strings.HasSuffix(s, declaredLeastSuffix)
	s = strings.TrimSuffix(s, declaredLeastSuffix)
	pq, option, found := strings.Cut(s, "=")
	if !found {
		return "", "", "", false, false
	}
	product, question, found = strings.Cut(pq, "/")
	return product, question, option, given, found
}

// DeclaredAssumption is the assumption of a way through n: declared_by_you,
// not_declared_least_controlled, or "" when n is not declared.
func DeclaredAssumption(n Node) string {
	_, _, _, given, ok := DeclaredOf(n)
	switch {
	case !ok:
		return ""
	case given:
		return AssumeDeclaredByYou
	}
	return AssumeNotDeclared
}

// CursorFacts are the facts of a Cursor scan that decide which questions
// apply (CTO-56 §1.2): the user's permissions.json approvalMode and
// terminalAllowlist, and a readBoundary that sandbox.json fixes (the user's,
// or every repository's). The repositories are those the scan looked in.
func CursorFacts(scan platform.CursorScan) declared.CursorFacts {
	var f declared.CursorFacts
	boundary := map[string]bool{}
	var repoRoots []string
	for _, file := range scan.Files {
		if file.Kind == platform.CursorSandbox && file.Scope == platform.CursorScopeProject {
			repoRoots = append(repoRoots, file.Root)
		}
		if file.Status != platform.MCPSourceRead {
			continue
		}
		switch file.Kind {
		case platform.CursorPermissions:
			if file.Scope == platform.CursorScopeUser {
				f.ApprovalMode = file.ApprovalMode
			}
			f.TerminalAllowlist = f.TerminalAllowlist || file.HasTerminalAllowlist
		case platform.CursorSandbox:
			if file.ReadBoundary == "workspace" {
				boundary[cursorLayer(file)] = true
			}
		}
	}
	f.ReadBoundaryFixed = boundary[platform.MCPScopeUser]
	if !f.ReadBoundaryFixed && len(repoRoots) > 0 {
		all := true
		for _, r := range repoRoots {
			all = all && boundary[platform.MCPScopeProject+":"+canonicalPath(r)]
		}
		f.ReadBoundaryFixed = all
	}
	return f
}

// CursorDeclared adds the declared capabilities of every Cursor ai_client in
// nodes (cursorDeclared).
func CursorDeclared(nodes []Node, scan platform.CursorScan) Fragment {
	b := newFragmentBuilder()
	for _, n := range nodes {
		if n.Kind == KindAgent && n.Type == AgentAIClient && n.Platform == knowledge.CursorProduct {
			cursorDeclared(b, n.ID, scan)
		}
	}
	return b.fragment()
}

// cursorDeclared adds Cursor's declared capabilities to b (CTO-56 §1.3): the
// run mode (from C1, or configured from approvalMode), the class of the
// command list (C2), Read Access Workspace (C3) and External-File Protection
// off (C4). A question that does not apply adds nothing; Allowlist and Ask
// Every Time add a mode with no way of its own.
func cursorDeclared(b *fragmentBuilder, client string, scan platform.CursorScan) {
	facts := CursorFacts(scan)
	st := declared.ResolveCursor(facts, declared.Clean(declared.Cursor, scan.Declared))
	add := func(typ, approval, scope, question string, a declared.Answer) {
		n := Node{
			ID:   "cap_" + keyHash(KindCapability, BasisDeclared, client, question),
			Kind: KindCapability, Type: typ, Basis: BasisDeclared, InForce: true,
			Approval: approval, TargetScope: scope, AppliesIn: platform.MCPScopeUser,
			Source: declaredSource(declared.Cursor, question, a.Code, a.Declared),
		}
		if b.node(n) {
			b.edge(Edge{ID: edgeID(EdgeHas, client, n.ID), Type: EdgeHas, From: client, To: n.ID, Source: n.Source})
		}
	}
	if st.ModeRead {
		file := ""
		for _, f := range scan.Files {
			if f.Kind == platform.CursorPermissions && f.Scope == platform.CursorScopeUser && f.Status == platform.MCPSourceRead {
				file = f.Path
			}
		}
		c := capContext{b: b, client: client, file: file, appliesIn: platform.MCPScopeUser}
		c.add(Node{Type: CapPermissionMode, Approval: ApprovalAllow, TargetScope: cursorModeScope(st.Mode.Code), InForce: true},
			cursorApprovalModeRule+facts.ApprovalMode, "/approvalMode", nil)
	} else {
		add(CapPermissionMode, ApprovalAllow, cursorModeScope(st.Mode.Code), declared.C1RunMode, st.Mode)
	}
	if a, ok := st.Answers[declared.C2Allowlist]; ok {
		switch a.Code {
		case declared.ListReads, declared.ListNetwork, declared.ListRunCode:
			add(CapExecuteShell, ApprovalAllow, a.Code, declared.C2Allowlist, a)
		}
	}
	if a, ok := st.Answers[declared.C3ReadAccess]; ok && a.Code == declared.ReadWorkspace {
		add(CapReadFiles, ApprovalDeny, outsideWorkingDirectoryScope, declared.C3ReadAccess, a)
	}
	if a := st.Answers[declared.C4ExternalFileOn]; a.Code == declared.Off {
		add(CapWriteFiles, ApprovalAllow, CursorScopeExternalFiles, declared.C4ExternalFileOn, a)
	}
}

// cursorModeScope is the target scope of Cursor's mode capability: the two
// Auto-review modes share the knowledge base's auto_review.
func cursorModeScope(mode string) string {
	if declared.IsAutoReview(mode) {
		return CursorScopeAutoReview
	}
	return mode
}

// CursorModeOption is the run mode a Cursor mode capability stands for: its
// declared option, or the mode approvalMode maps to.
func CursorModeOption(n Node) string {
	if _, q, opt, _, ok := DeclaredOf(n); ok && q == declared.C1RunMode {
		return opt
	}
	if strings.HasPrefix(n.Rule, cursorApprovalModeRule) {
		m, _ := declared.ApprovalModeRunMode(strings.TrimPrefix(n.Rule, cursorApprovalModeRule))
		return m
	}
	return ""
}

// CursorModeRank orders Cursor's run modes from the most controlled (0) to
// the least: when the IDE and the CLI set different modes, the least
// controlled one counts.
func CursorModeRank(option string) int {
	switch option {
	case declared.ModeRunEverything:
		return 4
	case declared.ModeAutoReview, declared.ModeAutoReviewSandbox:
		return 3
	case declared.ModeAllowlistSandbox:
		return 2
	case declared.ModeAllowlist:
		return 1
	}
	return 0
}

// IsCursorMode reports Cursor's mode capability: declared by C1 or read from
// approvalMode.
func IsCursorMode(n Node) bool {
	return n.Type == CapPermissionMode && CursorModeOption(n) != ""
}

// CursorListClass classifies a Cursor command-list capability: a configured
// terminalAllowlist entry (CursorAllowlistClass) or the declared class of C2.
// ok is false for any other capability.
func CursorListClass(c Node, base knowledge.Base) (reads, network, launcher, ok bool) {
	if c.Type != CapExecuteShell || c.Approval != ApprovalAllow {
		return false, false, false, false
	}
	if _, q, opt, _, d := DeclaredOf(c); d && q == declared.C2Allowlist {
		return opt == declared.ListReads, opt == declared.ListNetwork, opt == declared.ListRunCode, true
	}
	if c.Basis == BasisConfigured && strings.HasPrefix(c.Rule, cursorTerminalRule) {
		r, n, l := CursorAllowlistClass(c.TargetScope, base)
		return r, n, l, true
	}
	return false, false, false, false
}

// IsCursorReadBoundary reports a capability that limits Cursor's shell reads
// to the projects: readBoundary workspace in sandbox.json, or C3 Workspace.
func IsCursorReadBoundary(c Node) bool {
	if c.Type != CapReadFiles || c.Approval != ApprovalDeny {
		return false
	}
	if _, q, _, _, ok := DeclaredOf(c); ok {
		return q == declared.C3ReadAccess
	}
	return c.Basis == BasisConfigured && c.Rule == cursorReadBoundaryRule
}

// IsCursorExternalWrite reports External-File Protection off (C4).
func IsCursorExternalWrite(c Node) bool {
	_, q, _, _, ok := DeclaredOf(c)
	return ok && q == declared.C4ExternalFileOn && c.Type == CapWriteFiles && c.TargetScope == CursorScopeExternalFiles
}
