package rules_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// «How your agents work» (decision 2026-10-25, cómo trabajan tus agentes,
// cálculo, T1–T13): the cells, their coherence with the findings (§4), the
// scopes by row (T4) and the CTO's five cases (annex 2).

var allRoots = []string{repoWeb, repoIF, repoNotes, repoWeb2}

// ovFixture is a machine: Claude Code's settings files, Cursor's files and
// the configured roots.
type ovFixture struct {
	claude []platform.ClaudeSettingsFile
	cursor []platform.CursorFile
	roots  []string
}

func userSettings(perms platform.ClaudePermissions) platform.ClaudeSettingsFile {
	return platform.ClaudeSettingsFile{Scope: platform.MCPScopeUser, ConfigPath: userJSON, ConfigDir: claudeDir, Status: platform.MCPSourceRead, Permissions: perms}
}

func projectSettings(root string, perms platform.ClaudePermissions) platform.ClaudeSettingsFile {
	return platform.ClaudeSettingsFile{
		Scope: platform.MCPScopeProject, ScopePath: root, ConfigPath: root + "/.claude/settings.json", ConfigDir: claudeDir,
		Status: platform.MCPSourceRead, Permissions: perms,
	}
}

// ovEvaluate builds, infers and evaluates fx with the default credentials
// and every knowledge base.
func ovEvaluate(t *testing.T, fx ovFixture) (graph.Graph, rules.Result) {
	t.Helper()
	var cf platform.CredentialFileScan
	for _, e := range defaultEntries {
		cf.Entries = append(cf.Entries, platform.CredentialEntry{Path: e.path, Line: e.line, Key: e.key, Value: secrets.NewSecretValue(e.value)})
	}
	mcp := platform.MCPScan{Sources: []platform.MCPSource{
		{Platform: "cursor", ConfigPath: cursorMCP, ConfigDir: cursorDir, Status: platform.MCPSourceRead, LoadsMCP: true},
	}}
	if fx.claude == nil {
		fx.claude = []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{})}
	}
	var trust []platform.ProjectTrust
	for _, r := range fx.roots {
		trust = append(trust, platform.ProjectTrust{Path: r, Accepted: true})
	}
	settings := platform.ClaudeSettingsScan{Files: fx.claude, Trust: trust}
	f, _, err := graph.FromScansWithCursor(mcp, settings, platform.CursorScan{ConfigDir: cursorDir, Exists: true, Files: fx.cursor}, cf, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	var files []platform.InstructionFile
	for _, r := range []string{repoWeb, repoIF, repoNotes} {
		files = append(files, platform.InstructionFile{Product: "claude-code", ConfigDir: claudeDir, Path: r + "/CLAUDE.md"})
	}
	f = graph.AddInstructions(f, platform.InstructionScan{Files: files}, now)
	g, err := graph.Infer(graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(salt)}, now), fx.roots, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.Encode(g); err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	res := rules.Evaluate(rules.Input{
		Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases, RepoRoots: fx.roots,
		Home: home, Now: now, ClaudeSettings: &settings,
	})
	return g, res
}

func ovCell(t *testing.T, res rules.Result, agent, row string) rules.OverviewCell {
	t.Helper()
	for _, a := range res.AgentsOverview {
		if a.Agent != agent {
			continue
		}
		for _, c := range a.Rows {
			if c.Row == row {
				return c
			}
		}
	}
	t.Fatalf("no %s cell for %s", row, agent)
	return rules.OverviewCell{}
}

// ovCases are the machines the property tests run over.
func ovCases() map[string]ovFixture {
	sandbox := func(typ string) platform.CursorFile {
		return platform.CursorFile{Path: cursorDir + "/sandbox.json", Kind: platform.CursorSandbox, Scope: platform.CursorScopeUser, Status: platform.MCPSourceRead, SandboxType: typ}
	}
	return map[string]ovFixture{
		"default": {roots: allRoots},
		"edit src": {roots: allRoots, claude: []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{
			Allow: []string{"Edit(src/**)", "Bash(npm test)"},
		})}},
		// A launcher writes any file (decision 2026-10-26, lanzadores en
		// escritura, L1).
		"launcher": {roots: allRoots, claude: []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{
			Allow: []string{"Bash(npm test)", "Bash(node -e *)"},
		})}},
		"launcher default": {roots: allRoots, claude: []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{
			DefaultMode: "default", Allow: []string{"Bash(npx tsx *)"},
		})}},
		"default mode": {roots: allRoots, claude: []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{DefaultMode: "default"})}},
		"dontAsk":      {roots: allRoots, claude: []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{DefaultMode: "dontAsk"})}},
		"bypass":       {roots: allRoots, claude: []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{DefaultMode: "bypassPermissions"})}},
		"acceptEdits":  {roots: allRoots, claude: []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{DefaultMode: "acceptEdits"})}},
		"no roots":     {claude: []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{DefaultMode: "acceptEdits"})}},
		"deny bash": {roots: allRoots, claude: []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{
			DefaultMode: "default", Deny: []string{"Bash", "WebFetch"}, BlockReadsOutsideWorkingDirectories: true,
		})}},
		"insecure_none": {roots: allRoots, cursor: []platform.CursorFile{sandbox("insecure_none")}},
	}
}

// T4: each row uses only its scopes, also's included; the rows are T1's, in
// their order; not_applicable only in model_provider (T12).
func TestOverview_RowScopes(t *testing.T) {
	for name, fx := range ovCases() {
		_, res := ovEvaluate(t, fx)
		if len(res.AgentsOverview) != 2 {
			t.Fatalf("%s: %d agents", name, len(res.AgentsOverview))
		}
		for _, a := range res.AgentsOverview {
			var rows []string
			for _, c := range a.Rows {
				rows = append(rows, c.Row)
				ok := map[string]bool{}
				for _, s := range rules.RowScopes[c.Row] {
					ok[s] = true
				}
				if !ok[c.Scope] || c.Also != nil && !ok[c.Also.Scope] {
					t.Errorf("%s %s %s: scope %s also %+v", name, a.Agent, c.Row, c.Scope, c.Also)
				}
				if (c.Decider == rules.DeciderNotApplicable) != (c.Row == rules.RowModelProvider) {
					t.Errorf("%s %s %s: decider %s", name, a.Agent, c.Row, c.Decider)
				}
				if c.Also != nil && c.Also.Decider == rules.DeciderBlocked {
					t.Errorf("%s %s %s: also blocked", name, a.Agent, c.Row)
				}
			}
			if !reflect.DeepEqual(rows, rules.AllOverviewRows) {
				t.Errorf("%s %s: rows %v", name, a.Agent, rows)
			}
		}
	}
}

// §4, annex 2 point 2: every finding of a reads_files or sends_out cell
// stands on the cell's winning way (way code + capability); the targets of
// changes_other_agents are exactly those of the agent's foreign_* findings,
// both ways; credentials within reach and a way out give cred_read_and_send,
// no way out cred_readable; an assumed or model cell has medium findings.
func TestOverview_Coherence(t *testing.T) {
	for name, fx := range ovCases() {
		g, res := ovEvaluate(t, fx)
		nodes := map[string]graph.Node{}
		for _, n := range g.Nodes {
			nodes[n.ID] = n
		}
		byID := map[string]rules.Finding{}
		for _, f := range res.Findings {
			byID[f.ID] = f
		}
		for _, a := range res.AgentsOverview {
			cells := map[string]rules.OverviewCell{}
			for _, c := range a.Rows {
				cells[c.Row] = c
			}
			for _, row := range []string{rules.RowReadsFiles, rules.RowSendsOut} {
				c := cells[row]
				code, capID := rules.CellWay(c)
				for _, id := range c.Data.FindingIDs {
					f, ok := byID[id]
					if !ok || !rules.FindingWays(f)[[2]string{code, capID}] {
						t.Errorf("%s %s %s: %s does not stand on %s/%s", name, a.Agent, row, id, code, capID)
					}
					if (c.Assumed || c.Decider == rules.DeciderModel) && f.Confidence != rules.ConfidenceMedium {
						t.Errorf("%s %s %s: %s confidence %s", name, a.Agent, row, id, f.Confidence)
					}
				}
				if (c.Decider == rules.DeciderNobody || c.Decider == rules.DeciderModel) && len(c.Data.FindingIDs) == 0 && c.NoFindingReason == nil {
					t.Errorf("%s %s %s: no finding and no reason", name, a.Agent, row)
				}
			}
			// changes_other_agents: strict, both ways.
			want := map[string]bool{}
			for _, f := range res.Findings {
				if len(f.Evidence.Nodes) == 0 || f.Evidence.Nodes[0] != a.AgentID ||
					f.Rule != rules.RuleForeignAgentConfigWritable && f.Rule != rules.RuleForeignAgentInstructionsWritable {
					continue
				}
				for _, n := range f.Evidence.Nodes[1:] {
					if nodes[n].Kind == graph.KindAgent {
						want[n] = true
					}
				}
			}
			got := map[string]bool{}
			for _, id := range cells[rules.RowChangesOtherAgents].Data.TargetAgents {
				got[id] = true
			}
			if !reflect.DeepEqual(got, want) && len(got)+len(want) > 0 {
				t.Errorf("%s %s: target_agents %v, foreign findings %v", name, a.Agent, got, want)
			}
			// reads + sends.
			reads, sends := cells[rules.RowReadsFiles], cells[rules.RowSendsOut]
			if reads.Data.CredentialsReachable == nil || *reads.Data.CredentialsReachable == 0 {
				continue
			}
			rule := rules.RuleCredReadable
			if sends.Decider == rules.DeciderNobody || sends.Decider == rules.DeciderModel {
				rule = rules.RuleCredReadAndSend
			}
			found := false
			for _, f := range res.Findings {
				found = found || f.Rule == rule && len(f.Evidence.Nodes) > 0 && f.Evidence.Nodes[0] == a.AgentID
			}
			if !found && *reads.Data.CredentialsReachableHigh > 0 {
				t.Errorf("%s %s: sends %s and %d keys within reach, no %s", name, a.Agent, sends.Decider, *reads.Data.CredentialsReachable, rule)
			}
		}
	}
}

// Annex 2, point 2: reads_files is machine by a way (the winner's or
// also's, nobody or model) exactly when the probe's target outside the
// roots is reached.
func TestOverview_ReadsScopeIsTheOutsideTarget(t *testing.T) {
	for name, fx := range ovCases() {
		g, res := ovEvaluate(t, fx)
		probe, err := graph.Probe(g, fx.roots, home)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range res.AgentsOverview {
			ap := probe[a.AgentID]
			outside := false
			for _, h := range ap.Read {
				outside = outside || len(h.Edges) > 0 && (h.Target.Outside || ap.NoOutside)
			}
			c := ovCell(t, res, a.Agent, rules.RowReadsFiles)
			way := func(d, s string) bool {
				return (d == rules.DeciderNobody || d == rules.DeciderModel) && s == rules.ScopeMachine
			}
			machine := way(c.Decider, c.Scope) || c.Also != nil && way(c.Also.Decider, c.Also.Scope)
			if machine != outside {
				t.Errorf("%s %s: reads machine %v, outside target reached %v", name, a.Agent, machine, outside)
			}
		}
	}
}

// The CTO's five (annex 2): Edit(src/**) allow; a project rule in one root;
// dontAsk; insecure_none; no roots with acceptEdits.
func TestOverview_CTOCases(t *testing.T) {
	cases := ovCases()

	// 1. Edit(src/**) allow: writes_files nobody, same_project.
	_, res := ovEvaluate(t, cases["edit src"])
	if c := ovCell(t, res, "Claude Code", rules.RowWritesFiles); c.Decider != rules.DeciderNobody || c.Scope != rules.ScopeSameProject ||
		*c.Data.EditAllowRules != 1 || c.Origin != rules.OriginConfigured {
		t.Errorf("Edit(src/**): %+v", c)
	}

	// 2. A project rule in one root changes the cell.
	base := ovFixture{roots: allRoots, claude: []platform.ClaudeSettingsFile{userSettings(platform.ClaudePermissions{DefaultMode: "default"})}}
	_, before := ovEvaluate(t, base)
	withProject := base
	withProject.claude = append(append([]platform.ClaudeSettingsFile{}, base.claude...), projectSettings(repoWeb, platform.ClaudePermissions{Allow: []string{"Edit(**)"}}))
	_, after := ovEvaluate(t, withProject)
	b, a := ovCell(t, before, "Claude Code", rules.RowWritesFiles), ovCell(t, after, "Claude Code", rules.RowWritesFiles)
	if b.Decider != rules.DeciderYou || a.Decider != rules.DeciderNobody || a.Scope != rules.ScopeSameProject {
		t.Errorf("project rule: before %s/%s, after %s/%s", b.Decider, b.Scope, a.Decider, a.Scope)
	}

	// 3. dontAsk: every question is a refusal.
	_, res = ovEvaluate(t, cases["dontAsk"])
	for _, row := range []string{rules.RowWritesFiles, rules.RowRunsCommands, rules.RowSendsOut} {
		if c := ovCell(t, res, "Claude Code", row); c.Decider != rules.DeciderBlocked {
			t.Errorf("dontAsk %s: %s", row, c.Decider)
		}
	}

	// 4. insecure_none: Cursor runs commands with all the user's rights.
	_, res = ovEvaluate(t, cases["insecure_none"])
	if c := ovCell(t, res, "Cursor", rules.RowRunsCommands); c.Scope != rules.ScopeMachine {
		t.Errorf("insecure_none: %s/%s", c.Decider, c.Scope)
	}
	_, res = ovEvaluate(t, cases["default"])
	if c := ovCell(t, res, "Cursor", rules.RowRunsCommands); c.Decider != rules.DeciderModel || c.Scope != rules.ScopeSandbox || !c.Assumed {
		t.Errorf("Cursor's sandbox: %+v", c)
	}

	// 5. No roots: acceptEdits is same_project (a synthetic root).
	_, res = ovEvaluate(t, cases["no roots"])
	if c := ovCell(t, res, "Claude Code", rules.RowWritesFiles); c.Decider != rules.DeciderNobody || c.Scope != rules.ScopeSameProject {
		t.Errorf("no roots, acceptEdits: %s/%s", c.Decider, c.Scope)
	}
}

// T3 and §2.4: preapproved commands are data, a launcher among them makes
// runs_commands nobody; a denied tool without specifier is blocked, not you.
func TestOverview_RunsAndBlocked(t *testing.T) {
	_, res := ovEvaluate(t, ovCases()["launcher"])
	c := ovCell(t, res, "Claude Code", rules.RowRunsCommands)
	if *c.Data.PreapprovedRules != 2 || *c.Data.LauncherRules != 1 || c.Decider != rules.DeciderNobody || c.Scope != rules.ScopeMachine {
		t.Errorf("runs: %+v %d %d", c, *c.Data.PreapprovedRules, *c.Data.LauncherRules)
	}
	_, res = ovEvaluate(t, ovCases()["deny bash"])
	for _, row := range []string{rules.RowRunsCommands} {
		if c := ovCell(t, res, "Claude Code", row); c.Decider != rules.DeciderBlocked || c.Scope != rules.ScopeNone {
			t.Errorf("deny Bash %s: %s/%s", row, c.Decider, c.Scope)
		}
	}
	if c := ovCell(t, res, "Claude Code", rules.RowReadsFiles); c.Scope != rules.ScopeSameProject {
		t.Errorf("blockReadsOutsideWorkingDirectories: reads %s/%s also %+v", c.Decider, c.Scope, c.Also)
	}
}

// T12: the provider's variables by precedence; the default assumes no
// session variable; values are compared, never kept.
func TestOverview_ModelProvider(t *testing.T) {
	_, res := ovEvaluate(t, ovFixture{roots: allRoots})
	c := ovCell(t, res, "Claude Code", rules.RowModelProvider)
	if c.Data.Recipient != rules.RecipientAnthropicAPI || c.Origin != rules.OriginProductDefault || !c.Assumed ||
		!reflect.DeepEqual(c.Data.Assumptions, []string{rules.AssumeSessionEnvNotReadable}) {
		t.Errorf("default: %+v", c)
	}
	if c := ovCell(t, res, "Cursor", rules.RowModelProvider); c.Data.Recipient != rules.RecipientVendorBackend || c.Assumed ||
		c.Data.ByoKeyStillRouted == nil || !*c.Data.ByoKeyStillRouted {
		t.Errorf("Cursor: %+v", c)
	}
	env := func(k, v string) []platform.EnvVar {
		return []platform.EnvVar{{Key: k, Value: secrets.NewSecretValue(v)}}
	}
	user := userSettings(platform.ClaudePermissions{})
	user.Env = env("ANTHROPIC_BASE_URL", "https://gateway.example.com")
	project := projectSettings(repoWeb, platform.ClaudePermissions{})
	project.Env = env("CLAUDE_CODE_USE_BEDROCK", "1")
	off := projectSettings(repoIF, platform.ClaudePermissions{})
	off.Env = env("CLAUDE_CODE_USE_VERTEX", "0")
	for _, tc := range []struct {
		files []platform.ClaudeSettingsFile
		want  string
	}{
		{[]platform.ClaudeSettingsFile{user}, rules.RecipientCustomGateway},
		{[]platform.ClaudeSettingsFile{user, project}, rules.RecipientAWSBedrock},
		{[]platform.ClaudeSettingsFile{off}, rules.RecipientAnthropicAPI},
	} {
		_, res := ovEvaluate(t, ovFixture{roots: allRoots, claude: tc.files})
		c := ovCell(t, res, "Claude Code", rules.RowModelProvider)
		if c.Data.Recipient != tc.want {
			t.Errorf("recipient %s, want %s", c.Data.Recipient, tc.want)
		}
		if tc.want != rules.RecipientAnthropicAPI && (c.Origin != rules.OriginConfigured || c.Assumed) {
			t.Errorf("%s: %+v", tc.want, c)
		}
	}
}

// §6: knowledge {date, stale}, stale from 90 days.
func TestOverview_KnowledgeStale(t *testing.T) {
	_, res := ovEvaluate(t, ovFixture{roots: allRoots})
	for _, a := range res.AgentsOverview {
		if a.Knowledge.Date == nil || a.Knowledge.Stale {
			t.Errorf("%s: %+v", a.Agent, a.Knowledge)
		}
	}
	for days, want := range map[int]bool{89: false, 90: true} {
		if got := rules.Stale(knowledge.Version, mustDate(t, knowledge.Version).AddDate(0, 0, days)); got != want {
			t.Errorf("%d days: stale %v", days, got)
		}
	}
}

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// §5, T8: an agent detected with no knowledge base is one element, not
// evaluated (no_inference), all its cells not_evaluated, whether it is a
// client, an extension or both (decision 2026-10-26, agentes y MCP nuevos,
// A3).
func TestOverview_NotEvaluatedOnce(t *testing.T) {
	g, _ := ovEvaluate(t, ovFixture{roots: allRoots})
	g.Nodes = append(g.Nodes,
		graph.Node{ID: "agent_client_c1c1c1c1c1c1c1c1", Kind: graph.KindAgent, Type: graph.AgentAIClient, Platform: "cline", Name: "Cline"},
		graph.Node{ID: "agent_ext_c1c1c1c1c1c1c1c1", Kind: graph.KindAgent, Type: graph.AgentExtension, Platform: "cline", Name: "Cline"},
	)
	bases, _ := knowledge.All()
	res := rules.Evaluate(rules.Input{Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases, RepoRoots: allRoots, Home: home, Now: now})
	var cline []rules.OverviewAgent
	for _, a := range res.AgentsOverview {
		if a.Agent == "Cline" {
			cline = append(cline, a)
		}
	}
	if len(cline) != 1 || cline[0].Evaluated || cline[0].NotEvaluatedReason == nil || *cline[0].NotEvaluatedReason != rules.NotEvaluatedNoInference ||
		cline[0].AgentID != "agent_client_c1c1c1c1c1c1c1c1" || cline[0].Knowledge.Date != nil {
		t.Fatalf("Cline: %+v", cline)
	}
	for _, c := range cline[0].Rows {
		if c.Decider != rules.DeciderNotEvaluated || c.Scope != rules.ScopeNone || c.Origin != rules.OriginNone {
			t.Errorf("Cline %s: %+v", c.Row, c)
		}
	}
}
