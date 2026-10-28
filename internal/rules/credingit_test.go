package rules_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// committed is one committed occurrence of value, as the repository scan
// reports it: identified with the salt, the value discarded.
func committed(value, patternID, repo, presence, file string, line int, remote bool) finding.CommittedSecret {
	return finding.CommittedSecret{
		PatternID: patternID, Ref: graph.Identifier(salt)(patternID, value), Repo: repo, Presence: presence,
		File: file, Line: line, Commit: "0123456789abcdef0123456789abcdef01234567", HasRemote: remote,
	}
}

// gitScenario is the real-shape scenario with the committed secrets cs.
func gitScenario(t *testing.T, cs []finding.CommittedSecret, entries []entry, roots ...string) graph.Graph {
	t.Helper()
	var cf platform.CredentialFileScan
	for _, e := range entries {
		cf.Entries = append(cf.Entries, platform.CredentialEntry{Path: e.path, Line: e.line, Key: e.key, Value: secrets.NewSecretValue(e.value)})
	}
	mcp := platform.MCPScan{Sources: []platform.MCPSource{
		{Platform: "claude-code", ConfigPath: home + "/.claude.json", ConfigDir: claudeDir, Status: platform.MCPSourceRead, LoadsMCP: true},
	}}
	settings := platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{{
		Scope: platform.MCPScopeUser, ConfigPath: userJSON, ConfigDir: claudeDir, Status: platform.MCPSourceRead,
	}}}
	fs := []finding.Finding{{ID: "secret:1", Category: "secret_tracked", Committed: cs}}
	f, _, err := graph.FromScansWithRepos(mcp, settings, platform.CursorScan{}, cf, graph.Repos{Findings: fs}, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) == 0 {
		roots = []string{repoWeb, repoIF, repoNotes, repoWeb2}
	}
	g, err := graph.Infer(graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Version, HostID: graph.HostID(salt)}, now), roots, home)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.Encode(g); err != nil {
		t.Fatal(err)
	}
	return g
}

func gitEvaluate(t *testing.T, g graph.Graph, cs []finding.CommittedSecret, mod func(*rules.Input)) rules.Result {
	t.Helper()
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	in := rules.Input{
		Graph: g, Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: []string{repoWeb, repoIF, repoNotes, repoWeb2}, Families: map[string]string{repoWeb: repoWeb, repoWeb2: repoWeb},
		Committed: cs, ConfigFile: home + "/.rosmarops/config.json", Now: now,
	}
	if mod != nil {
		mod(&in)
	}
	return rules.Evaluate(in)
}

func gitFindings(res rules.Result) []rules.Finding {
	var out []rules.Finding
	for _, f := range res.Findings {
		if f.Rule == rules.RuleCredInGit {
			out = append(out, f)
		}
	}
	return out
}

// M8: a high key committed in a repository with a remote is red, first in
// the impact order (before cred_read_and_send), weighs 20 × 1.5 and carries
// remote_visibility_unknown; without a remote it is red, after
// cred_read_and_send, weighs 20.
func TestCredInGit_RemoteAndLocal(t *testing.T) {
	for _, remote := range []bool{true, false} {
		cs := []finding.CommittedSecret{committed(sbSecret, "supabase_secret_key", repoIF, finding.PresenceHistoryOnly, "config/keys.ts", 0, remote)}
		g := gitScenario(t, cs, defaultEntries)
		res := gitEvaluate(t, g, cs, nil)
		gf := gitFindings(res)
		if len(gf) != 1 {
			t.Fatalf("remote %v: %d cred_in_git findings", remote, len(gf))
		}
		f := gf[0]
		if f.Severity != rules.SeverityRed || f.Params.HasRemote == nil || *f.Params.HasRemote != remote || f.Params.CredID == "" {
			t.Errorf("remote %v: %+v", remote, f.Params)
		}
		hasAssumption := strings.Contains(strings.Join(f.Evidence.Assumptions, ","), rules.AssumeRemoteVisibilityUnknown)
		if hasAssumption != remote {
			t.Errorf("remote %v: assumptions %v", remote, f.Evidence.Assumptions)
		}
		first := 0
		for i, x := range res.Findings {
			if x.Rule == rules.RuleCredReadAndSend && x.Severity == rules.SeverityRed {
				first = i
				break
			}
		}
		pos := -1
		for i, x := range res.Findings {
			if x.ID == f.ID {
				pos = i
			}
		}
		if remote && pos != 0 || !remote && pos < first {
			t.Errorf("remote %v: cred_in_git at %d, first red cred_read_and_send at %d", remote, pos, first)
		}
		if a := f.Actions; len(a) != 1 || a[0].Code != rules.ActionRotateCredential || a[0].Completeness != rules.CompletenessFull ||
			a[0].Magnitude != 3 || !strings.Contains(a[0].Value, f.Params.CredID) || !strings.Contains(a[0].Value, `"date": "2026-10-24"`) {
			t.Errorf("remote %v: actions %+v", remote, f.Actions)
		}
		// Index v2: in every evaluated agent's score, 20 × 1.5 with a remote.
		idx := index.Compute(index.Input{
			GraphBuilt: true, Inference: map[string]bool{knowledge.ClaudeCodeProduct: true},
			Agents:   []index.Agent{{ID: agentID(g), Name: "Claude Code", Product: knowledge.ClaudeCodeProduct}},
			Findings: res.Findings, Today: res.Today, RulesVersion: "1", KnowledgeVersion: "k",
		})
		want := 20.0
		if remote {
			want = 30
		}
		found := false
		for _, w := range idx.Agents[0].Weights {
			if w.FindingID == f.ID {
				found = w.Weight == want
			}
		}
		if !found {
			t.Errorf("remote %v: weight of cred_in_git not %v: %+v", remote, want, idx.Agents[0].Weights)
		}
	}
}

func agentID(g graph.Graph) string {
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient {
			return n.ID
		}
	}
	return ""
}

// M8: one key in 27 clones is one finding with 27 members in one family.
func TestCredInGit_ClonesAreOneFinding(t *testing.T) {
	var cs []finding.CommittedSecret
	var roots []string
	fams := map[string]string{}
	for i := 0; i < 27; i++ {
		root := repoWeb + "-" + string(rune('a'+i%26)) + strings.Repeat("x", i/26)
		roots = append(roots, root)
		fams[root] = roots[0]
		cs = append(cs, committed(sbSecret, "supabase_secret_key", root, finding.PresenceHistoryOnly, "src/env.ts", 0, false))
	}
	g := gitScenario(t, cs, nil, roots...)
	res := gitEvaluate(t, g, cs, func(in *rules.Input) { in.RepoRoots, in.Families = roots, fams })
	gf := gitFindings(res)
	if len(gf) != 1 || gf[0].Params.Count != 27 || len(gf[0].Group.Members) != 1 || gf[0].Group.Members[0].Count != 27 {
		t.Fatalf("findings %d, %+v", len(gf), gf)
	}
}

// §2.2: the key committed in a repository and the same key in a .env file
// are one credential node; a key only in the history has no location, a
// committed_in edge and its finding; a key in a tracked file has that file
// as location, committed_in, reaches and its finding.
func TestCredInGit_SameNodeTrackedAndHistory(t *testing.T) {
	historyKey := "sk_live_" + strings.Repeat("H", 24)
	cs := []finding.CommittedSecret{
		committed(sbSecret, "supabase_secret_key", repoWeb, finding.PresenceTracked, "dashboard/.env.local", 2, false),
		committed(historyKey, "stripe_secret_key", repoIF, finding.PresenceHistoryOnly, "old/keys.txt", 0, false),
	}
	g := gitScenario(t, cs, defaultEntries)
	nodes := map[string]graph.Node{}
	sbCount := 0
	for _, n := range g.Nodes {
		nodes[n.ID] = n
		if n.Kind == graph.KindCredential && n.Fingerprint == graph.Fingerprint(salt, sbSecret) {
			sbCount++
		}
	}
	if sbCount != 1 {
		t.Fatalf("%d nodes for the Supabase secret", sbCount)
	}
	sb := nodes["cred_"+graph.KeyHash(graph.Fingerprint(salt, sbSecret))]
	hist := nodes["cred_"+graph.KeyHash(graph.Fingerprint(salt, historyKey))]
	if len(hist.Locations) != 0 || hist.Type != graph.CredentialAPIKey || hist.Service != "stripe" {
		t.Errorf("history-only key %+v", hist)
	}
	edges := map[string][]graph.Edge{}
	for _, e := range g.Edges {
		edges[e.From+"/"+e.Type] = append(edges[e.From+"/"+e.Type], e)
	}
	if c := edges[hist.ID+"/"+graph.EdgeCommittedIn]; len(c) != 1 || c[0].Presence != finding.PresenceHistoryOnly || c[0].Source != "0123456:old/keys.txt" {
		t.Errorf("history committed_in %+v", c)
	}
	if c := edges[sb.ID+"/"+graph.EdgeCommittedIn]; len(c) != 1 || c[0].Presence != finding.PresenceTracked || c[0].Source != "dashboard/.env.local:2" {
		t.Errorf("tracked committed_in %+v", c)
	}
	reached := false
	for _, e := range g.Edges {
		reached = reached || e.Type == graph.EdgeReaches && e.To == sb.ID
	}
	if !reached {
		t.Error("the tracked key is not reached")
	}
	gf := gitFindings(gitEvaluate(t, g, cs, nil))
	ids := map[string]bool{}
	for _, f := range gf {
		ids[f.Params.CredID] = true
	}
	if !ids[sb.ID] || !ids[hist.ID] {
		t.Errorf("cred_in_git for %v", ids)
	}
	// A credential without a location and without committed_in is invalid.
	bad := g
	bad.Edges = nil
	if err := graph.ValidateIntegrity(bad); err == nil || !strings.Contains(err.Error(), "no location and no committed_in") {
		t.Errorf("integrity: %v", err)
	}
}

// M9: a credential declared in allow_list.rotated_credentials gives no
// cred_in_git; another one still does.
func TestCredInGit_Rotated(t *testing.T) {
	cs := []finding.CommittedSecret{committed(sbSecret, "supabase_secret_key", repoIF, finding.PresenceHistoryOnly, "a.ts", 0, true)}
	g := gitScenario(t, cs, nil)
	id := gitFindings(gitEvaluate(t, g, cs, nil))[0].Params.CredID
	res := gitEvaluate(t, g, cs, func(in *rules.Input) {
		in.Rotated = []config.AllowEntryRotated{{ID: id, Date: "2026-10-24", Note: ""}, {ID: "cred_0000000000000000", Date: "2026-10-24"}}
	})
	if gf := gitFindings(res); len(gf) != 0 {
		t.Errorf("rotated credential still reported: %+v", gf)
	}
	// B9: what it hid is counted with its entry; the unused entry is not.
	if want := []config.AllowHit{{List: config.ListRotatedCredentials, ID: id}}; !reflect.DeepEqual(res.Silenced.Rotated, want) {
		t.Errorf("rotated hits %+v, want %+v", res.Silenced.Rotated, want)
	}
}

// M10: the four guards. An entry covering test files silences their keys
// (not in the score, counted); one covering a .env file is not applied
// there (guard 1); a key also in a .env file elsewhere is not silenced
// anywhere (guard 2); an expired entry is ignored and counted.
func TestCredInGit_TestFixtures(t *testing.T) {
	fake := "sk_live_" + strings.Repeat("F", 24)
	real := sbSecret // also in dashboard/.env.local (defaultEntries)
	cs := []finding.CommittedSecret{
		committed(fake, "stripe_secret_key", repoIF, finding.PresenceTracked, "internal/x/x_test.go", 3, false),
		committed(real, "supabase_secret_key", repoIF, finding.PresenceTracked, "internal/y/y_test.go", 4, false),
		committed("sk_live_"+strings.Repeat("E", 24), "stripe_secret_key", repoNotes, finding.PresenceTracked, ".env.test", 1, false),
	}
	g := gitScenario(t, cs, defaultEntries)
	res := gitEvaluate(t, g, cs, func(in *rules.Input) {
		in.Fixtures = []config.AllowEntryTestFixture{
			{Repo: repoIF, Path: "**/*_test.go", Note: "fake keys in tests", ExpiresOn: "2027-10-01"},
			{Repo: repoNotes, Path: "**", Note: "fixtures folder", ExpiresOn: "2027-10-01"},
			{Repo: repoWeb, Path: "**/*_test.go", Note: "old entry", ExpiresOn: "2026-01-01"},
		}
	})
	sil := res.Silenced
	if sil.Count != 1 || sil.Expired != 1 || len(sil.Entries) != 3 {
		t.Fatalf("silenced %+v", sil)
	}
	states := map[string]string{}
	for _, e := range sil.Entries {
		states[e.Repo] = e.State
	}
	if states[repoIF] != rules.FixtureApplied || states[repoNotes] != rules.FixtureIgnoredScope || states[repoWeb] != rules.FixtureExpired {
		t.Errorf("states %v", states)
	}
	guards := map[string]bool{}
	for _, gh := range sil.Guards {
		guards[gh.Guard+" "+gh.Repo] = true
	}
	if !guards[rules.FixtureFingerprintMatch+" "+repoIF] || !guards[rules.FixtureIgnoredScope+" "+repoNotes] {
		t.Errorf("guards %v", sil.Guards)
	}
	creds := map[string]bool{}
	for _, f := range gitFindings(res) {
		creds[f.Params.CredID] = true
	}
	fakeID := "cred_" + graph.KeyHash(graph.Fingerprint(salt, fake))
	realID := "cred_" + graph.KeyHash(graph.Fingerprint(salt, real))
	if creds[fakeID] || !creds[realID] || len(creds) != 2 {
		t.Errorf("reported %v (fake %s, real %s)", creds, fakeID, realID)
	}
	// Silenced everywhere: no agent finding reads the fake key either.
	for _, f := range res.Findings {
		for _, n := range f.Evidence.Nodes {
			if n == fakeID {
				t.Errorf("silenced key in %s %s", f.Rule, f.ID)
			}
		}
	}
	// Not in the score: the silenced finding is not among the findings.
	idx := index.Compute(index.Input{
		GraphBuilt: true, Inference: map[string]bool{knowledge.ClaudeCodeProduct: true},
		Agents:   []index.Agent{{ID: agentID(g), Name: "Claude Code", Product: knowledge.ClaudeCodeProduct}},
		Findings: res.Findings, RulesVersion: "1", KnowledgeVersion: "k",
	})
	for _, w := range idx.Agents[0].Weights {
		if strings.Contains(w.FindingID, graph.KeyHash(rules.RuleCredInGit, fakeID)) {
			t.Error("silenced finding weighs in the score")
		}
	}
}

// I7: with index_version 1 in the previous run, the comparison is direct
// and MethodDelta is the method's share (cred_in_git weighs 0 in v1).
func TestCredInGit_MethodChange(t *testing.T) {
	cs := []finding.CommittedSecret{committed(openAIKey, "openai_project_key", repoIF, finding.PresenceHistoryOnly, "a.ts", 0, false)}
	g := gitScenario(t, cs, nil)
	res := gitEvaluate(t, g, cs, nil)
	prevValue := 70
	in := index.Input{
		GraphBuilt: true, Inference: map[string]bool{knowledge.ClaudeCodeProduct: true},
		Agents:   []index.Agent{{ID: agentID(g), Name: "Claude Code", Product: knowledge.ClaudeCodeProduct}},
		Findings: res.Findings, RulesVersion: "1", KnowledgeVersion: "k",
		Previous: &index.Entry{
			IndexVersion: 1, RulesVersion: "1", KnowledgeVersion: "k", Scope: index.EntryScope{Agents: []string{"Claude Code"}},
			Agents: []index.EntryAgent{{Agent: "Claude Code", Value: &prevValue}},
		},
	}
	a := index.Compute(in).Agents[0]
	if a.Comparison.Status != index.CompareDirect || a.Comparison.MethodDelta == nil || *a.Comparison.MethodDelta >= 0 || a.Comparison.PrevIndexVersion != 1 {
		t.Errorf("comparison %+v", a.Comparison)
	}
}
