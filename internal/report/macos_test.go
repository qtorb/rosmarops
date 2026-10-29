package report_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/report"
)

// macReport is a report with one credential finding, from a run on os,
// with notChecked and the executable's path set.
func macReport(t *testing.T, os string, notChecked []string) report.V2 {
	t.Helper()
	g := v2Graph(t)
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	v1 := report.Build("t", []finding.Finding{
		{ID: "st:1", Severity: finding.SeverityRed, Category: "secret_tracked", GroupDetail: "github_pat_classic", Evidence: finding.Evidence{RepoPath: "/src/a", File: "config.py"}},
	}, nil, 0, 0, 0, 0, nil)
	v1.Platform.OS = os
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("ab", 32), Base: base, RepoRoots: []string{"/src/a", "/src/b"},
		Families: [][]string{{"/src/a"}, {"/src/b"}}, V1: v1, GeneratedAt: v2Now,
		ExePath: "/Users/ana/Downloads/rosmarops-darwin-arm64", NotChecked: notChecked,
	})
	return v
}

// allNotChecked is every code, out of order, with a duplicate and an unknown
// code: the report keeps NotCheckedCodes' order, once each.
var allNotChecked = []string{"display_language", "weekly", "python_cross", "bogus", "scheduledtasks", "avexclusions", "shellprofile", "weekly"}

// assumeDialog cuts the assumptions window out of page.
func assumeDialog(t *testing.T, page string) string {
	t.Helper()
	_, dlg, ok := strings.Cut(page, `<dialog id="as-dialog"`)
	if !ok {
		t.Fatal("no assumptions window")
	}
	dlg, _, _ = strings.Cut(dlg, `</dialog>`)
	return dlg
}

// BV-102 (decision 2026-10-29, macOS vista previa, points 3–6): on macOS
// the assumptions window ends with the macOS scope and the list of what is
// not checked yet, in its order; no weekly review anywhere (no html.weekly.*
// text, no PowerShell command, also not in the AI export the page carries);
// the strip in two columns; the agent prompt names the user's folder with ~
// and the manual step opens the file with TextEdit.
func TestRenderHTML_MacOS(t *testing.T) {
	v := macReport(t, "macos", allNotChecked)
	if want := report.NotCheckedCodes; strings.Join(v.NotChecked, ",") != strings.Join(want, ",") {
		t.Fatalf("NotChecked = %v, want %v", v.NotChecked, want)
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		text := visibleText(page)
		if m := regexp.MustCompile(`\[[a-z][a-z0-9_]*(\.[a-z0-9_]+)+\]`).FindString(text); m != "" {
			t.Errorf("%s: key rendered: %s", lang, m)
		}
		dlg := assumeDialog(t, page)
		scope := `<li>` + htmlEscape(cat.Text("html.assume.scope.macos", nil)) + `</li>`
		var na strings.Builder
		na.WriteString(`<li id="not-checked"><strong>` + htmlEscape(cat.Text("html.na.title", nil)) + `</strong> ` + htmlEscape(cat.Text("html.na.intro", nil)) + `<ul>`)
		for _, c := range report.NotCheckedCodes {
			na.WriteString(`<li>` + htmlEscape(cat.Text("html.na."+c, nil)) + `</li>`)
		}
		na.WriteString(`</ul></li></ul>`)
		if !strings.HasSuffix(dlg, scope+na.String()) {
			t.Errorf("%s: the window does not end with the macOS scope and the list:\n%s", lang, dlg)
		}
		if strings.Contains(dlg, htmlEscape(cat.Text("html.assume.scope", nil))) {
			t.Errorf("%s: the Windows scope on macOS", lang)
		}
		for _, k := range []string{"html.weekly.title", "html.weekly.pitch", "html.weekly.prompt", "html.weekly.manual.step", "html.weekly.agent_note", "html.weekly.copy_command"} {
			if strings.Contains(page, htmlEscape(cat.Text(k, nil))) {
				t.Errorf("%s: %s on macOS", lang, k)
			}
		}
		for _, cmd := range []string{"Register-ScheduledTask", "New-ScheduledTask", "--unattended"} {
			if strings.Contains(page, cmd) {
				t.Errorf("%s: the weekly command (%s) on macOS", lang, cmd)
			}
		}
		if !strings.Contains(page, `<div class="more two">`) || strings.Count(page[strings.Index(page, `<div class="more two">`):], `<h3>`) != 2 {
			t.Errorf("%s: the strip is not two columns", lang)
		}
		if !strings.Contains(page, ".more.two{grid-template-columns:repeat(2,minmax(0,1fr))}") {
			t.Errorf("%s: no two-column style", lang)
		}
		if strings.Contains(page, "%USERPROFILE%") || !strings.Contains(page, "~/.claude/settings.json") {
			t.Errorf("%s: the agent prompt does not name the folder with ~", lang)
		}
		open := strings.SplitN(cat.Text("html.fix.manual.open.macos", nil), "{", 2)[0]
		winOpen := strings.SplitN(cat.Text("html.fix.manual.open", nil), "{", 2)[0]
		if !strings.Contains(page, htmlEscape(open)) || (open != winOpen && strings.Contains(page, htmlEscape(winOpen))) {
			t.Errorf("%s: the manual step does not open the file with TextEdit", lang)
		}
		if lang == "en" && (!strings.Contains(page, "TextEdit") || strings.Contains(page, "Notepad")) {
			t.Error("en: Notepad on macOS")
		}
	}
}

// Off macOS nothing changes: the Windows scope, no list (even with codes),
// the weekly column, %USERPROFILE% and Notepad.
func TestRenderHTML_WindowsUnchangedByMacOS(t *testing.T) {
	v := macReport(t, "windows", allNotChecked)
	if v.NotChecked != nil {
		t.Errorf("NotChecked on Windows: %v", v.NotChecked)
	}
	cat, _ := catalog.Load("en")
	page := renderHTML(t, "en", v)
	dlg := assumeDialog(t, page)
	if !strings.HasSuffix(dlg, `<li>`+htmlEscape(cat.Text("html.assume.scope", nil))+`</li></ul>`) || strings.Contains(page, "not-checked") {
		t.Errorf("the window changed on Windows:\n%s", dlg)
	}
	for _, want := range []string{htmlEscape(cat.Text("html.weekly.title", nil)), "Register-ScheduledTask", "%USERPROFILE%/.claude/settings.json", "Notepad", `<div class="more">`} {
		if !strings.Contains(page, want) {
			t.Errorf("missing on Windows: %s", want)
		}
	}
	for _, gone := range []string{"TextEdit", "Keychain", ".more.two", `class="more two"`} {
		if strings.Contains(page, gone) {
			t.Errorf("on Windows: %s", gone)
		}
	}
}

// The Markdown lists what is not checked in Coverage, after the gaps,
// without counting it as a gap; the AI export has the macOS limits and the
// list next to them; the JSON carries neither.
func TestMacOSMarkdownAIAndJSON(t *testing.T) {
	mac := macReport(t, "macos", allNotChecked)
	win := macReport(t, "windows", nil)
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		md := report.RenderV2Markdown(mac, cat, time.UTC)
		var list strings.Builder
		list.WriteString("**" + cat.Text("html.na.title", nil) + "** " + cat.Text("html.na.intro", nil) + "\n\n")
		for _, c := range report.NotCheckedCodes {
			list.WriteString("- " + cat.Text("html.na."+c, nil) + "\n")
		}
		_, coverage, _ := strings.Cut(md, "## "+cat.Text("section.coverage", nil)+"\n")
		coverage, _, _ = strings.Cut(coverage, "## "+cat.Text("section.legacy", nil))
		if !strings.Contains(coverage, list.String()) {
			t.Errorf("%s: the list is not in Coverage:\n%s", lang, coverage)
		}
		// Not a gap: the status and scope lines are Windows's.
		head := func(md string) string { h, _, _ := strings.Cut(md, "## "); return h }
		if head(md) != head(report.RenderV2Markdown(win, cat, time.UTC)) {
			t.Errorf("%s: the list changed the scope line", lang)
		}
		if strings.Contains(report.RenderV2Markdown(win, cat, time.UTC), cat.Text("html.na.title", nil)) {
			t.Errorf("%s: the list on Windows", lang)
		}

		ai := report.RenderAIExport(mac, cat, "/h")
		if !strings.Contains(ai, cat.Text("ai.limits.macos", nil)+"\n\n"+cat.Text("html.na.title", nil)+" "+cat.Text("html.na.intro", nil)+"\n\n") ||
			strings.Contains(ai, cat.Text("ai.limits", nil)) {
			t.Errorf("%s: the AI export's limits on macOS:\n%s", lang, ai[max(0, len(ai)-800):])
		}
		for _, c := range report.NotCheckedCodes {
			if !strings.Contains(ai, "- "+cat.Text("html.na."+c, nil)+"\n") {
				t.Errorf("%s: %s not in the AI export", lang, c)
			}
		}
		if strings.Contains(ai, "%USERPROFILE%") {
			t.Errorf("%s: %%USERPROFILE%% in the AI export", lang)
		}
		if aw := report.RenderAIExport(win, cat, "/h"); !strings.HasSuffix(aw, cat.Text("ai.limits", nil)+"\n") {
			t.Errorf("%s: the AI export changed on Windows", lang)
		}
	}
	js, err := report.RenderV2JSON(mac)
	if err != nil {
		t.Fatal(err)
	}
	wj, _ := report.RenderV2JSON(win)
	for _, c := range append([]string{"NotChecked", "not_checked\""}, report.NotCheckedCodes[1:]...) {
		if strings.Contains(string(js), c) && !strings.Contains(string(wj), c) {
			t.Errorf("%s in the JSON", c)
		}
	}
}
