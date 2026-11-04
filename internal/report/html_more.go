package report

import (
	"html"
	"strconv"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
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
button.wide{margin-top:4px}
.cardl,.note{font-size:13px;margin:6px 0 0}
.sep{height:8px}
.warn{color:var(--amb)}
.foot #as-open{margin:0 12px 0 0}
#as-dlg-title{font-size:17px;font-weight:700;margin:0 0 10px}
#as-dialog ul{font-size:14px;line-height:1.5;padding-left:18px;margin:0}
.howfix{border-top:1px solid var(--line);margin-top:20px;padding-top:14px}
.howfix h2{font-size:18px;margin:0 0 8px}
.howfix p{font-size:14px;color:var(--mut);line-height:1.45;margin:0 0 10px}
.space p.mut{color:var(--mut)}
.moreinfo{border-top:1px solid var(--line);margin-top:20px;padding-top:4px}
.moreinfo>summary{font:700 16px/1.4 inherit;min-height:52px;display:flex;align-items:center;cursor:pointer}
.migrid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:28px;margin-top:10px}
.migrid>div>h3{font-size:14px;font-weight:700;margin:0 0 6px}
.migrid p{font-size:14px;color:var(--mut);line-height:1.45;margin:0 0 8px}
.migrid .ov{margin:0}
@media (max-width:1100px){.migrid{grid-template-columns:minmax(0,1fr);row-gap:20px}}`

// macOSCSS is the style the macOS preview adds (decision 2026-10-29): the
// strip in two columns, the list of what is not checked yet. Only on macOS,
// so that the Windows page does not change.
const macOSCSS = `
#as-dialog ul ul{margin:4px 0 0}
.more.two{grid-template-columns:repeat(2,minmax(0,1fr))}
@media (max-width:1099px){.more.two{grid-template-columns:minmax(0,1fr)}}`

// osCSS is macOSCSS on macOS, else nothing.
func (c htmlCtx) osCSS() string {
	if c.v.macOS() {
		return macOSCSS
	}
	return ""
}

// renderHowFix is «How to fix it» (BV-158 part 3a): the agent-fix block with the
// copy (primary) and download buttons and the AI export. Always shown, below the
// flow. No card here — the card moved to «How this analysis was made».
func (c htmlCtx) renderHowFix(local time.Time) {
	c.w(`<div class="howfix"><h2>%s</h2><p>%s</p>`, c.t("html.agentfix.title", nil), c.t("html.agentfix.body", nil))
	c.w(`<button type="button" class="wide" id="ai-copy" data-done="%s">%s</button><span class="live" aria-live="polite"></span><button type="button" class="b2" id="ai-download" data-file="rosmarops-ai-%s.md">%s</button><template id="ai-export">%s</template>`,
		c.t("html.fix.copied", nil), c.t("html.ai.copy", nil), local.Format("2006-01-02"), c.t("html.ai.download", nil),
		html.EscapeString(RenderAIExport(c.v, c.cat, c.home)))
	c.w(`</div>`)
}

// renderYourSpace is «Your space» (BV-137 point 7; BV-158 part 3b): always
// shown. Connected, it says whether this run's results were sent, points to the
// space (report.space_has), and — when the automatic review is another copy's —
// warns about it with the copy block. Not connected, it pitches the space and
// says the next check will offer to connect (report.space_pitch/next), with a
// «Create your space» button. Its only external URL is the exact
// https://app.rosmarops.com/space the trust test allows, at most once, here.
func (c htmlCtx) renderYourSpace() {
	c.w(`<section class="space"><h2>%s</h2>`, c.t("report.space_h2", nil))
	if c.v.SpaceConnected {
		line := c.t("report.space_pending", nil)
		switch {
		case c.v.SpaceSentDate != "":
			line = c.t("report.space_sent", map[string]string{"date": c.v.SpaceSentDate})
		case c.v.SpaceRejected:
			line = c.t("report.space_rejected", nil)
		}
		c.w(`<p>%s</p><p class="mut">%s</p><a class="spacebtn" href="https://app.rosmarops.com/space">%s</a>`,
			line, c.t("report.space_has", nil), c.t("report.space_button", nil))
		if c.v.weekly.State == scheduledtasks.WeeklyOther {
			c.w(`<p class="warn">%s</p>`, c.t("html.weekly.other_exe", map[string]string{"path": userPath(c.v.weekly.Other, c.home, "~")}))
			c.renderWeeklyCopy()
		}
	} else {
		// BV-172: a 409/401 during the send forgets the space mid-run, so this
		// report is not connected; still say the check could not be sent before
		// the pitch to reconnect, so the rejection is never silent.
		if c.v.SpaceRejected {
			c.w(`<p class="warn">%s</p>`, c.t("report.space_rejected", nil))
		}
		c.w(`<p>%s</p><p class="mut">%s</p><a class="spacebtn" href="https://app.rosmarops.com/space">%s</a>`,
			c.t("report.space_pitch", nil), c.t("report.space_next", nil), c.t("report.space_create", nil))
	}
	c.w(`</section>`)
}

// renderMoreInfo is the folded «More information» (BV-158 part 3d), shown only
// when this computer is not connected: a two-column grid (one column under
// 1100px) with your agents, since the last check, how your agents work, the
// automatic check (not on macOS, which has none yet), and how this analysis was
// made. Connected, all of this lives in the space and none of it is here.
func (c htmlCtx) renderMoreInfo(local time.Time) {
	c.w(`<details class="moreinfo"><summary>%s</summary><div class="migrid">`, c.t("html.more.title", nil))
	c.w(`<div><h3>%s</h3>`, c.t("html.more.agents", nil))
	c.renderAgentsList()
	c.w(`</div><div><h3>%s</h3>`, c.t("html.since.title", nil))
	c.renderSince()
	c.w(`</div><div class="ovcell">`)
	c.renderOverview()
	c.w(`</div>`)
	if !c.v.macOS() {
		c.w(`<div><h3>%s</h3>`, c.t("html.weekly.title", nil))
		c.renderWeekly()
		c.w(`</div>`)
	}
	c.w(`<div><h3>%s</h3>`, c.t("html.more.analysis", nil))
	c.renderHowMade(local)
	c.w(`</div></div></details>`)
}

// renderAgentsList is «Your agents» (notes and detected-not-evaluated): one
// line per evaluated agent with its score and band, then the evaluated /
// not-evaluated coverage line. It re-homes the scores and coverage that left
// step 1.
func (c htmlCtx) renderAgentsList() {
	for _, o := range c.scoredAgents() {
		c.w(`<p>%s</p>`, c.t("html.step.1.other", map[string]string{"agent": o.Agent, "score": strconv.Itoa(*o.Value), "band": c.cat.Text("band."+o.Band, nil)}))
	}
	ev, ne := c.v.Index.Scope.Evaluated, c.v.Index.Scope.NotEvaluated
	switch {
	case len(ev) > 0 && len(ne) > 0:
		c.w(`<p>%s</p>`, c.t("html.step.1.coverage", map[string]string{"evaluated": listNames(c.cat, ev), "pending": listNames(c.cat, ne)}))
	case len(ev) > 0:
		c.w(`<p>%s</p>`, c.t("html.step.1.coverage.all", map[string]string{"evaluated": listNames(c.cat, ev)}))
	case len(ne) > 0:
		c.w(`<p>%s</p>`, c.t("html.step.1.coverage.none", map[string]string{"pending": listNames(c.cat, ne)}))
	}
}

// renderHowMade is «How this analysis was made»: the test/template key count,
// the threat-data stamp, when the agents' knowledge was reviewed, the
// assumptions button, the share card and the pointer to the technical report.
// It re-homes what left step 1 and the old footer.
func (c htmlCtx) renderHowMade(local time.Time) {
	if n := c.v.TestData.Count; n > 0 {
		c.w(`<p>%s</p>`, html.EscapeString(c.cat.Plural("html.testdata", n, map[string]string{"count": strconv.Itoa(n)})))
	}
	if c.v.Provenance.IntelVersion != "" {
		c.w(`<p class="foot">%s</p>`, html.EscapeString(c.t("report.intel_stamp",
			map[string]string{"version": c.v.Provenance.IntelVersion, "date": c.v.Provenance.IntelPublishedAt})))
	}
	c.w(`<p>%s</p>`, c.t("html.knowledge.reviewed", map[string]string{"date": knowledgeDate(c.cat, knowledge.ReviewedOn)}))
	c.w(`<p class="foot"><button type="button" class="b2" id="as-open">%s</button>%s</p>`,
		c.t("html.assume.open", nil), c.t("html.legacy.pointer", map[string]string{"file": c.v.base + ".md"}))
	if a, ok := c.scored(); ok {
		if svg, text, ok := index.Card(*a, c.v.Silenced.Count, local, c.cat); ok {
			day := local.Format("2006-01-02")
			c.w(`<div class="sep"></div><button type="button" class="b2" id="card-button" data-svg="rosmarops-card-%s.svg" data-txt="rosmarops-card-%s.txt">%s</button><template id="card-svg">%s</template><template id="card-txt">%s</template><p class="cardl">%s</p>`,
				day, day, c.t("html.card.button", nil), svg, html.EscapeString(text), c.t("html.card.line", nil))
		}
	}
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
	if c.v.linux() {
		c.renderWeeklyLinux()
		return
	}
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

// renderWeeklyLinux is the weekly column on Linux (BV-165): the systemd user
// timer, its state the same as on Windows, with the Linux texts and command; or
// html.weekly.no_systemd when this machine has no user systemd.
func (c htmlCtx) renderWeeklyLinux() {
	if c.v.weeklyNoSystemd {
		c.w(`<p>%s</p>`, c.t("html.weekly.no_systemd", nil))
		return
	}
	switch w := c.v.weekly; w.State {
	case scheduledtasks.WeeklyOwn:
		c.w(`<p>%s</p>`, c.t("html.weekly.on", map[string]string{"day": c.cat.Text("html.day."+w.Day, nil), "time": w.Time}))
	case scheduledtasks.WeeklyOther:
		c.w(`<p>%s</p>`, c.t("html.weekly.other_exe", map[string]string{"path": userPath(w.Other, c.home, "~")}))
		c.renderWeeklyCopy()
	default:
		c.w(`<p>%s</p>`, c.t("html.weekly.pitch.linux", nil))
		c.renderWeeklyCopy()
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
	// BV-165: on Linux the command is the systemd units shell snippet and the
	// texts are the Linux ones; elsewhere the PowerShell command. The instruction
	// is written as a «# » comment so pasting it runs without error in both
	// PowerShell and a POSIX shell (BV-82).
	prompt, manualStep := "html.weekly.prompt", "html.weekly.manual.step"
	cmd := WeeklyCommand(c.v.exePath, c.cat.Lang())
	if c.v.linux() {
		prompt, manualStep = "html.weekly.prompt.linux", "html.weekly.manual.step.linux"
		cmd = WeeklyCommandLinux(c.v.exePath)
	}
	cmd = html.EscapeString(cmd)
	c.w(`<pre hidden># %s

%s</pre><button type="button" class="wide" data-copied="%s">%s</button><span class="live" aria-live="polite"></span><p class="note">%s</p>`,
		c.t(prompt, nil), cmd, c.t("html.fix.copied", nil), c.t("html.fix.copy.generic", nil), c.t("html.weekly.agent_note", nil))
	c.w(`<details class="manual"><summary><span class="chev" aria-hidden="true">›</span>%s</summary><p>%s</p><pre>%s</pre><button type="button" class="b2" data-copied="%s">%s</button><span class="live" aria-live="polite"></span></details>`,
		c.t("html.fix.manual", nil), c.t(manualStep, nil), cmd, c.t("html.fix.copied", nil), c.t("html.weekly.copy_command", nil))
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

// WeeklyCommandLinux is the shell command (BV-165) that creates the systemd
// user units for the weekly review — rosmarops-weekly.service and
// rosmarops-weekly.timer under ~/.config/systemd/user, Fridays at 09:00,
// Persistent — and enables the timer. It runs exe with «run --unattended».
// RosmarOps never runs this: the agent or the person does.
func WeeklyCommandLinux(exe string) string {
	return "mkdir -p ~/.config/systemd/user\n" +
		"cat > ~/.config/systemd/user/rosmarops-weekly.service <<'EOF'\n" +
		"[Unit]\n" +
		"Description=RosmarOps weekly review\n" +
		"\n" +
		"[Service]\n" +
		"Type=oneshot\n" +
		"ExecStart=" + systemdExecArg(exe) + " run --unattended\n" +
		"EOF\n" +
		"cat > ~/.config/systemd/user/rosmarops-weekly.timer <<'EOF'\n" +
		"[Unit]\n" +
		"Description=RosmarOps weekly review\n" +
		"\n" +
		"[Timer]\n" +
		"OnCalendar=Fri 09:00\n" +
		"Persistent=true\n" +
		"\n" +
		"[Install]\n" +
		"WantedBy=timers.target\n" +
		"EOF\n" +
		"systemctl --user enable --now rosmarops-weekly.timer"
}

// systemdExecArg is exe as a systemd ExecStart argument: double-quoted when it
// has a space, with backslash and double-quote escaped (systemd quoting), so
// the detection reads back the same path. A plain path is left as is.
func systemdExecArg(exe string) string {
	if !strings.ContainsAny(exe, " \t\"\\") {
		return exe
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(exe) + `"`
}

// renderAssumeDialog is the window of the analysis assumptions (§17.5): the
// common line, one per evaluated agent in the columns' order, the scope;
// then what this system does not check yet (decision 2026-10-29, point 3).
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
	c.w(`<li>%s</li>`, c.t(c.v.osKey("html.assume.scope"), nil))
	if len(c.v.NotChecked) > 0 {
		c.w(`<li id="not-checked"><strong>%s</strong> %s<ul>`, c.t("html.na.title", nil), c.t("html.na.intro", nil))
		for _, code := range c.v.NotChecked {
			c.w(`<li>%s</li>`, c.t("html.na."+code, nil))
		}
		c.w(`</ul></li>`)
	}
	c.w(`</ul></dialog>`)
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
		// BV-165: the Linux weekly review (systemd user timer) and its no-systemd note.
		"html.weekly.pitch.linux", "html.weekly.prompt.linux", "html.weekly.manual.step.linux", "html.weekly.no_systemd",
		"html.assume.scope.linux",
		"html.agentfix.title", "html.agentfix.body", "html.card.line",
		"html.assume.open", "html.assume.title", "html.assume.common", "html.assume.claude-code", "html.assume.cursor", "html.assume.scope", "html.assume.scope.macos",
		"html.na.title", "html.na.intro",
		"html.integrity.not_checked", "html.integrity.state_lost", "html.unattended.pending", "html.config.rejected",
		"html.abort.run_failed", // BV-93, B3: the run-could-not-finish page
	}
	for _, d := range []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"} {
		keys = append(keys, "html.day."+d)
	}
	for _, code := range NotCheckedCodes {
		keys = append(keys, "html.na."+code)
	}
	for _, r := range notify.Reasons {
		keys = append(keys, "html.notify.unavailable."+r)
	}
	return keys
}
