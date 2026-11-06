package report_test

import (
	"strings"
	"testing"
	"time"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

// bv184Build builds the BV-107 one-finding machine (a project hook that runs a
// repo script) once and returns a function that rebuilds the v2 report with any
// allow_list.dismissed. The graph (and so the finding id) is fixed across
// rebuilds, so a test can dismiss the exact id it saw.
func bv184Build(t *testing.T) func(dismissed []config.AllowEntryDismissed) report.V2 {
	t.Helper()
	home := t.TempDir()
	repo := home + "/src/app"
	claudeDir := home + "/.claude"
	bv107Write(t, repo+"/.claude/settings.json", []byte(bv107Hook))
	bv107Write(t, repo+"/.github/init.js", make([]byte, 10))
	userFile := windows.ReadClaudeSettings(claudeDir+"/settings.json", platform.MCPScopeUser, "", claudeDir)
	projFile := windows.ReadClaudeSettings(repo+"/.claude/settings.json", platform.MCPScopeProject, repo, claudeDir)
	settings := platform.ClaudeSettingsScan{
		Files: []platform.ClaudeSettingsFile{userFile, projFile},
		Trust: []platform.ProjectTrust{{Path: repo, Accepted: true}},
	}
	salt := []byte("0123456789abcdef0123456789abcdef")
	frag, _, err := graph.FromScans(platform.MCPScan{}, settings, platform.CredentialFileScan{}, salt, v2Now)
	if err != nil {
		t.Fatal(err)
	}
	g, err := graph.Infer(graph.Build(frag, graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt)}, v2Now), []string{repo}, home)
	if err != nil {
		t.Fatal(err)
	}
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	return func(dismissed []config.AllowEntryDismissed) report.V2 {
		v, _ := report.BuildV2(report.V2Input{
			Graph: &g, GraphSHA256: strings.Repeat("ab", 32), Base: base, RepoRoots: []string{repo},
			Families: [][]string{{repo}}, Dismissed: dismissed,
			V1: report.Build("t", nil, nil, 1, 0, 0, 0, nil), GeneratedAt: v2Now,
		})
		return v
	}
}

// BV-184: a dismissed finding leaves the score and the findings list and moves
// to Dismissed; the report shows the folded section and the score-line note.
func TestBV184_DismissedPartitionAndReport(t *testing.T) {
	build := bv184Build(t)
	v0 := build(nil)
	if len(v0.Findings) != 1 {
		t.Fatalf("baseline: %d findings, want 1", len(v0.Findings))
	}
	if v0.Status == "green" {
		t.Fatal("baseline should not be green with a finding")
	}
	id := v0.Findings[0].ID

	v := build([]config.AllowEntryDismissed{{ID: id, Date: "2026-11-06"}})
	if len(v.Findings) != 0 {
		t.Errorf("dismissed: %d findings still scored, want 0", len(v.Findings))
	}
	if len(v.Dismissed) != 1 || v.Dismissed[0].ID != id {
		t.Errorf("dismissed list = %+v, want the one id %s", v.Dismissed, id)
	}
	if v.Status != "green" {
		t.Errorf("status with the only finding dismissed = %s, want green", v.Status)
	}
	cat, err := catalog.Load("es")
	if err != nil {
		t.Fatal(err)
	}
	page := report.RenderHTML(v, cat, "/h", time.UTC)
	if !strings.Contains(page, "Descartados por ti (1)") {
		t.Error("the report lacks the «Descartados por ti (1)» section")
	}
	if !strings.Contains(page, "sin los avisos que descartaste") {
		t.Error("the score meta line lacks the dismissed note")
	}
	// The report with nothing dismissed has neither.
	plain := report.RenderHTML(v0, cat, "/h", time.UTC)
	if strings.Contains(plain, "Descartados por ti") || strings.Contains(plain, "sin los avisos que descartaste") {
		t.Error("the report shows the dismissed section with nothing dismissed")
	}
}

// BV-184 point 1/5: a dismissal for an id that does not appear this run keeps
// that id in the list but hides nothing — the real finding still counts.
func TestBV184_StaleDismissalHidesNothing(t *testing.T) {
	v := bv184Build(t)([]config.AllowEntryDismissed{{ID: "cred_0000000000000000", Date: "2026-01-01"}})
	if len(v.Findings) != 1 {
		t.Errorf("a dismissal of an absent id changed the scored findings: %d, want 1", len(v.Findings))
	}
	if len(v.Dismissed) != 0 {
		t.Errorf("a dismissal of an absent id produced %d dismissed cards, want 0", len(v.Dismissed))
	}
}

// BV-184: FindingCards reads the last report back and groups the findings into
// new / the rest / dismissed, red before yellow, matching what the window and
// the console show.
func TestBV184_FindingCardsFromReport(t *testing.T) {
	v, err := report.ReadV2("../schemaval/testdata/valid", []string{"/home/dev/src/webapp"}, 0)
	if err != nil {
		t.Fatalf("ReadV2: %v", err)
	}
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	cards := report.FindingCards(v, cat, "/h")
	if len(cards) == 0 {
		t.Fatal("no cards from a report with findings")
	}
	for _, c := range cards {
		if c.Title == "" {
			t.Errorf("card %s has no title", c.ID)
		}
		if c.Severity != "red" && c.Severity != "yellow" {
			t.Errorf("card %s severity %q", c.ID, c.Severity)
		}
	}
	// Within a group the reds come before the yellows.
	seenYellow := map[string]bool{}
	for _, c := range cards {
		if c.Severity == "yellow" {
			seenYellow[c.Group] = true
		} else if seenYellow[c.Group] {
			t.Errorf("group %q: a red (%s) after a yellow", c.Group, c.ID)
		}
	}
}
