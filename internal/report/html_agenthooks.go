package report

import (
	"encoding/json"
	"html"
	"strings"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/untrusted"
)

// AgentHooks is RosmarOps inside Claude Code for the report (BV-226 §4, §5).
type AgentHooks struct {
	// ClaudeCode is true when RosmarOps found Claude Code on this computer.
	ClaudeCode bool
	// File is Claude Code's user settings file on this system.
	File string
	// Exe is RosmarOps's path as RosmarOps resolves it: the Store's execution
	// alias when it runs packaged.
	Exe string
	// StatusLineTaken is true when the person already has a status line that
	// is not RosmarOps's.
	StatusLineTaken bool
	// Installed are RosmarOps's own hooks found in Claude Code's settings
	// (platform.OwnHookSessionCheck, platform.OwnHookStatusline).
	Installed []string
}

func (a AgentHooks) installed(h string) bool {
	for _, x := range a.Installed {
		if x == h {
			return true
		}
	}
	return false
}

// hookCommand is the fixed command of one of our hooks for exe: the path, in
// double quotes when it has a space, and the subcommand (CTO-44 §4d).
func hookCommand(exe, sub string) string {
	if strings.ContainsAny(exe, " \t") {
		return `"` + exe + `" ` + sub
	}
	return exe + " " + sub
}

// AgentHooksJSON is what Claude Code's user settings need: hooks.SessionStart
// with the session check and, only when withStatusLine, statusLine.
func AgentHooksJSON(exe string, withStatusLine bool) string {
	type handler struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	type group struct {
		Hooks []handler `json:"hooks"`
	}
	doc := struct {
		Hooks struct {
			SessionStart []group `json:"SessionStart"`
		} `json:"hooks"`
		StatusLine *handler `json:"statusLine,omitempty"`
	}{}
	doc.Hooks.SessionStart = []group{{Hooks: []handler{{Type: "command", Command: hookCommand(exe, platform.OwnHookSessionCheck)}}}}
	if withStatusLine {
		doc.StatusLine = &handler{Type: "command", Command: hookCommand(exe, platform.OwnHookStatusline)}
	}
	b, _ := json.MarshalIndent(doc, "", "  ")
	return string(b)
}

// renderAgentHooks is «RosmarOps inside Claude Code» in «More information»
// (BV-226 §5): only when RosmarOps found Claude Code and its session check is
// not installed yet. First the text to paste into Claude Code, which shows
// the change and asks before saving it; below, the way by hand. RosmarOps
// never writes Claude Code's configuration.
func (c htmlCtx) renderAgentHooks() {
	a := c.v.AgentHooks
	if !a.ClaudeCode || a.Exe == "" || a.File == "" || a.installed(platform.OwnHookSessionCheck) {
		return
	}
	withStatusLine := !a.StatusLineTaken && !a.installed(platform.OwnHookStatusline)
	doc := AgentHooksJSON(a.Exe, withStatusLine)
	c.w(`<div><h3>%s</h3><p>%s</p>`, c.t("html.agenthooks.title", nil), c.t("html.agenthooks.pitch", nil))
	switch {
	case withStatusLine:
		c.w(`<p>%s</p>`, c.t("html.agenthooks.statusline", nil))
	case a.StatusLineTaken:
		c.w(`<p class="mut">%s</p>`, c.t("html.agenthooks.statusline_taken", nil))
	}
	c.w(`<p>%s</p>`, c.t("html.agenthooks.ask", nil))
	prompt := c.cat.Text("html.agenthooks.prompt", map[string]string{"file": userPath(a.File, c.home, c.v.homePrefix())})
	c.w(`<pre hidden>%s

%s</pre><button type="button" class="wide" data-copied="%s">%s</button><span class="live" aria-live="polite"></span>`,
		html.EscapeString(prompt), html.EscapeString(doc), c.t("html.fix.copied", nil), c.t("html.agenthooks.copy", nil))
	mp := map[string]string{"file": userPath(a.File, c.home, "~")}
	c.w(`<details class="manual"><summary><span class="chev" aria-hidden="true">›</span>%s</summary><ol><li>%s</li><li>%s<pre>%s</pre></li></ol></details></div>`,
		c.t("html.fix.manual", nil), c.manualOpen(mp), c.t("html.fix.manual.add", nil), html.EscapeString(untrusted.VisiblePaste(doc)))
}

// renderInstalled is «What RosmarOps has installed» (BV-226 §4): our own
// hooks, recognised by the five conditions, so that they show.
func (c htmlCtx) renderInstalled() {
	a := c.v.AgentHooks
	if len(a.Installed) == 0 {
		return
	}
	c.w(`<div><h3>%s</h3><ul>`, c.t("html.installed.title", nil))
	for _, h := range []string{platform.OwnHookSessionCheck, platform.OwnHookStatusline} {
		if a.installed(h) {
			key := map[string]string{platform.OwnHookSessionCheck: "html.installed.session", platform.OwnHookStatusline: "html.installed.statusline"}[h]
			c.w(`<li>%s</li>`, c.t(key, nil))
		}
	}
	c.w(`</ul></div>`)
}
