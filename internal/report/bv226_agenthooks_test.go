package report_test

import (
	"encoding/json"
	"html"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

// TestAgentHooksSection (BV-226 §5, §7): with Claude Code and no status line
// the JSON carries both pieces; with a status line of the person's own, only
// the hook and statusline_taken; without Claude Code there is no section;
// with our hooks installed, «What RosmarOps has installed» lists them.
func TestAgentHooksSection(t *testing.T) {
	g := v2Graph(t)
	base := buildV2(t, &g, report.Report{})
	const exe = `C:\Users\ana\AppData\Local\Microsoft\WindowsApps\rosmarops.exe`
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		text := func(k string) string { return html.EscapeString(cat.Text(k, nil)) }

		v := base
		v.OS = "windows"
		v.AgentHooks = report.AgentHooks{ClaudeCode: true, File: `C:\Users\ana\.claude\settings.json`, Exe: exe}
		page := renderHTML(t, lang, v)
		if !strings.Contains(page, text("html.agenthooks.title")) || !strings.Contains(page, text("html.agenthooks.statusline")) {
			t.Fatalf("%s: no section or no status line line", lang)
		}
		doc := report.AgentHooksJSON(exe, true)
		var m map[string]any
		if err := json.Unmarshal([]byte(doc), &m); err != nil {
			t.Fatal(err)
		}
		if _, ok := m["hooks"]; !ok {
			t.Errorf("%s: no hooks: %s", lang, doc)
		}
		if sl, ok := m["statusLine"].(map[string]any); !ok || sl["command"] != exe+" statusline" {
			t.Errorf("%s: statusLine %v", lang, m["statusLine"])
		}
		if !strings.Contains(page, html.EscapeString(doc)) {
			t.Errorf("%s: the page does not carry the JSON with both pieces", lang)
		}
		prefix := strings.SplitN(cat.Text("html.agenthooks.prompt", nil), "{file}", 2)[0]
		if !strings.Contains(page, html.EscapeString(prefix)+html.EscapeString(`C:\Users\ana\.claude\settings.json`)) {
			t.Errorf("%s: no prompt with the user settings file", lang)
		}

		v.AgentHooks.StatusLineTaken = true
		page = renderHTML(t, lang, v)
		only := report.AgentHooksJSON(exe, false)
		if strings.Contains(only, "statusLine") || !strings.Contains(page, html.EscapeString(only)) || !strings.Contains(page, text("html.agenthooks.statusline_taken")) {
			t.Errorf("%s: with a status line of the person's own: %s", lang, only)
		}

		v.AgentHooks = report.AgentHooks{Exe: exe, File: `C:\Users\ana\.claude\settings.json`}
		if page := renderHTML(t, lang, v); strings.Contains(page, text("html.agenthooks.title")) {
			t.Errorf("%s: a section without Claude Code", lang)
		}

		v.AgentHooks = report.AgentHooks{ClaudeCode: true, Exe: exe, File: `C:\Users\ana\.claude\settings.json`, Installed: []string{platform.OwnHookSessionCheck, platform.OwnHookStatusline}}
		page = renderHTML(t, lang, v)
		if strings.Contains(page, text("html.agenthooks.title")) || !strings.Contains(page, text("html.installed.title")) ||
			!strings.Contains(page, text("html.installed.session")) || !strings.Contains(page, text("html.installed.statusline")) {
			t.Errorf("%s: installed hooks not listed, or still offered", lang)
		}
	}
	// A path with a space goes in double quotes.
	if doc := report.AgentHooksJSON(`/Users/Ana María/bin/rosmarops`, false); !strings.Contains(doc, `"\"/Users/Ana María/bin/rosmarops\" session-check"`) {
		t.Errorf("quoted path: %s", doc)
	}
}
