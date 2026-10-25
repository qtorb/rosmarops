package report_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-48: with Cursor's knowledge base, Cursor is the second evaluated agent:
// two scores in the index (the machine score stays unavailable while an
// agent is not evaluated), and the HTML shows the lowest one big and the
// other as html.step.1.other, with a «What we don't know» paragraph each.
func TestBuildV2_CursorEvaluated(t *testing.T) {
	g := v2GraphFull(t, []platform.CredentialEntry{
		{Path: "/src/a/dashboard/.env.local", Line: 4, Key: "SUPABASE_SERVICE_ROLE_KEY", Value: secrets.NewSecretValue(serviceRoleJWT)},
	}, []string{"/src/a/CLAUDE.md"})
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("ab", 32), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: []string{"/src/a", "/src/b"}, Families: [][]string{{"/src/a"}, {"/src/b"}},
		V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: v2Now,
	})
	scores := map[string]int{}
	for _, a := range v.Index.Agents {
		if a.Value == nil {
			t.Fatalf("%s: no score (%s)", a.Agent, a.Unavailable)
		}
		scores[a.Agent] = *a.Value
	}
	if len(scores) != 2 || len(v.Index.Scope.Evaluated) != 2 {
		t.Fatalf("evaluated %v, scores %v", v.Index.Scope.Evaluated, scores)
	}
	// Annex 3 point 1: the report with Cursor's action validates against
	// report.v2 (RenderV2JSON validates before returning).
	js, err := report.RenderV2JSON(v)
	if err != nil {
		t.Fatalf("report with Cursor's action: %v", err)
	}
	if !strings.Contains(string(js), `"code": "sandbox_read_boundary_workspace"`) && !strings.Contains(string(js), `"code":"sandbox_read_boundary_workspace"`) {
		t.Error("no sandbox_read_boundary_workspace action — test would be vacuous")
	}
	cursorFindings := 0
	for _, f := range v.Findings {
		if f.Params.Agent == "Cursor" {
			cursorFindings++
		}
	}
	if cursorFindings == 0 {
		t.Error("no Cursor finding")
	}
	low, high := "Claude Code", "Cursor" // a tie goes by name: Claude Code first
	if scores["Cursor"] < scores["Claude Code"] {
		low, high = "Cursor", "Claude Code"
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		of := cat.Text("html.index.of", nil)
		if !strings.Contains(page, `aria-label="`+htmlEscape(strconv.Itoa(scores[low])+of)+`"`) {
			t.Errorf("%s: big score is not %s's", lang, low)
		}
		var band string
		for _, a := range v.Index.Agents {
			if a.Agent == high {
				band = a.Band
			}
		}
		if other := htmlEscape(cat.Text("html.step.1.other", map[string]string{"agent": high, "score": strconv.Itoa(scores[high]), "band": cat.Text("band."+band, nil)})); !strings.Contains(page, other) {
			t.Errorf("%s: no line %q", lang, other)
		}
		if !strings.Contains(page, htmlEscape(cat.Text("html.unknown.body.cursor", map[string]string{"agent": "Cursor"}))) {
			t.Errorf("%s: no Cursor paragraph", lang)
		}
	}
}
