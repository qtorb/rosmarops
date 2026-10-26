package rules_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/rules"
)

// Decision 2026-10-26 (ruido del informe), point 1, with the real case: one
// key in smoke_test.ps1 and in vars_raw.txt in 27 clones of a family, and
// other keys only in *_test.go, tests/*.test.mjs, .env.example and the
// history of test files. One cred_in_git, the shared key's, with its 27
// repositories and both files (the guard: vars_raw.txt is no test file);
// the rest counted as test data, with their files, and not in the score.
func TestCredInGit_TestDataByDefault(t *testing.T) {
	var cs []finding.CommittedSecret
	var roots []string
	fams := map[string]string{}
	for i := 0; i < 27; i++ {
		root := repoWeb + "-" + string(rune('a'+i%26)) + strings.Repeat("x", i/26)
		roots = append(roots, root)
		fams[root] = roots[0]
		cs = append(cs,
			committed(sbSecret, "supabase_secret_key", root, finding.PresenceTracked, "scripts/smoke_test.ps1", 3, true),
			committed(sbSecret, "supabase_secret_key", root, finding.PresenceTracked, "vars_raw.txt", 1, true),
			committed(sbSecret, "supabase_secret_key", root, finding.PresenceHistoryOnly, "old/vars.txt", 0, true))
	}
	stripe := func(c string) string { return "sk_live_" + strings.Repeat(c, 24) }
	cs = append(cs,
		committed(stripe("A"), "stripe_secret_key", repoIF, finding.PresenceTracked, "internal/scan/secrets/patterns_test.go", 12, false),
		committed(stripe("B"), "stripe_secret_key", repoIF, finding.PresenceTracked, "tests/no-secret-in-tree.test.mjs", 7, false),
		committed(stripe("C"), "stripe_secret_key", repoNotes, finding.PresenceTracked, ".env.example", 2, false),
		committed(stripe("D"), "stripe_secret_key", repoNotes, finding.PresenceHistoryOnly, "test/fixtures/keys.json", 0, false),
		committed(stripe("D"), "stripe_secret_key", repoIF, finding.PresenceHistoryOnly, "pay/pay.spec.ts", 0, false))
	roots = append(roots, repoIF, repoNotes)
	// The collector reads the template too: a location in a template file
	// of a repository keeps it test data.
	g := gitScenario(t, cs, []entry{{repoNotes + "/.env.example", 2, "STRIPE_SECRET_KEY", stripe("C")}}, roots...)
	res := gitEvaluate(t, g, cs, func(in *rules.Input) { in.RepoRoots, in.Families = roots, fams })
	gf := gitFindings(res)
	sbID := "cred_" + graph.KeyHash(graph.Fingerprint(salt, sbSecret))
	if len(gf) != 1 || gf[0].Params.CredID != sbID || gf[0].Params.Count != 27 {
		t.Fatalf("cred_in_git findings %d: %+v", len(gf), gf)
	}
	locs := strings.Join(gf[0].Evidence.Locations, "\n")
	if strings.Count(locs, "smoke_test.ps1") != 27 || strings.Count(locs, "vars_raw.txt") != 27 {
		t.Errorf("locations %v", gf[0].Evidence.Locations)
	}
	td := res.TestData
	if td.Count != 4 {
		t.Errorf("test data %d, want 4", td.Count)
	}
	want := []rules.TestDataFile{
		{Repo: repoIF, Path: "internal/scan/secrets/patterns_test.go"},
		{Repo: repoIF, Path: "pay/pay.spec.ts"},
		{Repo: repoIF, Path: "tests/no-secret-in-tree.test.mjs"},
		{Repo: repoNotes, Path: ".env.example"},
		{Repo: repoNotes, Path: "test/fixtures/keys.json"},
	}
	if len(td.Files) != len(want) {
		t.Fatalf("files %+v", td.Files)
	}
	for i := range want {
		if td.Files[i] != want[i] {
			t.Errorf("file %d: %+v, want %+v", i, td.Files[i], want[i])
		}
	}
	if res.Silenced.Count != 0 {
		t.Errorf("silenced %+v", res.Silenced)
	}
	// Not in the score: the shared key's is the only cred_in_git weight.
	idx := index.Compute(index.Input{
		GraphBuilt: true, Inference: map[string]bool{knowledge.ClaudeCodeProduct: true},
		Agents:   []index.Agent{{ID: agentID(g), Name: "Claude Code", Product: knowledge.ClaudeCodeProduct}},
		Findings: res.Findings, RulesVersion: "1", KnowledgeVersion: "k",
	})
	rule := map[string]string{}
	for _, f := range res.Findings {
		rule[f.ID] = f.Rule
	}
	n := 0
	for _, w := range idx.Agents[0].Weights {
		if rule[w.FindingID] == rules.RuleCredInGit {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d cred_in_git weights", n)
	}
}

// Point 1, the guard: a key in test files whose fingerprint the collector
// also finds outside test and template files (or outside any repository)
// is no test data: a normal finding, with all its locations.
func TestCredInGit_TestDataGuard(t *testing.T) {
	key := "sk_live_" + strings.Repeat("G", 24)
	for _, loc := range []string{repoIF + "/.env.local", home + "/keys/stripe.env"} {
		cs := []finding.CommittedSecret{committed(key, "stripe_secret_key", repoIF, finding.PresenceTracked, "tests/pay_test.go", 5, false)}
		g := gitScenario(t, cs, []entry{{loc, 1, "STRIPE_SECRET_KEY", key}})
		res := gitEvaluate(t, g, cs, nil)
		gf := gitFindings(res)
		if len(gf) != 1 || res.TestData.Count != 0 {
			t.Fatalf("%s: cred_in_git %d, test data %+v", loc, len(gf), res.TestData)
		}
		if l := strings.Join(gf[0].Evidence.Locations, " "); !strings.Contains(l, "pay_test.go") {
			t.Errorf("%s: locations %v", loc, gf[0].Evidence.Locations)
		}
	}
}

// Point 1, the criterion, literal: test segments in any case, test and
// template names; anything else is not test data.
func TestCredInGit_TestDataCriterion(t *testing.T) {
	cases := map[string]bool{
		"Test/a.go": true, "x/TESTS/a.go": true, "src/__tests__/a.js": true, "pkg/testdata/k.txt": true,
		"fixtures/a.json": true, "fixture/a.json": true, "__fixtures__/a": true, "spec/a.rb": true, "specs/a.rb": true,
		"a_test.go": true, "web/a.test.mjs": true, "a.spec.ts": true, "test_keys.py": true,
		".env.example": true, ".env.sample": true, ".env.template": true, "template.env": true,
		"config.example": true, "keys.sample": true, "x.template": true,
		"vars_raw.txt": false, "testing/a.go": false, "contest/a.go": false, "test_keys.go": false,
		".env": false, ".env.test": false, "latest.go": false, "attest.go": false,
	}
	for file, want := range cases {
		key := "sk_live_" + graph.KeyHash(file)[:12] + strings.Repeat("Q", 12)
		cs := []finding.CommittedSecret{committed(key, "stripe_secret_key", repoIF, finding.PresenceHistoryOnly, file, 0, false)}
		g := gitScenario(t, cs, nil)
		res := gitEvaluate(t, g, cs, nil)
		if got := res.TestData.Count == 1; got != want || got == (len(gitFindings(res)) == 1) {
			t.Errorf("%s: test data %v (findings %d), want %v", file, got, len(gitFindings(res)), want)
		}
	}
}
