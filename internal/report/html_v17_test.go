package report_test

import (
	"html"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/scheduledtasks"
)

// v17Exe is a path with spaces, accents and a single quote.
const (
	v17Home = `C:\Users\Ana María`
	v17Exe  = `C:\Users\Ana María\O'Brien tools\rosmarops.exe`
)

// v17Report is the one-agent report with the weekly review's inputs:
// in sets them (and anything else) before BuildV2.
func v17Report(t *testing.T, in func(*report.V2Input)) report.V2 {
	t.Helper()
	g := v2Graph(t)
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	input := report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("ab", 32), Base: base, RepoRoots: []string{"/src/a", "/src/b"},
		Families: [][]string{{"/src/a"}, {"/src/b"}}, V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: v2Now,
		ExePath: v17Exe,
	}
	if in != nil {
		in(&input)
	}
	v, _ := report.BuildV2(input)
	return v
}

// column cuts the n-th column (1–3) of div.more out of page, from its h3.
func column(t *testing.T, page string, n int) string {
	t.Helper()
	_, more, ok := strings.Cut(page, `<div class="more">`)
	if !ok {
		t.Fatal("no div.more")
	}
	more, _, _ = strings.Cut(more, `<p class="foot">`)
	cols := strings.Split(more, `<h3>`)
	if len(cols) != 4 {
		t.Fatalf("%d columns", len(cols)-1)
	}
	return `<h3>` + cols[n]
}

// fakeTasks is the fake adapter's scheduled tasks for each state of column
// 2 (J5): none, this exe's own task (the §17.3 command's, Fridays 09:00),
// and another rosmarops.exe's.
func fakeTasks(state string) []platform.ScheduledTask {
	own := &platform.RosmarOpsTask{
		Actions: 1, Execute: v17Exe, ExecuteExpanded: v17Exe, Arguments: "run --lang es --unattended",
		WorkingDirectory: `C:\Users\Ana María\O'Brien tools`, UserID: `PC\ana`, LogonType: "Interactive", RunLevel: "Limited",
		Triggers: []platform.TaskTrigger{{Class: "MSFT_TaskWeeklyTrigger", DaysOfWeek: 32, Start: "2026-10-30T09:00:00"}},
	}
	backup := platform.ScheduledTask{Name: "Backup"}
	switch state {
	case "own":
		return []platform.ScheduledTask{backup, {Name: "RosmarOps weekly review", RosmarOps: own}}
	case "other":
		other := *own
		other.Execute, other.ExecuteExpanded = `%USERPROFILE%\Downloads\rosmarops.exe`, v17Home+`\Downloads\rosmarops.exe`
		return []platform.ScheduledTask{backup, {Name: "RosmarOps weekly review", RosmarOps: &other}}
	}
	return []platform.ScheduledTask{backup}
}

var v17Self = scheduledtasks.Self{Exe: v17Exe, Domain: "PC", User: "ana", Langs: []string{"en", "es"}}

// §17.3: the three states of column 2 from a fake adapter; the command,
// literal, in the hidden pre and in «by hand», where §18's button copies
// the element right before it: the command alone; the agent note under the
// button with no task or another exe's, not with its own (J11); the
// notification line only when the notification did not show.
func TestRenderHTML_WeeklyStates(t *testing.T) {
	for _, state := range []string{"none", "own", "other"} {
		weekly := scheduledtasks.WeeklyOf(fakeTasks(state), v17Self)
		if weekly.State != state {
			t.Fatalf("%s: fake adapter gives %+v", state, weekly)
		}
		for _, reason := range []string{"", "disabled_by_group_policy"} {
			v := v17Report(t, func(in *report.V2Input) { in.Weekly, in.NotificationUnavailable = weekly, reason })
			for _, lang := range []string{"en", "es"} {
				cat, _ := catalog.Load(lang)
				col := column(t, report.RenderHTML(v, cat, v17Home, time.UTC), 2)
				esc := func(k string, p map[string]string) string { return htmlEscape(cat.Text(k, p)) }
				cmd := html.EscapeString(report.WeeklyCommand(v17Exe, lang))
				copyBlock := strings.Contains(col, `<pre hidden># `+esc("html.weekly.prompt", nil)+"\n\n"+cmd+`</pre><button type="button" class="wide"`) &&
					strings.Contains(col, esc("html.fix.copy.generic", nil)+`</button><span class="live" aria-live="polite"></span><p class="note">`+esc("html.weekly.agent_note", nil)+`</p><details class="manual">`) &&
					strings.Contains(col, `<details class="manual"><summary><span class="chev" aria-hidden="true">›</span>`+esc("html.fix.manual", nil)+`</summary><p>`+esc("html.weekly.manual.step", nil)+`</p><pre>`+cmd+`</pre>`+
						`<button type="button" class="b2" data-copied="`+esc("html.fix.copied", nil)+`">`+esc("html.weekly.copy_command", nil)+`</button><span class="live" aria-live="polite"></span></details>`)
				if !strings.HasPrefix(col, `<h3>`+esc("html.weekly.title", nil)+`</h3>`) {
					t.Errorf("%s %s: column 2 is not «%s»", state, lang, cat.Text("html.weekly.title", nil))
				}
				switch state {
				case "none":
					if !strings.Contains(col, `<p>`+esc("html.weekly.pitch", nil)+`</p>`) || !copyBlock {
						t.Errorf("%s %s: pitch or copy block missing:\n%s", state, lang, col)
					}
				case "own":
					on := esc("html.weekly.on", map[string]string{"day": cat.Text("html.day.friday", nil), "time": "09:00"})
					if !strings.Contains(col, `<p>`+on+`</p>`) || strings.Contains(col, "<button") || strings.Contains(col, "<pre") ||
						strings.Contains(col, esc("html.weekly.agent_note", nil)) {
						t.Errorf("%s %s: no %q, or a button:\n%s", state, lang, on, col)
					}
				case "other":
					other := esc("html.weekly.other_exe", map[string]string{"path": `~\Downloads\rosmarops.exe`})
					if !strings.Contains(col, `<p>`+other+`</p>`) || !copyBlock || strings.Contains(col, esc("html.weekly.pitch", nil)) {
						t.Errorf("%s %s: no %q or copy block:\n%s", state, lang, other, col)
					}
				}
				warn := `<p class="warn">` + esc("html.notify.unavailable.disabled_by_group_policy", nil) + `</p>`
				if strings.Contains(col, warn) != (reason != "") || strings.Count(col, `class="warn"`) > 1 {
					t.Errorf("%s %s reason %q: notification line wrong:\n%s", state, lang, reason, col)
				}
			}
		}
	}
}

// psSingleQuoted is every single-quoted string of a PowerShell line, as
// PowerShell reads it: two quotes inside are one.
func psSingleQuoted(line string) []string {
	var out []string
	for i := 0; i < len(line); i++ {
		if line[i] != '\'' {
			continue
		}
		var b strings.Builder
		for i++; i < len(line); i++ {
			if line[i] == '\'' {
				if i+1 < len(line) && line[i+1] == '\'' {
					b.WriteByte('\'')
					i++
					continue
				}
				break
			}
			b.WriteByte(line[i])
		}
		out = append(out, b.String())
	}
	return out
}

// §17.3: the command is the five lines of J4, literal; the exe and its
// folder, quoted, read back as themselves with spaces, accents and a quote.
func TestWeeklyCommandLiteral(t *testing.T) {
	want := `$a = New-ScheduledTaskAction -Execute 'C:\Users\Ana María\O''Brien tools\rosmarops.exe' -Argument 'run --lang es --unattended' -WorkingDirectory 'C:\Users\Ana María\O''Brien tools'
$t = New-ScheduledTaskTrigger -Weekly -DaysOfWeek Friday -At 09:00
$p = New-ScheduledTaskPrincipal -UserId "$env:USERDOMAIN\$env:USERNAME" -LogonType Interactive -RunLevel Limited
$s = New-ScheduledTaskSettingsSet -StartWhenAvailable -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit (New-TimeSpan -Minutes 10) -MultipleInstances IgnoreNew
Register-ScheduledTask -TaskName 'RosmarOps weekly review' -Action $a -Trigger $t -Principal $p -Settings $s -Force`
	got := report.WeeklyCommand(v17Exe, "es")
	if got != want {
		t.Fatalf("command:\n%s\nwant:\n%s", got, want)
	}
	first, _, _ := strings.Cut(got, "\n")
	args := psSingleQuoted(first)
	if len(args) != 3 || args[0] != v17Exe || args[1] != "run --lang es --unattended" || args[2] != `C:\Users\Ana María\O'Brien tools` {
		t.Errorf("read back: %q", args)
	}
	// The task this command creates is this RosmarOps's own (J6).
	task := platform.ScheduledTask{RosmarOps: &platform.RosmarOpsTask{
		Actions: 1, Execute: args[0], ExecuteExpanded: args[0], Arguments: args[1], WorkingDirectory: args[2],
		UserID: `PC\ana`, LogonType: "Interactive", RunLevel: "Limited",
	}}
	if !v17Self.Owns(task) {
		t.Error("the command's task is not recognized as its own")
	}
	if en := report.WeeklyCommand(v17Exe, "en"); !strings.Contains(en, "-Argument 'run --lang en --unattended'") {
		t.Errorf("en: %s", en)
	}
}

// §17.2: column 1 says «no changes» or the counts in one line, a score line
// only for the agent whose score changed, and no series.
func TestRenderHTML_SinceColumn(t *testing.T) {
	v := twoAgentsAfter(t, 0, 29) // Claude Code's previous score is set below
	var ccNow, cuNow int
	for _, a := range v.Index.Agents {
		switch a.Agent {
		case "Claude Code":
			ccNow = *a.Value
		case "Cursor":
			cuNow = *a.Value
		}
	}
	if cuNow == 29 {
		t.Fatal("Cursor's score did not change: test would be vacuous")
	}
	v = twoAgentsAfter(t, ccNow, 29)
	for _, c := range []struct {
		name   string
		counts map[string]int
	}{
		{"same", map[string]int{memory.VerdictStillObserved: 3}},
		{"counts", map[string]int{memory.VerdictNew: 1, memory.VerdictObservedAgain: 1, memory.VerdictNoLongerObserved: 2, memory.VerdictStillObserved: 3}},
	} {
		w := cloneV2(v)
		w.Memory.Reason, w.Memory.Counts = "", c.counts
		for _, lang := range []string{"en", "es"} {
			cat, _ := catalog.Load(lang)
			col := column(t, renderHTML(t, lang, w), 1)
			esc := func(k string, p map[string]string) string { return htmlEscape(cat.Text(k, p)) }
			if !strings.HasPrefix(col, `<h3>`+esc("html.since.title", nil)+`</h3>`) {
				t.Errorf("%s %s: column 1 is not «%s»", c.name, lang, cat.Text("html.since.title", nil))
			}
			line := htmlEscape(cat.Plural("html.since.same", 3, map[string]string{"count": "3"}))
			if c.name == "counts" {
				line = esc("html.since.counts", map[string]string{"new": "2", "gone": "2", "still": "3"})
			}
			if !strings.Contains(col, `<p>`+line+`</p>`) {
				t.Errorf("%s %s: no %q in\n%s", c.name, lang, line, col)
			}
			cursor := esc("html.since.score", map[string]string{"agent": "Cursor", "prev": "29", "now": strconv.Itoa(cuNow)})
			if !strings.Contains(col, cursor) {
				t.Errorf("%s %s: no Cursor score line", c.name, lang)
			}
			if strings.Contains(col, ">"+htmlEscape("Claude Code")) || regexp.MustCompile(`\d+ → \d+ → \d+`).MatchString(col) {
				t.Errorf("%s %s: a line for the unchanged agent, or a series:\n%s", c.name, lang, col)
			}
		}
	}
}

// §17.1 and §17.5: the retired titles and notes are not in the HTML (the
// .ai.md's template keeps its own); the assumptions window has the common
// line, one per evaluated agent and the scope, and #as-open opens it; below
// 1100px, one column; the step 1 line when integrity was not checked.
func TestRenderHTML_StripAndAssumptions(t *testing.T) {
	v := twoAgents(t)
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		outside := regexp.MustCompile(`(?s)<template[^>]*>.*?</template>`).ReplaceAllString(page, "")
		for _, k := range []string{"html.memory.title", "html.unknown.title", "html.ai.title", "html.ai.note", "html.card.title", "html.card.note"} {
			if strings.Contains(outside, htmlEscape(cat.Text(k, nil))) {
				t.Errorf("%s: retired %s in the HTML", lang, k)
			}
		}
		column(t, page, 3) // three columns
		_, dlg, ok := strings.Cut(page, `<dialog id="as-dialog"`)
		dlg, _, _ = strings.Cut(dlg, `</dialog>`)
		if !ok {
			t.Fatalf("%s: no assumptions window", lang)
		}
		items := regexp.MustCompile(`<li>([^<]*)</li>`).FindAllStringSubmatch(dlg, -1)
		// §11.3's order: Cursor, the lower score, first.
		want := []string{"html.assume.common", "html.assume.cursor", "html.assume.claude-code", "html.assume.scope"}
		if len(items) != len(want) {
			t.Fatalf("%s: %d lines in the window: %v", lang, len(items), items)
		}
		for i, k := range want {
			if items[i][1] != htmlEscape(cat.Text(k, nil)) {
				t.Errorf("%s: line %d is not %s: %q", lang, i, k, items[i][1])
			}
		}
		if !strings.Contains(page, `<p class="foot"><button type="button" class="b2" id="as-open">`+htmlEscape(cat.Text("html.assume.open", nil))+`</button>`) ||
			!strings.Contains(page, `dlg('as-open','as-dialog','as-close')`) || !strings.Contains(dlg, `id="as-close"`) {
			t.Errorf("%s: #as-open does not open the window", lang)
		}
		narrow := regexp.MustCompile(`@media \(max-width:1099px\)\{[^\n]*\.more\{grid-template-columns:minmax\(0,1fr\);row-gap:20px\}`)
		if !narrow.MatchString(page) || !strings.Contains(page, `.more{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:28px`) {
			t.Errorf("%s: the strip's columns CSS", lang)
		}
		if strings.Contains(page, htmlEscape(cat.Text("html.integrity.not_checked", nil))) {
			t.Errorf("%s: integrity line without cause", lang)
		}
	}
	// One evaluated agent: one agent line.
	one := v17Report(t, func(in *report.V2Input) { in.IntegrityNotChecked = true })
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, one)
		_, dlg, _ := strings.Cut(page, `<dialog id="as-dialog"`)
		dlg, _, _ = strings.Cut(dlg, `</dialog>`)
		if n := strings.Count(dlg, "<li>"); n != 3 || strings.Contains(dlg, htmlEscape(cat.Text("html.assume.cursor", nil))) {
			t.Errorf("%s: one agent, %d lines", lang, n)
		}
		_, step1, _ := strings.Cut(page, `<section class="s1" aria-labelledby="step1">`)
		step1, _, _ = strings.Cut(step1, `</section>`)
		if !strings.Contains(step1, `<p class="meta warn">`+htmlEscape(cat.Text("html.integrity.not_checked", nil))+`</p>`) {
			t.Errorf("%s: no integrity line in step 1", lang)
		}
	}
}

// §18: «Copy the command» copies the element right before it (the page's
// copy script copies a button's previous sibling): the command, exactly,
// without the agent's instruction.
func TestRenderHTML_CopyCommandButton(t *testing.T) {
	for _, state := range []string{"none", "other"} {
		weekly := scheduledtasks.WeeklyOf(fakeTasks(state), v17Self)
		v := v17Report(t, func(in *report.V2Input) { in.Weekly = weekly })
		for _, lang := range []string{"en", "es"} {
			cat, _ := catalog.Load(lang)
			col := column(t, report.RenderHTML(v, cat, v17Home, time.UTC), 2)
			label := htmlEscape(cat.Text("html.weekly.copy_command", nil))
			m := regexp.MustCompile(`<pre>([^<]*)</pre><button type="button" class="b2" data-copied="[^"]*">` + regexp.QuoteMeta(label) + `</button>`).FindStringSubmatch(col)
			if m == nil || strings.Count(col, label) != 1 {
				t.Fatalf("%s %s: no single «%s» button after a pre:\n%s", state, lang, cat.Text("html.weekly.copy_command", nil), col)
			}
			copied := html.UnescapeString(m[1])
			if copied != report.WeeklyCommand(v17Exe, lang) || strings.Contains(copied, cat.Text("html.weekly.prompt", nil)) {
				t.Errorf("%s %s: copies %q", state, lang, copied)
			}
		}
	}
}

// BV-82: the agent button copies the hidden pre (the element right before
// it): «# » and the instruction, a PowerShell comment, a blank line and
// exactly the command, so that pasting it into PowerShell gives no error.
// «Copy the command» still copies the command alone.
func TestRenderHTML_WeeklyCopyIsPowerShell(t *testing.T) {
	weekly := scheduledtasks.WeeklyOf(fakeTasks("none"), v17Self)
	v := v17Report(t, func(in *report.V2Input) { in.Weekly = weekly })
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		col := column(t, report.RenderHTML(v, cat, v17Home, time.UTC), 2)
		m := regexp.MustCompile(`(?s)<pre hidden>(.*?)</pre><button type="button" class="wide"`).FindStringSubmatch(col)
		if m == nil {
			t.Fatalf("%s: no agent copy block", lang)
		}
		copied := html.UnescapeString(m[1])
		want := "# " + cat.Text("html.weekly.prompt", nil) + "\n\n" + report.WeeklyCommand(v17Exe, lang)
		if copied != want {
			t.Errorf("%s: copies\n%s\nwant\n%s", lang, copied, want)
		}
		if strings.Contains(cat.Text("html.weekly.prompt", nil), "\n") {
			t.Errorf("%s: the instruction has more than one line: only the first is a comment", lang)
		}
		manual := regexp.MustCompile(`<pre>([^<]*)</pre><button type="button" class="b2"`).FindStringSubmatch(col)
		if manual == nil || html.UnescapeString(manual[1]) != report.WeeklyCommand(v17Exe, lang) {
			t.Errorf("%s: «%s» does not copy the command alone", lang, cat.Text("html.weekly.copy_command", nil))
		}
	}
}
