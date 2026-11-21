package report_test

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/profiles"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// fixedRun is two runs of the same computer: the second has lost the
// Supabase secret key of /src/b, and both repositories finished their check,
// so that finding is resolved.
func fixedRun(t *testing.T, completed []string) (report.V2, report.V2) {
	t.Helper()
	sbSecret := "sb_secret_" + strings.Repeat("S", 22) + "_" + strings.Repeat("T", 8)
	first := v2GraphWith(t, []platform.CredentialEntry{
		{Path: "/src/a/dashboard/.env.local", Line: 4, Key: "SUPABASE_SERVICE_ROLE_KEY", Value: secrets.NewSecretValue(serviceRoleJWT)},
		{Path: "/src/b/.env", Line: 1, Key: "SUPABASE_SECRET_KEY", Value: secrets.NewSecretValue(sbSecret)},
	}, nil)
	second := v2GraphWith(t, []platform.CredentialEntry{
		{Path: "/src/a/dashboard/.env.local", Line: 4, Key: "SUPABASE_SERVICE_ROLE_KEY", Value: secrets.NewSecretValue(serviceRoleJWT)},
	}, nil)
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	salt := []byte("memory-salt")
	in := func(prev memory.File, at time.Time) report.V2Input {
		return report.V2Input{
			GraphSHA256: strings.Repeat("ab", 32), Base: base, RepoRoots: []string{"/src/a", "/src/b"},
			ReposCompleted: completed, Families: [][]string{{"/src/a"}, {"/src/b"}},
			V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: at, Salt: salt, Memory: prev,
		}
	}
	a := in(memory.File{}, v2Now)
	a.Graph = &first
	run1, _ := report.BuildV2(a)
	b := in(run1.NextMemory(), v2Now.Add(24*time.Hour))
	b.Graph = &second
	b.PreviousFindings = run1.Findings
	run2, _ := report.BuildV2(b)
	return run1, run2
}

// BV-273 §2: «Since the last check» inside block 1, after the result and
// before the note: the count and the fixed finding's title, only when
// something was fixed; not on the first run.
func TestRenderHTML_ResolvedInBlock1(t *testing.T) {
	run1, run2 := fixedRun(t, []string{"/src/a", "/src/b"})
	if len(run2.Memory.Resolved) != 1 {
		t.Fatalf("resolved %+v", run2.Memory)
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		if p := renderHTML(t, lang, run1); strings.Contains(p, `<div class="r3-done">`) {
			t.Errorf("%s: «Since the last check» on the first run", lang)
		}
		page := renderHTML(t, lang, run2)
		i := strings.Index(page, `<div class="r3-done">`)
		res := strings.Index(page, `<div class="r3-res">`)
		if i < 0 || res < 0 || i < res {
			t.Fatalf("%s: no r3-done after the result", lang)
		}
		block := page[i : i+strings.Index(page[i:], `</div>`)]
		for _, want := range []string{
			`<p class="r3-done-h">` + htmlEscape(cat.Text("html.r3.done.title", nil)) + `</p>`,
			`<p>` + htmlEscape(cat.Text("html.r3.done.count.one", nil)) + `</p>`,
			`<ul><li>`,
		} {
			if !strings.Contains(block, want) {
				t.Errorf("%s: r3-done lacks %q:\n%s", lang, want, block)
			}
		}
		if strings.Contains(block, "[html.") || strings.Contains(block, "{") {
			t.Errorf("%s: unfilled text in %s", lang, block)
		}
		// The column counts it as fixed.
		col := cell(t, page, htmlEscape(cat.Text("html.since.title", nil)))
		fixed := map[string]string{"en": "fixed: 1", "es": "resueltos: 1"}[lang]
		if !strings.Contains(col, fixed) || !strings.Contains(col, htmlEscape(cat.Text("memory.resolved.line", map[string]string{
			"severity": cat.Text("severity."+run2.Memory.Resolved[0].Severity, nil), "rule": run2.Memory.Resolved[0].Rule,
			"open_since": run2.Memory.Resolved[0].OpenSince,
		}))) {
			t.Errorf("%s: the column does not count or name the fixed finding:\n%s", lang, col)
		}
	}
}

// A repository that did not finish its check: nothing is fixed there, the
// finding is not checked, and block 1 says nothing.
func TestRenderHTML_NotCheckedIsNotFixed(t *testing.T) {
	_, run2 := fixedRun(t, []string{"/src/a"})
	if len(run2.Memory.Resolved) != 0 || run2.Memory.Counts[memory.VerdictNotChecked] != 1 {
		t.Fatalf("memory %+v", run2.Memory)
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, run2)
		if strings.Contains(page, `<div class="r3-done">`) {
			t.Errorf("%s: «Since the last check» for a finding that was not checked", lang)
		}
		unchecked := map[string]string{"en": "not checked: 1", "es": "no revisados: 1"}[lang]
		if col := cell(t, page, htmlEscape(cat.Text("html.since.title", nil))); !strings.Contains(col, unchecked) {
			t.Errorf("%s: the column does not count it as not checked:\n%s", lang, col)
		}
	}
}

// withTemplate is the one-agent report with Claude Code's user settings
// holding template id (plus an entry of the person's own), on goos.
func withTemplate(t *testing.T, id, goos string) report.V2 {
	t.Helper()
	p, err := profiles.Permissions(id)
	if err != nil {
		t.Fatal(err)
	}
	p.Allow = append(p.Allow, "Bash(make build)")
	g := v2Graph(t)
	base, _ := knowledge.ClaudeCode()
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("ab", 32), Base: base, RepoRoots: []string{"/src/a", "/src/b"},
		Families: [][]string{{"/src/a"}, {"/src/b"}}, V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: v2Now,
		ClaudeSettings: &platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{{
			Scope: platform.MCPScopeUser, Status: platform.MCPSourceRead, Permissions: p,
		}}},
	})
	v.OS = goos
	return v
}

// BV-273 §4: block 3 says the template is in use and names the next one for
// this system, or that it is the strictest; the card has the «In use» pill.
func TestRenderHTML_TemplateInUse(t *testing.T) {
	for _, c := range []struct{ id, goos, next string }{
		{profiles.P1, "linux", profiles.P2},
		{profiles.P1, "windows", ""}, // «Sandboxed development» is never offered on Windows
		{profiles.P2, "darwin", ""},
	} {
		v := withTemplate(t, c.id, c.goos)
		for _, lang := range []string{"en", "es"} {
			cat, _ := catalog.Load(lang)
			page := renderHTML(t, lang, v)
			want := htmlEscape(cat.Text("html.r3.tpl.in_use", map[string]string{"template": cat.Text("cc.name."+c.id, nil), "agent": "Claude Code"}))
			if c.next != "" {
				want += " " + htmlEscape(cat.Text("html.r3.tpl.next", map[string]string{"next": cat.Text("cc.name."+c.next, nil), "next_line": cat.Text("cc."+c.next+".no.1", nil)}))
			} else {
				want += " " + htmlEscape(cat.Text("html.r3.tpl.top", nil))
			}
			if !strings.Contains(page, `<p class="r3-tpl-fixes">`+want+`</p>`) {
				t.Errorf("%s %s %s: block 3 is not %q", c.id, c.goos, lang, want)
			}
			if c.goos == "windows" && strings.Contains(page, `<p class="r3-tpl-fixes">`+htmlEscape(cat.Text("html.r3.tpl.in_use", map[string]string{"template": cat.Text("cc.name."+c.id, nil), "agent": "Claude Code"}))+" "+htmlEscape(strings.SplitN(cat.Text("html.r3.tpl.next", nil), "{", 2)[0])) {
				t.Errorf("%s windows %s: a next template offered", c.id, lang)
			}
			pill := `<h4>` + htmlEscape(cat.Text("cc.name."+c.id, nil)) + ` <span class="r3-pill green">` + htmlEscape(cat.Text("html.tpl.in_use_pill", nil)) + `</span></h4>`
			if !strings.Contains(page, pill) {
				t.Errorf("%s %s %s: no «In use» pill on the card", c.id, c.goos, lang)
			}
			if n := strings.Count(page, `class="r3-pill green"`); n < 1 {
				t.Errorf("%s %s %s: %d pills", c.id, c.goos, lang, n)
			}
		}
	}
	// Without a template in use, block 3 is as before.
	g := v2Graph(t)
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	for _, lang := range []string{"en", "es"} {
		if page := renderHTML(t, lang, v); strings.Contains(page, `r3-pill green`) && strings.Contains(page, `<span class="r3-pill green">`) {
			t.Errorf("%s: a pill with no template in use", lang)
		}
	}
}
