package report

import (
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/notify"
	"github.com/qtorb/rosmarops/internal/scan/scheduledtasks"
)

// The bottom strip of informe HTML v3 §17 (decision 2026-10-26, integridad y
// revisión semanal, J4, J5, J7, J8): since the last review, the weekly
// review, fix it with your agent; the footer with the analysis assumptions.

// moreCSS is the rest of §17's style (the columns are in the base CSS, the
// window shares §16.2's).
const moreCSS = `
.more button.wide{margin-top:4px}
.more .cardl,.more .note{font-size:13px;margin:6px 0 0}
.more .sep{height:8px}
.warn{color:var(--amb)}
.foot #as-open{margin:0 12px 0 0}
#as-dlg-title{font-size:17px;font-weight:700;margin:0 0 10px}
#as-dialog ul{font-size:14px;line-height:1.5;padding-left:18px;margin:0}`

// renderMore writes div.more (§17.1–§17.4).
func (c htmlCtx) renderMore(local time.Time) {
	c.w(`<div class="more"><div><h3>%s</h3>`, c.t("html.since.title", nil))
	c.renderSince()
	c.w(`</div><div><h3>%s</h3>`, c.t("html.weekly.title", nil))
	c.renderWeekly()
	c.w(`</div><div><h3>%s</h3><p>%s</p>`, c.t("html.agentfix.title", nil), c.t("html.agentfix.body", nil))
	c.w(`<button type="button" class="b2" id="ai-copy" data-done="%s">%s</button><span class="live" aria-live="polite"></span><button type="button" class="b2" id="ai-download" data-file="rosmarops-ai-%s.md">%s</button><template id="ai-export">%s</template>`,
		c.t("html.fix.copied", nil), c.t("html.ai.copy", nil), local.Format("2006-01-02"), c.t("html.ai.download", nil),
		html.EscapeString(RenderAIExport(c.v, c.cat, c.home)))
	if a, ok := c.scored(); ok {
		if svg, text, ok := index.Card(*a, c.v.Silenced.Count, local, c.cat); ok {
			day := local.Format("2006-01-02")
			c.w(`<div class="sep"></div><button type="button" class="b2" id="card-button" data-svg="rosmarops-card-%s.svg" data-txt="rosmarops-card-%s.txt">%s</button><template id="card-svg">%s</template><template id="card-txt">%s</template><p class="cardl">%s</p>`,
				day, day, c.t("html.card.button", nil), svg, html.EscapeString(text), c.t("html.card.line", nil))
		}
	}
	c.w(`</div></div>`)
}

// renderSince is column 1 (§17.2): the first run, or not comparable, or
// one line of counts; a line per agent whose score changed; the new agents
// and MCP servers; the fold of moved and gone findings.
func (c htmlCtx) renderSince() {
	m := c.v.Memory
	// BV-91 (D4): a server that points somewhere else now, before the rest.
	for _, s := range c.v.MCPChanged {
		c.w(`<p>%s</p>`, c.t("html.memory.mcp_changed", c.mcpParams(s)))
	}
	switch {
	case c.v.memoryLost: // B8: it ran before; never «first run»
		c.w(`<p>%s</p>`, c.t("html.memory.lost", nil))
	case !c.v.memoryPrev:
		c.w(`<p>%s</p>`, c.t("html.memory.first", nil))
	case m.Reason != "":
		c.w(`<p>%s</p>`, c.t("memory.not_comparable", map[string]string{"reason": c.cat.Text("memory.reason."+m.Reason, nil)}))
	default:
		newN := m.Counts[memory.VerdictNew] + m.Counts[memory.VerdictObservedAgain]
		gone, still := m.Counts[memory.VerdictNoLongerObserved], m.Counts[memory.VerdictStillObserved]
		if newN == 0 && gone == 0 {
			c.w(`<p>%s</p>`, html.EscapeString(c.cat.Plural("html.since.same", still, map[string]string{"count": strconv.Itoa(still)})))
		} else {
			c.w(`<p>%s</p>`, c.t("html.since.counts", map[string]string{"new": strconv.Itoa(newN), "gone": strconv.Itoa(gone), "still": strconv.Itoa(still)}))
		}
	}
	for _, a := range c.scoredAgents() {
		if cmp := a.Comparison; cmp.Status == index.CompareDirect && cmp.Previous != nil && *cmp.Previous != *a.Value {
			c.w(`<p>%s</p>`, c.t("html.since.score", map[string]string{"agent": a.Agent, "prev": strconv.Itoa(*cmp.Previous), "now": strconv.Itoa(*a.Value)}))
		}
	}
	for _, a := range c.v.NewAgents {
		c.w(`<p>%s</p>`, c.t("html.memory.new_agent", map[string]string{"agent": a}))
	}
	for _, s := range c.v.NewMCP {
		c.w(`<p>%s</p>`, c.t("html.memory.new_mcp", c.mcpParams(s)))
	}
	if !c.v.memoryPrev {
		return
	}
	var lines []string
	for _, mv := range m.Moved {
		lines = append(lines, c.t("memory.moved", map[string]string{"rule": mv.Rule}))
	}
	for _, g := range m.NoLongerObserved {
		report := html.EscapeString(g.LastReport)
		// The earlier run's page in this language; before U7, its one page.
		for _, name := range []string{PageName(g.LastReport, c.cat.Lang()), g.LastReport + ".html"} {
			if c.exists(name) {
				report = `<a href="` + html.EscapeString(name) + `">` + html.EscapeString(name) + `</a>`
				break
			}
		}
		lines = append(lines, c.withHTML("memory.no_longer_observed.line", map[string]string{
			"severity": c.cat.Text("severity."+g.Severity, nil), "rule": g.Rule, "open_since": g.OpenSince,
		}, "last_report", report))
	}
	if len(lines) > 0 {
		c.w(`<details><summary>%s</summary>`, html.EscapeString(c.cat.Plural("html.memory.more", len(lines), map[string]string{"count": strconv.Itoa(len(lines))})))
		for _, l := range lines {
			c.w(`<p>%s</p>`, l)
		}
		c.w(`</details>`)
	}
}

// mcpParams are the {name}, {agent} and {destination} of a new or changed MCP
// server line (BV-91, D4). The outer c.t escapes the whole line, so the host is
// escaped once and the HTML never carries a raw "https://".
func (c htmlCtx) mcpParams(s MCPServerLine) map[string]string {
	return mcpParams(c.cat, s)
}

// renderWeekly is column 2 (§17.3): scheduled, with its day and time; or
// the pitch, or another copy's task, and the block that copies the command.
// A scheduled review whose notification did not show says why.
func (c htmlCtx) renderWeekly() {
	w := c.v.weekly
	switch w.State {
	case scheduledtasks.WeeklyOwn:
		c.w(`<p>%s</p>`, c.t("html.weekly.on", map[string]string{"day": c.cat.Text("html.day."+w.Day, nil), "time": w.Time}))
	case scheduledtasks.WeeklyOther:
		c.w(`<p>%s</p>`, c.t("html.weekly.other_exe", map[string]string{"path": userPath(w.Other, c.home, "~")}))
		c.renderWeeklyCopy()
	default:
		c.w(`<p>%s</p>`, c.t("html.weekly.pitch", nil))
		c.renderWeeklyCopy()
	}
	if r := c.v.NotificationUnavailable; r != "" {
		c.w(`<p class="warn">%s</p>`, c.t("html.notify.unavailable."+r, nil))
	}
}

// renderWeeklyCopy is the block that copies the command, as in step 2: the
// hidden pre with the instruction (a PowerShell comment) and the command, the main button, the
// live notice, the note on the agent the button is for (J11), and «I'd
// rather do it by hand» with the visible command and a button that copies
// only the command (§18).
func (c htmlCtx) renderWeeklyCopy() {
	if c.v.exePath == "" {
		return // the executable's path is not known: no command to give
	}
	cmd := html.EscapeString(WeeklyCommand(c.v.exePath, c.cat.Lang()))
	// The instruction as a PowerShell comment («# »), so that the text
	// pasted into PowerShell runs without error (BV-82).
	c.w(`<pre hidden># %s

%s</pre><button type="button" class="wide" data-copied="%s">%s</button><span class="live" aria-live="polite"></span><p class="note">%s</p>`,
		c.t("html.weekly.prompt", nil), cmd, c.t("html.fix.copied", nil), c.t("html.fix.copy.generic", nil), c.t("html.weekly.agent_note", nil))
	c.w(`<details class="manual"><summary><span class="chev" aria-hidden="true">›</span>%s</summary><p>%s</p><pre>%s</pre><button type="button" class="b2" data-copied="%s">%s</button><span class="live" aria-live="polite"></span></details>`,
		c.t("html.fix.manual", nil), c.t("html.weekly.manual.step", nil), cmd, c.t("html.fix.copied", nil), c.t("html.weekly.copy_command", nil))
}

// psQuote is s in PowerShell single quotes, the inner quote doubled.
func psQuote(s string) string { return "'" + psQuoteEscaper.Replace(s) + "'" }

// psQuoteEscaper doubles every character PowerShell takes as a single quote:
// the ASCII ' and the typographic U+2018–U+201B (decision 2026-10-27,
// blindaje, B7).
var psQuoteEscaper = strings.NewReplacer(
	"'", "''",
	"\u2018", "\u2018\u2018", "\u2019", "\u2019\u2019",
	"\u201a", "\u201a\u201a", "\u201b", "\u201b\u201b",
)

// WeeklyCommand is the PowerShell command of J4 (v3 §17.3) for exe, in
// language lang: a weekly task, Fridays at 09:00, as the current user,
// logged on and not elevated, created or updated with -Force.
func WeeklyCommand(exe, lang string) string {
	folder := exe
	if i := strings.LastIndexAny(exe, `\/`); i >= 0 {
		folder = exe[:i]
	}
	return "$a = New-ScheduledTaskAction -Execute " + psQuote(exe) + " -Argument 'run --lang " + lang + " --unattended' -WorkingDirectory " + psQuote(folder) + "\n" +
		"$t = New-ScheduledTaskTrigger -Weekly -DaysOfWeek Friday -At 09:00\n" +
		`$p = New-ScheduledTaskPrincipal -UserId "$env:USERDOMAIN\$env:USERNAME" -LogonType Interactive -RunLevel Limited` + "\n" +
		"$s = New-ScheduledTaskSettingsSet -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit (New-TimeSpan -Minutes 10) -MultipleInstances IgnoreNew\n" +
		"Register-ScheduledTask -TaskName 'RosmarOps weekly review' -Action $a -Trigger $t -Principal $p -Settings $s -Force"
}

// renderFoot writes the footer and the assumptions window
// (§17.5).
func (c htmlCtx) renderFoot() {
	// BV-100: the threat-data stamp, next to the footer. No match is not clean.
	if c.v.Provenance.IntelVersion != "" {
		c.w(`<p class="foot">%s</p>`, html.EscapeString(c.t("report.intel_stamp",
			map[string]string{"version": c.v.Provenance.IntelVersion, "date": c.v.Provenance.IntelPublishedAt})))
	}
	c.w(`<p class="foot"><button type="button" class="b2" id="as-open">%s</button>%s</p>`,
		c.t("html.assume.open", nil), c.t("html.legacy.pointer", map[string]string{"file": c.v.base + ".md"}))
}

// renderAssumeDialog is the window of the analysis assumptions (§17.5): the
// common line, one per evaluated agent in the columns' order, the scope.
func (c htmlCtx) renderAssumeDialog() {
	c.w(`<dialog id="as-dialog" aria-labelledby="as-dlg-title" hidden><button type="button" class="b2" id="as-close">%s</button><h2 id="as-dlg-title">%s</h2><ul><li>%s</li>`,
		c.t("html.ov.close", nil), c.t("html.assume.title", nil), c.t("html.assume.common", nil))
	for _, a := range overviewColumns(c.v) {
		product := a.Product
		if product == "" {
			continue
		}
		c.w(`<li>%s</li>`, c.t("html.assume."+product, nil))
	}
	c.w(`<li>%s</li></ul></dialog>`, c.t("html.assume.scope", nil))
}

// dialogJS opens and closes a window (v3 §16.2, §17.5): showModal, close,
// Esc natively, the focus back on its button; without showModal it shows in
// place under the button.
const dialogJS = `function dlg(o,d,c){o=document.getElementById(o);d=document.getElementById(d);c=document.getElementById(c);if(!o||!d)return;
o.addEventListener('click',function(){d.hidden=false;if(typeof d.showModal==='function'){d.showModal()}else{d.classList.add('inline');d.setAttribute('open','');o.parentNode.insertBefore(d,o.nextSibling)}});
d.addEventListener('close',function(){d.hidden=true;o.focus()});
c.addEventListener('click',function(){if(typeof d.close==='function'&&typeof d.showModal==='function'){d.close()}else{d.removeAttribute('open');d.hidden=true;o.focus()}})}
dlg('ov-open','ov-dialog','ov-close');dlg('as-open','as-dialog','as-close');`

// moreKeys are the catalog codes of §17.
func moreKeys() []string {
	keys := []string{
		"html.since.title", "html.since.same.one", "html.since.same.other", "html.since.counts", "html.since.score",
		"html.lang_switch", "html.weekly.title", "html.weekly.pitch", "html.weekly.prompt", "html.weekly.manual.step", "html.weekly.on", "html.weekly.other_exe", "html.weekly.agent_note", "html.weekly.copy_command",
		"html.agentfix.title", "html.agentfix.body", "html.card.line",
		"html.assume.open", "html.assume.title", "html.assume.common", "html.assume.claude-code", "html.assume.cursor", "html.assume.scope",
		"html.integrity.not_checked", "html.integrity.state_lost", "html.unattended.pending", "html.config.rejected",
		"html.abort.run_failed", // BV-93, B3: the run-could-not-finish page
	}
	for _, d := range []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"} {
		keys = append(keys, "html.day."+d)
	}
	for _, r := range notify.Reasons {
		keys = append(keys, "html.notify.unavailable."+r)
	}
	return keys
}
