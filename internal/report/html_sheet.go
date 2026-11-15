package report

import (
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"

	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/profiles"
	"github.com/qtorb/rosmarops/internal/rules"
)

// The first screen of the 0.12.0 report (BV-236 §1, prototipo-v6 w0): the
// top bar, and the sheet with the result, the first three findings and how
// to act. It replaces the old header and step 1; everything else stays
// below, in today's order.

// sheetLogo is the RosmarOps logo of the prototype, inline.
const sheetLogo = `<svg class="logo" viewBox="0 0 512 131" role="img" aria-label="RosmarOps"><g transform="translate(19.25,20.25) scale(0.1250)"><line class="stem" x1="185" y1="488" x2="320" y2="57" stroke-width="40" stroke-linecap="round"/><path d="M90,235 Q128.3,337.8 215,405 Q176.7,302.2 90,235Z" fill="#6F8F78"/><path d="M138,98 Q170.7,211.8 255,295 Q222.3,181.2 138,98Z" fill="#6F8F78"/><path d="M233,25 Q236.7,113.8 296,180 Q292.3,91.2 233,25Z" fill="#6F8F78"/><path d="M446,82 Q339.8,141.8 275,245 Q381.2,185.2 446,82Z" fill="#6F8F78"/><path d="M422,193 Q308.2,256.9 235,365 Q348.8,301.1 422,193Z" fill="#6F8F78"/><ellipse cx="283.7" cy="192.5" rx="26" ry="15" transform="rotate(-62.0 283.7 192.5)" fill="#8E7CC3"/><ellipse cx="300.5" cy="224.6" rx="26" ry="15" transform="rotate(6.8 300.5 224.6)" fill="#8E7CC3"/><ellipse cx="216.1" cy="283.3" rx="26" ry="15" transform="rotate(-152.0 216.1 283.3)" fill="#8E7CC3"/><ellipse cx="248.2" cy="266.6" rx="26" ry="15" transform="rotate(-83.2 248.2 266.6)" fill="#8E7CC3"/></g><g class="wm"><path transform="translate(89.75,82) scale(0.06304,-0.06304)" d="M229 0H77V698H408Q456 698 495.0 682.0Q534 666 561.5 636.5Q589 607 604.0 566.0Q619 525 619 475Q619 404 587.5 351.0Q556 298 492 274L629 0H460L338 256H229ZM389 384Q423 384 442.5 401.5Q462 419 462 453V497Q462 531 442.5 548.5Q423 566 389 566H229V384Z"/><path transform="translate(132.24,82) scale(0.06304,-0.06304)" d="M282 -12Q224 -12 178.5 7.0Q133 26 101.5 62.0Q70 98 53.0 149.0Q36 200 36 263Q36 326 53.0 377.0Q70 428 101.5 463.5Q133 499 178.5 518.0Q224 537 282 537Q340 537 385.5 518.0Q431 499 462.5 463.5Q494 428 511.0 377.0Q528 326 528 263Q528 200 511.0 149.0Q494 98 462.5 62.0Q431 26 385.5 7.0Q340 -12 282 -12ZM282 105Q326 105 350.0 132.0Q374 159 374 209V316Q374 366 350.0 393.0Q326 420 282 420Q238 420 214.0 393.0Q190 366 190 316V209Q190 159 214.0 132.0Q238 105 282 105Z"/><path transform="translate(167.79,82) scale(0.06304,-0.06304)" d="M247 -12Q171 -12 119.0 12.5Q67 37 26 82L113 170Q141 139 175.0 121.0Q209 103 252 103Q296 103 313.0 116.5Q330 130 330 153Q330 190 277 197L220 204Q43 226 43 366Q43 404 57.5 435.5Q72 467 98.5 489.5Q125 512 162.5 524.5Q200 537 247 537Q288 537 319.5 531.5Q351 526 375.5 515.5Q400 505 420.5 489.5Q441 474 461 455L376 368Q351 393 319.5 407.5Q288 422 255 422Q217 422 201.0 410.0Q185 398 185 378Q185 357 196.5 345.5Q208 334 240 329L299 321Q472 298 472 163Q472 125 456.0 93.0Q440 61 410.5 37.5Q381 14 339.5 1.0Q298 -12 247 -12Z"/><path transform="translate(199.56,82) scale(0.06304,-0.06304)" d="M69 0V525H217V436H223Q237 478 271.0 507.5Q305 537 364 537Q418 537 456.0 510.0Q494 483 511 431H514Q521 453 535.5 472.5Q550 492 571.0 506.0Q592 520 618.0 528.5Q644 537 674 537Q750 537 790.5 483.0Q831 429 831 329V0H683V317Q683 417 611 417Q578 417 551.0 398.0Q524 379 524 340V0H376V317Q376 417 304 417Q288 417 272.5 412.5Q257 408 244.5 398.0Q232 388 224.5 373.5Q217 359 217 340V0Z"/><path transform="translate(255.92,82) scale(0.06304,-0.06304)" d="M457 0Q417 0 390.5 25.0Q364 50 357 92H351Q339 41 298.0 14.5Q257 -12 197 -12Q118 -12 76.0 30.0Q34 72 34 141Q34 224 94.5 264.5Q155 305 259 305H342V338Q342 376 322.0 398.5Q302 421 255 421Q211 421 185.0 402.0Q159 383 142 359L54 437Q86 484 134.5 510.5Q183 537 266 537Q378 537 434.0 488.0Q490 439 490 345V115H539V0ZM252 91Q289 91 315.5 107.0Q342 123 342 159V221H270Q183 221 183 162V147Q183 118 201.0 104.5Q219 91 252 91Z"/><path transform="translate(291.78,82) scale(0.06304,-0.06304)" d="M69 0V525H217V411H222Q226 433 236.0 453.5Q246 474 262.5 490.0Q279 506 302.0 515.5Q325 525 356 525H382V387H345Q280 387 248.5 370.0Q217 353 217 307V0Z"/><path transform="translate(317.25,82) scale(0.06304,-0.06304)" d="M357 -12Q288 -12 231.0 11.0Q174 34 134.0 79.5Q94 125 72.0 192.5Q50 260 50 349Q50 438 72.0 505.5Q94 573 134.0 618.5Q174 664 231.0 687.0Q288 710 357 710Q426 710 483.0 687.0Q540 664 580.0 618.5Q620 573 642.0 505.5Q664 438 664 349Q664 260 642.0 192.5Q620 125 580.0 79.5Q540 34 483.0 11.0Q426 -12 357 -12ZM357 123Q426 123 464.5 168.0Q503 213 503 297V401Q503 485 464.5 530.0Q426 575 357 575Q288 575 249.5 530.0Q211 485 211 401V297Q211 213 249.5 168.0Q288 123 357 123Z"/><path transform="translate(362.26,82) scale(0.06304,-0.06304)" d="M69 525H217V437H222Q233 481 272.5 509.0Q312 537 363 537Q463 537 514.0 467.0Q565 397 565 263Q565 129 514.0 58.5Q463 -12 363 -12Q337 -12 314.0 -4.5Q291 3 272.5 16.5Q254 30 241.0 48.5Q228 67 222 88H217V-200H69ZM314 108Q356 108 383.5 137.5Q411 167 411 217V308Q411 358 383.5 387.5Q356 417 314 417Q272 417 244.5 396.5Q217 376 217 340V185Q217 149 244.5 128.5Q272 108 314 108Z"/><path transform="translate(400.59,82) scale(0.06304,-0.06304)" d="M247 -12Q171 -12 119.0 12.5Q67 37 26 82L113 170Q141 139 175.0 121.0Q209 103 252 103Q296 103 313.0 116.5Q330 130 330 153Q330 190 277 197L220 204Q43 226 43 366Q43 404 57.5 435.5Q72 467 98.5 489.5Q125 512 162.5 524.5Q200 537 247 537Q288 537 319.5 531.5Q351 526 375.5 515.5Q400 505 420.5 489.5Q441 474 461 455L376 368Q351 393 319.5 407.5Q288 422 255 422Q217 422 201.0 410.0Q185 398 185 378Q185 357 196.5 345.5Q208 334 240 329L299 321Q472 298 472 163Q472 125 456.0 93.0Q440 61 410.5 37.5Q381 14 339.5 1.0Q298 -12 247 -12Z"/></g></svg>`

// kindIcon are the five risk kinds' symbols (design system, assets/Riesgo),
// as the prototype draws them.
var kindIcon = map[string]string{
	rules.KindMoney:        `<rect x="2.5" y="6" width="19" height="12" rx="2"/><circle cx="12" cy="12" r="2.8"/>`,
	rules.KindSendsOut:     `<path d="M4 12h12"/><path d="M12 7l5 5-5 5"/><path d="M20.5 4v16"/>`,
	rules.KindKeys:         `<circle cx="8" cy="15" r="4"/><path d="M10.8 12.2L20 3"/><path d="M16.5 6.5l3 3"/>`,
	rules.KindRunsCommands: `<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7 9l3 3-3 3"/><path d="M12.5 15H17"/>`,
	rules.KindChangesFiles: `<path d="M14 3H7a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2V8z"/><path d="M14 3v5h5"/><path d="M9.5 16.5l5-5 1.5 1.5-5 5H9.5z"/>`,
}

// kindSVG is a kind's symbol, always next to its word (aria-hidden).
func kindSVG(kind string) string {
	return `<span class="ic" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">` + kindIcon[kind] + `</svg></span>`
}

// stateRankSheet orders the states from worst to best for the strip
// (BV-236 §1): nobody > model > bounded > you > blocked; unknown and
// none_known only when no agent has another state.
var stateRankSheet = map[string]int{
	rules.StateNobody: 7, rules.StateModel: 6, rules.StateBounded: 5, rules.StateYou: 4, rules.StateBlocked: 3,
	rules.StateUnknown: 2, rules.StateNoneKnown: 1,
}

// stateClass is the colour class of a state's text (design system, «Quién
// decide»): the colour is the state's, never the symbol's.
var stateClass = map[string]string{
	rules.StateNobody: "s", rules.StateModel: "m", rules.StateBounded: "l", rules.StateYou: "a", rules.StateBlocked: "a",
	rules.StateUnknown: "u", rules.StateNoneKnown: "nk",
}

// stateText is a state's word: the catalog's legend for the four of today,
// the design system's two new ones, and «—» for unknown.
func (c htmlCtx) stateText(state string) string {
	switch state {
	case rules.StateBlocked, rules.StateYou, rules.StateModel, rules.StateNobody:
		return c.t("html.ov.legend."+state, nil)
	case rules.StateBounded, rules.StateNoneKnown:
		return c.t("sheet.state."+state, nil)
	}
	return "—"
}

// bandLetter is the colour class of a band for a score (.nt.r, .a, .g).
func bandLetter(band string) string {
	switch band {
	case index.BandExposed:
		return "r"
	case index.BandOpenGaps:
		return "a"
	case index.BandContained:
		return "g"
	}
	return ""
}

// bandOf is the band of a score, by the bands' ranges (I1).
func bandOf(v int) string {
	switch {
	case v < 50:
		return index.BandExposed
	case v < 80:
		return index.BandOpenGaps
	}
	return index.BandContained
}

// nt is a score in its band's colour.
func nt(v int, band string) string {
	return fmt.Sprintf(`<b class="nt %s">%d</b>`, bandLetter(band), v)
}

// ntOf is nt for a score that may be missing («—»).
func ntOf(v *int, band string) string {
	if v == nil {
		return `<span class="nt u">—</span>`
	}
	if band == "" {
		band = bandOf(*v)
	}
	return nt(*v, band)
}

// cls is the class component: the letter in ink, dashed when provisional,
// «—» without a class.
func cls(c index.Class, extra string) string {
	if c.Class == nil {
		return `<span class="cls none` + extra + `">—</span>`
	}
	prov := ""
	if provisional(c) {
		prov = " prov"
	}
	return `<span class="cls` + prov + extra + `">` + html.EscapeString(*c.Class) + `</span>`
}

// provisional reports a class still provisional (an axis has no data).
func provisional(c index.Class) bool { return c.Status != nil && *c.Status == "provisional" }

// agentName is the name of an agent id, from its score.
func (c htmlCtx) agentName(id string) string {
	if a := c.score[id]; a != nil {
		return a.Agent
	}
	for _, o := range c.v.AgentsOverview {
		if o.AgentID == id {
			return o.Agent
		}
	}
	return id
}

// evaluated are the evaluated agents of the overview, in its order.
func (c htmlCtx) evaluated() []rules.OverviewAgent {
	var out []rules.OverviewAgent
	for _, o := range c.v.AgentsOverview {
		if o.Evaluated {
			out = append(out, o)
		}
	}
	return out
}

// pendingRows are the rows of the findings still pending (not dismissed, not
// accepted), in today's order: today's first, then «What remains» by
// severity, the score's agent's first, one row per credential.
func (c htmlCtx) pendingRows() []rules.Finding {
	todayID := c.v.Today.FindingID
	var list []rules.Finding
	for _, f := range c.v.Findings {
		if f.Accepted == nil {
			list = append(list, f)
		}
	}
	if a, ok := c.scored(); ok {
		sort.SliceStable(list, func(i, j int) bool { return c.ofScoredAgent(list[i], a) && !c.ofScoredAgent(list[j], a) })
	}
	var rows []rules.Finding
	done := map[*credRow]bool{}
	for _, f := range list {
		if r := c.rows[f.ID]; r != nil && containsID(r.ids, todayID) && !done[r] {
			done[r] = true
			rows = append(rows, r.rep)
		}
	}
	for _, sev := range []string{rules.SeverityRed, rules.SeverityYellow} {
		for _, f := range list {
			if r := c.rows[f.ID]; r != nil && f.Severity == sev && !done[r] {
				done[r] = true
				rows = append(rows, r.rep)
			}
		}
	}
	return rows
}

func containsID(ids []string, id string) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// renderBar is the top bar (BV-236 §1): the logo, the other languages, and
// «Your space», filled in brand, with the green dot when connected; it leads
// to today's «Your space» section.
func (c htmlCtx) renderBar() {
	dot := ""
	if c.v.SpaceConnected {
		dot = `<i aria-hidden="true"></i>`
	}
	// data-copyfail is C1, for today's copies when the browser refuses to copy.
	c.w(`<header class="bar" data-copyfail="%s">%s<span class="sp"></span>%s<a class="spacego" href="#your-space">%s%s</a></header>`,
		c.t("copy.C1", nil), sheetLogo, c.langLinks(), dot, c.t("report.space_h2", nil))
}

// renderSheet is the sheet (BV-236 §1, w0).
func (c htmlCtx) renderSheet() {
	v := c.v
	m := c.sheetScore()
	c.w(`<div class="sheetwrap"><article class="sheet" aria-labelledby="sheet-title"><header class="sh-head"><div><h1 class="sh-title" id="sheet-title">%s</h1><div class="sh-sub">%s</div></div><dl class="sh-meta">`,
		c.t("sheet.title", nil), c.t("sheet.sub", nil))
	program := "RosmarOps"
	if v.engineVersion != "" {
		program += " " + v.engineVersion
	}
	agents := len(v.Index.Scope.Evaluated)
	scope := c.cat.Text("sheet.scope", map[string]string{
		"agents":   c.cat.Plural("sheet.scope.agents", agents, map[string]string{"n": strconv.Itoa(agents)}),
		"projects": c.cat.Plural("sheet.scope.projects", v.repos, map[string]string{"n": strconv.Itoa(v.repos)}),
	})
	for _, kv := range [][2]string{
		{"sheet.meta.date", c.local.Format("2006-01-02 15:04")},
		{"sheet.meta.computer", osName(v.OS)},
		{"sheet.meta.scope", scope},
		{"sheet.meta.program", program},
	} {
		c.w(`<div><dt>%s</dt><dd>%s</dd></div>`, c.t(kv[0], nil), html.EscapeString(kv[1]))
	}
	c.w(`</dl></header>`)

	// 1 Result.
	c.w(`<section class="sh-sec" aria-labelledby="sh-result"><h2 class="sh-h" id="sh-result"><span aria-hidden="true">1</span>%s</h2><div class="res">`, c.t("sheet.result", nil))
	if m.Value != nil {
		letter := index.NoClass()
		if m.Letter != nil {
			letter = *m.Letter
		}
		prov, phrase := "", ""
		if provisional(letter) {
			prov = ` <span class="prov">` + c.t("sheet.provisional", nil) + `</span>`
		}
		if letter.Class != nil {
			phrase = c.t("sheet.class."+*letter.Class, nil)
		}
		label := c.t("sheet.result.label", map[string]string{"agent": c.agentName(m.AgentID)})
		if len(v.Dismissed) > 0 { // BV-184: the score leaves out what the person dismissed
			label += " · " + c.t("report.dismissed.note", nil)
		}
		c.w(`<div class="res-main"><div class="res-num"><b class="nt %s big">%d</b><small>/100</small></div><div><div class="res-lab">%s</div><div class="clsline">%s%s%s</div></div></div>`,
			bandLetter(m.Band), *m.Value, label, cls(letter, ""), phrase, prov)
		c.w(`<div class="res-ref"><div class="res-k">%s</div><div>`, c.t("sheet.reference", nil))
		if m.FactoryValue != nil {
			c.w(`%s`, strings.Replace(c.t("sheet.reference.value", map[string]string{"n": "\x00"}), "\x00", "<b>"+strconv.Itoa(*m.FactoryValue)+"</b>", 1))
		} else {
			c.w(`—`)
		}
		c.w(`</div></div><div class="res-int"><div class="res-k">%s</div><div>%s</div></div>`, c.t("sheet.reading", nil), c.reading(m, letter))
	} else {
		reason := index.ReasonNotApplicable
		switch {
		case !v.Coverage.GraphBuilt:
			reason = index.ReasonCoverage
		case m.Unavailable != "":
			reason = m.Unavailable
		}
		c.w(`<div class="res-main"><div class="unavail">%s</div></div>`, c.t("html.index.unavailable", map[string]string{"reason": c.cat.Text("html.index.reason."+reason, nil)}))
	}
	c.renderStrip()
	c.w(`</div></section>`)

	// 2 Findings.
	rows := c.pendingRows()
	count := ""
	switch {
	case len(rows) > 3:
		count = c.t("sheet.findings.count.first", map[string]string{"n": strconv.Itoa(len(rows))})
	case len(rows) == 1:
		count = c.t("sheet.findings.count.one", nil)
	default:
		count = c.t("sheet.findings.count", map[string]string{"n": strconv.Itoa(len(rows))})
	}
	if count != "" {
		count = ` <em>` + count + `</em>`
	}
	c.w(`<section class="sh-sec" aria-labelledby="sh-findings"><h2 class="sh-h" id="sh-findings"><span aria-hidden="true">2</span>%s%s</h2>`, c.t("sheet.findings", nil), count)
	if len(rows) > 0 {
		c.w(`<table class="tbl short"><thead><tr><th scope="col">%s</th><th scope="col">%s</th><th scope="col">%s</th><th scope="col">%s</th><th scope="col" class="n">%s <span class="th-sub">%s</span></th></tr></thead><tbody>`,
			c.t("sheet.col.no", nil), c.t("sheet.col.finding", nil), c.t("sheet.col.affects", nil), c.t("sheet.col.level", nil), c.t("sheet.col.change", nil), c.t("sheet.col.change.sub", nil))
		for i, f := range rows {
			if i == 3 {
				break
			}
			pill := "a"
			if f.Severity == rules.SeverityRed {
				pill = "r"
			}
			c.w(`<tr><td>%d</td><td class="t">%s</td><td class="af">%s</td><td><span class="pill %s">%s</span></td><td class="n">%s</td></tr>`,
				i+1, c.rowTitle(f), html.EscapeString(c.affects(f)), pill, c.t("html.group."+f.Severity, nil), c.ifChange(f))
		}
		c.w(`</tbody></table>`)
	}
	c.w(`</section>`)

	// 3 How to act.
	c.w(`<section class="sh-sec sh-reco" aria-labelledby="sh-act"><h2 class="sh-h" id="sh-act"><span aria-hidden="true">3</span>%s</h2><div class="rrow"><p>%s</p><div class="ways">`,
		c.t("sheet.act", nil), c.t("sheet.act.A1", nil))
	c.w(`<div class="way"><a class="btn p" href="#v-pc" data-view="v-pc">%s</a><small>%s</small></div>`, c.t("sheet.act.profile", nil), c.t("sheet.act.profile.sub", nil))
	c.w(`<div class="way"><a class="btn" href="#todo" data-view="">%s</a><small>%s</small></div>`, c.t("sheet.act.findings", nil), c.t("sheet.act.findings.sub", nil))
	c.w(`</div></div></section>`)

	// Footer.
	p2 := "sheet.foot.P2.final"
	for _, a := range v.Index.Agents {
		if provisional(a.Letter) {
			p2 = "sheet.foot.P2"
		}
	}
	c.w(`<footer class="sh-foot"><span>%s</span><span>%s</span></footer></article></div>`, c.t("sheet.foot.P1", nil), c.t(p2, nil))
}

// osName is the computer's system as the sheet names it: the name of the
// system the review ran on (the engine knows the system, not its version).
func osName(os string) string {
	switch os {
	case "windows":
		return "Windows"
	case "macos":
		return "macOS"
	case "linux":
		return "Linux"
	}
	return os
}

// reading is «Reading» (L1 to L7, BV-236 §7): who sets the score, what the
// person's settings do against the factory score, why class and score
// differ when they do, and the agent with a worse class.
func (c htmlCtx) reading(m index.MachineScore, letter index.Class) string {
	var out []string
	name := c.agentName(m.AgentID)
	out = append(out, c.t("sheet.reading.L1", map[string]string{"agent": name}))
	if m.FactoryValue != nil {
		d := *m.Value - *m.FactoryValue
		switch {
		case d < 0:
			out = append(out, c.t(c.cat.One("sheet.reading.L2.other", -d), nil))
		case d > 0:
			out = append(out, c.t(c.cat.One("sheet.reading.L3.other", d), nil))
		default:
			out = append(out, c.t("sheet.reading.L4", nil))
		}
		if d < 0 {
			out[len(out)-1] = html.EscapeString(c.cat.Plural("sheet.reading.L2", -d, map[string]string{"n": strconv.Itoa(-d)}))
		} else if d > 0 {
			out[len(out)-1] = html.EscapeString(c.cat.Plural("sheet.reading.L3", d, map[string]string{"n": strconv.Itoa(d)}))
		}
	}
	if letter.Class != nil && letter.Coherent != nil && !*letter.Coherent {
		switch l := *letter.Class; {
		case (l == "A" || l == "B") && m.Band == index.BandExposed:
			out = append(out, c.t("sheet.reading.L5", nil))
		case (l == "C" || l == "D") && m.Band == index.BandContained:
			out = append(out, c.t("sheet.reading.L6", nil))
		}
	}
	if m.WorstLetterAgentID != "" {
		if a := c.score[m.WorstLetterAgentID]; a != nil && a.Letter.Class != nil {
			out = append(out, c.t("sheet.reading.L7", map[string]string{"agent": a.Agent, "letter": *a.Letter.Class}))
		}
	}
	return strings.Join(out, " ")
}

// renderStrip is the RiskStrip (BV-236 §1): for each of the five kinds, the
// worst state among the evaluated agents and who causes it; « *» when it
// rests on something not readable at rest, with the note under the strip.
func (c htmlCtx) renderStrip() {
	ev := c.evaluated()
	if len(ev) == 0 {
		return
	}
	c.w(`<div class="syms">`)
	star := false
	for _, kind := range rules.KindOrder {
		worst, who, assumed := "", []string{}, false
		for _, a := range ev {
			for _, k := range a.Kinds {
				if k.Kind != kind {
					continue
				}
				switch {
				case worst == "" || stateRankSheet[k.State] > stateRankSheet[worst]:
					worst, who, assumed = k.State, []string{a.Agent}, k.Assumed
				case k.State == worst:
					who = append(who, a.Agent)
					assumed = assumed || k.Assumed
				}
			}
		}
		if worst == "" {
			worst = rules.StateUnknown
		}
		mark := ""
		if assumed {
			mark, star = " *", true
		}
		whoText := ""
		if worst != rules.StateUnknown && worst != rules.StateNoneKnown {
			if len(who) == 1 {
				whoText = html.EscapeString(who[0])
			} else {
				whoText = c.t("sheet.who.agents", map[string]string{"n": strconv.Itoa(len(who))})
			}
		}
		c.w(`<div class="sym" data-kind="%s" data-state="%s"><span class="lab">%s%s</span><span class="st %s">%s%s</span><span class="who">%s</span></div>`,
			kind, worst, kindSVG(kind), c.t("sheet.kind."+kind, nil), stateClass[worst], c.stateText(worst), mark, whoText)
	}
	c.w(`</div>`)
	if star {
		c.w(`<p class="starnote">%s</p>`, c.t("sheet.assumed.note", nil))
	}
}

// affects is «Affects» of a findings row: its projects for a key in git
// (one name, or «N projects»), else its agents (one name, or «N agents»).
func (c htmlCtx) affects(f rules.Finding) string {
	if rules.IsMachineFinding(f) {
		ps := c.projects(f)
		if len(ps) == 1 {
			return ps[0]
		}
		if len(ps) > 1 {
			return c.cat.Plural("sheet.scope.projects", len(ps), map[string]string{"n": strconv.Itoa(len(ps))})
		}
		return ""
	}
	if r := c.rows[f.ID]; r != nil && len(r.agents) > 1 {
		return c.cat.Text("sheet.who.agents", map[string]string{"n": strconv.Itoa(len(r.agents))})
	}
	if a, ok := agentOfFinding(c.v, f); ok {
		return a.Agent
	}
	return f.Params.Agent
}

// ifChange is «If you change it (agent's score)»: «{agent} would go up to
// {n}» from the agent's group without that finding (index groups), or «—».
func (c htmlCtx) ifChange(f rules.Finding) string {
	a, ok := agentOfFinding(c.v, f)
	if !ok || a.Value == nil {
		return `<span class="zero">—</span>`
	}
	best := -1
	for _, g := range a.Groups {
		if containsID(g.FindingIDs, f.ID) && g.Without > best {
			best = g.Without
		}
	}
	if best <= *a.Value {
		return `<span class="zero">—</span>`
	}
	return c.t("sheet.would", map[string]string{"agent": a.Agent, "n": strconv.Itoa(best)})
}

// The uses of «Computer profile» (BV-236 §2) and the profile each proposes
// for Claude Code.
var sheetUses = []struct{ id, profile string }{
	{"code", profiles.P1}, {"docs", profiles.P0}, {"mail", profiles.P0}, {"tasks", profiles.P3},
}

// claudeProfile is Claude Code's score and class with a measured profile.
func claudeProfile(a *index.AgentIndex, id string) (index.Profile, bool) {
	if a == nil {
		return index.Profile{}, false
	}
	for _, p := range a.Profiles {
		if p.Profile == id {
			return p, true
		}
	}
	return index.Profile{}, false
}

// claudeAgent is the evaluated Claude Code agent's score, nil without one.
func (c htmlCtx) claudeAgent() *index.AgentIndex {
	for _, o := range c.evaluated() {
		if o.Product == knowledge.ClaudeCodeProduct {
			return c.score[o.AgentID]
		}
	}
	return nil
}

// ncKey is a profile's «Doesn't cover»: P1's names Windows' missing sandbox
// only on Windows (P1b elsewhere, BV-236 §6).
func (c htmlCtx) ncKey(id string) string {
	if id == profiles.P1 && c.v.OS != "" && c.v.OS != "windows" {
		return "cc.P1b.nc"
	}
	return "cc." + id + ".nc"
}

// hide is the hidden attribute of a fragment that is not the default one:
// without JavaScript the default (use «I write code») is what shows.
func hide(on bool) string {
	if on {
		return ""
	}
	return " hidden"
}

// renderProfileViews are «Computer profile» (§2, w1) and Claude Code in
// detail (§3, w2): views of the same page, no network. Without JavaScript
// both show below the sheet with the use «I write code».
func (c htmlCtx) renderProfileViews() {
	cc := c.claudeAgent()
	m := c.sheetScore()
	before := index.NoClass()
	if m.Letter != nil {
		before = *m.Letter
	}
	gitKey := false
	for _, f := range c.v.Findings {
		if f.Rule == rules.RuleCredInGit {
			gitKey = true
		}
	}
	def := sheetUses[0]

	// §2 Computer profile.
	c.w(`<section class="view pcv" id="v-pc" aria-labelledby="pc-q"><div class="pc"><div class="pc-side">`)
	c.w(`<div><a class="back" href="#top" data-view="">%s</a></div><div><div class="k">%s</div><h2 id="pc-q">%s</h2></div><div class="q" role="radiogroup" aria-labelledby="pc-q">`,
		c.t("pc.back", nil), c.t("pc.kicker", nil), c.t("pc.q", nil))
	for _, u := range sheetUses {
		checked := ""
		if u.id == def.id {
			checked = " checked"
		}
		c.w(`<label><input type="radio" name="use" value="%s" data-profile="%s"%s><span class="radio" aria-hidden="true"></span>%s</label>`, u.id, u.profile, checked, c.t("pc.use."+u.id, nil))
	}
	c.w(`</div><div class="ba"><div class="k">%s</div>`, c.t("pc.computer", nil))
	for _, id := range []string{profiles.P0, profiles.P1, profiles.P3} {
		after, afterVal, agent := before, m.Value, m.AgentID
		measured := false
		for _, mp := range m.Profiles {
			if mp.Profile.Profile == id {
				v := mp.Value
				after, afterVal, agent, measured = mp.Letter, &v, mp.AgentID, true
			}
		}
		c.w(`<div data-p="%s"%s><div class="line">%s%s<span class="arrow">→</span>%s%s</div><p>`, id, hide(id == def.profile),
			cls(before, ""), ntOf(m.Value, m.Band), cls(after, ""), ntOf(afterVal, ""))
		switch {
		case id == profiles.P3:
			c.w(`%s`, c.t("pc.B2", nil))
		case measured:
			key := "pc.B1.noprofile"
			if cc != nil && agent == cc.AgentID {
				key = "pc.B1"
			}
			c.w(`%s`, c.t(key, map[string]string{"agent": c.agentName(agent)}))
		}
		if gitKey {
			c.w(` %s`, c.t("pc.B3", nil))
		}
		c.w(`</p></div>`)
	}
	c.w(`</div></div><div class="pc-main"><h3>%s</h3><div class="plist"><div class="prow hd" aria-hidden="true"><span>%s</span><span>%s</span><span>%s</span><span>%s</span><span></span></div>`,
		c.t("pc.suggest", nil), c.t("pc.col.agent", nil), c.t("pc.col.today", nil), c.t("pc.col.suggest", nil), c.t("pc.col.nc", nil))
	// Claude Code first; then Cursor and Grok Build, which have their own
	// row while they have a phrase (N2, N1); every other evaluated agent goes
	// in one row «The others» at the end (BV-236, Albert 15-11).
	var own, others []rules.OverviewAgent
	for _, o := range c.evaluated() {
		switch o.Product {
		case knowledge.ClaudeCodeProduct, knowledge.CursorProduct, knowledge.GrokBuildProduct:
			own = append(own, o)
		default:
			others = append(others, o)
		}
	}
	sort.SliceStable(own, func(i, j int) bool {
		return own[i].Product == knowledge.ClaudeCodeProduct && own[j].Product != knowledge.ClaudeCodeProduct
	})
	for _, o := range own {
		a := c.score[o.AgentID]
		var now index.Class
		var val *int
		band := ""
		if a != nil {
			now, val, band = a.Letter, a.Value, a.Band
		} else {
			now = index.NoClass()
		}
		c.w(`<div class="prow"><span class="ag">%s</span><span class="now">%s%s</span>`, html.EscapeString(o.Agent), cls(now, ""), ntOf(val, band))
		if o.Product == knowledge.ClaudeCodeProduct && a != nil {
			for _, id := range []string{profiles.P0, profiles.P1, profiles.P3} {
				c.w(`<div class="chg" data-p="%s"%s><span class="c1">%s`, id, hide(id == def.profile), c.t("cc.name."+id, nil))
				if p, ok := claudeProfile(a, id); ok {
					c.w(` · %s<span class="arrow">→</span>%s</span><span class="c2">%s</span></div>`, cls(now, ""), cls(p.Letter, ""),
						c.t("pc.score", map[string]string{"a": scoreText(val), "b": strconv.Itoa(p.Value)}))
				} else {
					c.w(`</span><span class="c2">%s</span></div>`, c.t("cc.nomeasure", nil))
				}
			}
			for _, id := range []string{profiles.P0, profiles.P1, profiles.P3} {
				c.w(`<div class="nc2" data-p="%s"%s>%s</div>`, id, hide(id == def.profile), c.t(c.ncKey(id), nil))
			}
			c.w(`<a class="chgl" href="#v-cc" data-view="v-cc">%s</a></div>`, c.t("pc.change", nil))
			continue
		}
		nc := c.t("pc.N2", nil)
		if o.Product == knowledge.GrokBuildProduct {
			nc = c.t("pc.N1", nil)
		}
		c.w(`<span class="soon">%s</span><div class="nc2">%s</div><span></span></div>`, c.t("pc.soon", nil), nc)
	}
	if len(others) > 0 {
		c.w(`<div class="prow oth"><span class="ag">%s</span><span class="now"></span><div class="olist"><div class="onames">`, c.t("pc.others", nil))
		for i, o := range others {
			if i > 0 {
				c.w(`<span class="sep"> · </span>`)
			}
			now, val, band := index.NoClass(), (*int)(nil), ""
			if a := c.score[o.AgentID]; a != nil {
				now, val, band = a.Letter, a.Value, a.Band
			}
			c.w(`<span class="oname">%s %s%s</span>`, html.EscapeString(o.Agent), cls(now, " sm"), ntOf(val, band))
		}
		c.w(`</div><span class="soon">%s</span></div></div>`, c.t("pc.soon", nil))
	}
	c.w(`</div>`)
	c.renderApply("pc")
	c.w(`</div></div></section>`)

	// §3 Claude Code in detail.
	if cc == nil {
		return
	}
	c.w(`<section class="view ccv" id="v-cc" aria-labelledby="cc-title"><div class="pf"><div class="pf-side">`)
	c.w(`<div><a class="back" href="#v-pc" data-view="v-pc">%s</a></div><div><div class="k">%s</div><h2 id="cc-title">%s</h2></div><p>%s</p><div class="nb">%s %s %s</div></div>`,
		c.t("cc.back", nil), html.EscapeString(cc.Agent), c.t("cc.title", nil), c.t("cc.lead", nil), c.t("cc.today", nil), cls(cc.Letter, ""), ntOf(cc.Value, cc.Band))
	c.w(`<div class="pf-main"><div class="uses"><span class="k">%s</span>`, c.t("cc.use", nil))
	for _, u := range sheetUses {
		c.w(`<span class="owner" data-use="%s"%s>%s</span>`, u.id, hide(u.id == def.id), c.t("cc.use.where", map[string]string{"use": c.cat.Text("pc.use."+u.id, nil)}))
	}
	c.w(`</div><div class="cards" role="radiogroup" aria-labelledby="cc-title">`)
	for _, id := range []string{profiles.P0, profiles.P1, profiles.P3} {
		checked := ""
		if id == def.profile {
			checked = " checked"
		}
		c.w(`<label class="card"><input type="radio" name="cprofile" value="%s"%s><div class="top"><span class="radio" aria-hidden="true"></span><h4>%s</h4></div><div class="chg">`, id, checked, c.t("cc.name."+id, nil))
		if p, ok := claudeProfile(cc, id); ok {
			c.w(`<span class="c1">%s %s<span class="arrow">→</span>%s</span><span class="c2">%s</span>`, c.t("pc.class", nil), cls(cc.Letter, ""), cls(p.Letter, ""),
				c.t("pc.score", map[string]string{"a": scoreText(cc.Value), "b": strconv.Itoa(p.Value)}))
		} else {
			c.w(`<span class="c2">%s</span>`, c.t("cc.nomeasure", nil))
		}
		c.w(`</div>`)
		for _, u := range sheetUses {
			if u.profile == id {
				c.w(`<span class="rectag" data-use="%s"%s>%s</span>`, u.id, hide(u.id == def.id), c.t("cc.rec", map[string]string{"use": c.cat.Text("cc.use.lc."+u.id, nil)}))
			}
		}
		c.renderRows(id)
		c.w(`</label>`)
	}
	c.w(`</div>`)
	c.renderApply("cc")
	c.w(`</div></div></section>`)
}

func scoreText(v *int) string {
	if v == nil {
		return "—"
	}
	return strconv.Itoa(*v)
}

// profileRows are the four literal rows of each profile (BV-236 §7), each
// line with its kind.
var profileRows = map[string][4][][2]string{
	profiles.P0: {
		{{rules.KindKeys, "cc.P0.does.1"}},
		{},
		{{rules.KindChangesFiles, "cc.P0.no.1"}, {rules.KindRunsCommands, "cc.P0.no.2"}, {rules.KindSendsOut, "cc.P0.no.3"}},
	},
	profiles.P1: {
		{{rules.KindChangesFiles, "cc.P1.does.1"}, {rules.KindRunsCommands, "cc.P1.does.2"}},
		{{rules.KindRunsCommands, "cc.P1.asks.1"}, {rules.KindChangesFiles, "cc.P1.asks.2"}},
		{{rules.KindKeys, "cc.P1.no.1"}, {rules.KindSendsOut, "cc.P1.no.2"}},
	},
	profiles.P3: {
		{{rules.KindRunsCommands, "cc.P3.does.1"}},
		{},
		{{rules.KindRunsCommands, "cc.P3.no.1"}, {rules.KindRunsCommands, "cc.P3.no.2"}, {rules.KindSendsOut, "cc.P3.no.3"}},
	},
}

// renderRows is a ProfileCard's four rows: «Does on its own», «Asks you»,
// «Doesn't do» and «Doesn't cover»; an empty row is «—».
func (c htmlCtx) renderRows(id string) {
	r := profileRows[id]
	c.w(`<dl class="rows">`)
	for i, head := range []string{"cc.row.does", "cc.row.asks", "cc.row.no"} {
		c.w(`<dt>%s</dt>`, c.t(head, nil))
		if len(r[i]) == 0 {
			c.w(`<dd class="empty">—</dd>`)
		}
		for _, l := range r[i] {
			c.w(`<dd>%s<span>%s</span></dd>`, kindSVG(l[0]), c.t(l[1], nil))
		}
	}
	c.w(`<div class="nc"><dt>%s</dt><dd>%s</dd></div></dl>`, c.t("cc.row.nc", nil), c.t(c.ncKey(id), nil))
}

// copyText is what a profile's button copies (BV-236 §3): T1 and the JSON
// block of CTO-50 §1 for P0 and P1, the literal command for P3.
func (c htmlCtx) copyText(id string) string {
	if id == profiles.P3 {
		return profiles.Block(id)
	}
	return c.cat.Text("cc.T1", nil) + "\n\n" + profiles.Block(id)
}

// renderApply is the apply bar and the lab test line of each profile
// (BV-236 §3), the default one visible; where is the view's prefix for ids.
func (c htmlCtx) renderApply(where string) {
	for _, id := range []string{profiles.P0, profiles.P1, profiles.P3} {
		pid := where + "-" + id
		c.w(`<div class="apply" data-p="%s"%s><div><div class="steps">`, id, hide(id == sheetUses[0].profile))
		steps, nb, see, cp := []string{"cc.S1", "cc.S2", "cc.S3"}, "cc.S4", "cc.see.text", "cc.copy.text"
		if id == profiles.P3 {
			steps, nb, see, cp = []string{"cc.S5", "cc.S6"}, "cc.S7", "cc.see.cmd", "cc.copy.cmd"
		}
		for i, s := range steps {
			c.w(`<span><i>%d</i>%s</span>`, i+1, c.t(s, nil))
		}
		c.w(`</div><div class="nb">%s</div></div><div class="bt"><button type="button" class="btn s" data-see="%s" aria-controls="%s" aria-expanded="false">%s</button><button type="button" class="btn p" data-copy="%s" data-done="%s" data-fail="%s">%s</button></div>`,
			boldLead(c.t(nb, nil), ". "), pid, pid, c.t(see, nil), pid, c.t("cc.copied", nil), c.t("copy.C1", nil), c.t(cp, nil))
		c.w(`<pre class="ptext" id="%s" tabindex="-1">%s</pre><span class="live" aria-live="polite"></span></div>`, pid, html.EscapeString(c.copyText(id)))
		c.w(`<p class="proof" data-p="%s"%s>%s %s</p>`, id, hide(id == sheetUses[0].profile),
			boldLead(c.t("cc.lab", map[string]string{"profile": c.cat.Text("cc.name."+id, nil)}), ": "), c.t("cc.R1", nil))
	}
}

// sheetTokens are the prototype's colour tokens the sheet and the profile
// views use (prototipo-v6, design system v1), prefixed so that today's
// tokens of the rest of the report do not change.
var sheetTokens = []cssToken{
	{"s-paper", "#FFFFFF", "#1C1B19"},
	{"s-line", "#E3E0D8", "#34322D"},
	{"s-ink2", "#5E5B55", "#AAA69D"},
	{"s-ink3", "#8A867E", "#7E7A72"},
	{"s-brand", "#2346B8", "#8FA9FF"},
	{"s-brand-ink", "#FFFFFF", "#0F1631"},
	{"s-brand-bg", "#EAEEFA", "#1D2440"},
	{"s-red", "#B3261E", "#F08A80"},
	{"s-red-bg", "#FBEBEA", "#3A1F1C"},
	{"s-amber", "#7A5200", "#F5C451"},
	{"s-amber-bg", "#FBF1DD", "#2A2310"},
	{"s-ok", "#2E7D4F", "#6FD6A3"},
	{"s-ok-bg", "#E7F3EC", "#12281D"},
}

// sheetCSS is the CSS of the bar, the sheet and the profile views, copied
// from prototipo-v6 (its values, sizes and order).
func sheetCSS() string {
	var light, dark strings.Builder
	for _, t := range sheetTokens {
		fmt.Fprintf(&light, "--%s:%s;", t.Name, t.Light)
		fmt.Fprintf(&dark, "--%s:%s;", t.Name, t.Dark)
	}
	return "\n:root{" + strings.TrimSuffix(light.String(), ";") + "}\n@media (prefers-color-scheme:dark){:root{" + strings.TrimSuffix(dark.String(), ";") + "}}\n" + sheetRules
}

const sheetRules = `a,a.lang,a.spacelink{color:var(--s-brand)}
body{padding:0}
.bar{height:76px;display:flex;align-items:center;gap:16px;padding:0 36px;border-bottom:1px solid var(--s-line);background:var(--s-paper)}
.bar .logo{height:46px;width:auto;display:block;margin-left:-10px}
.logo .wm{fill:var(--ink)} .logo .stem{stroke:var(--ink)}
.bar .sp{flex:1}
.bar a.lang{color:var(--s-brand);font-size:14px;text-decoration:none}
a.spacego{display:inline-flex;align-items:center;gap:10px;padding:9px 16px;font-size:14px;font-weight:650;border-radius:12px;background:var(--s-brand);color:var(--s-brand-ink);text-decoration:none;white-space:nowrap}
a.spacego i{width:8px;height:8px;border-radius:50%;background:#7CE0A8;box-shadow:0 0 0 3px color-mix(in srgb,#7CE0A8 35%,transparent)}
.sheetwrap{display:flex;justify-content:center;align-items:flex-start;padding:16px 32px}
.sheet{width:100%;max-width:1120px;background:var(--s-paper);border:1px solid var(--s-line);border-radius:6px;box-shadow:0 1px 0 var(--s-line),0 18px 40px -28px rgba(0,0,0,.45);padding:18px 36px 12px;display:flex;flex-direction:column;gap:10px;font-size:15px}
.sh-head{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:24px;align-items:end;border-bottom:2px solid var(--ink);padding-bottom:10px}
@media (max-width:860px){.sh-head{grid-template-columns:minmax(0,1fr)}}
.sh-title{font-size:28px;margin:4px 0 2px;letter-spacing:-.01em;line-height:1.2}
.sh-sub{color:var(--s-ink2);font-size:14px}
.sh-meta{display:grid;grid-template-columns:repeat(2,auto);gap:4px 22px;margin:0;font-size:12.5px}
.sh-meta div{display:flex;gap:8px}
.sh-meta dt{color:var(--s-ink3)} .sh-meta dd{margin:0;font-weight:600;font-variant-numeric:tabular-nums}
.sh-sec{display:flex;flex-direction:column;gap:10px}
.sh-h{margin:0;font-size:12px;letter-spacing:.1em;text-transform:uppercase;color:var(--ink);display:flex;align-items:center;gap:10px}
.sh-h span{width:20px;height:20px;border-radius:50%;background:var(--ink);color:var(--s-paper);display:grid;place-items:center;font-size:11px;letter-spacing:0}
.sh-h em{font-style:normal;text-transform:none;letter-spacing:0;color:var(--s-ink3);font-weight:500;font-size:13px}
.res{display:grid;grid-template-columns:auto minmax(0,1fr) minmax(0,1.6fr);gap:8px 32px;align-items:center;border:1px solid var(--s-line);border-radius:8px;padding:10px 18px}
@media (max-width:860px){.res{grid-template-columns:minmax(0,1fr)}}
.res-main{display:flex;align-items:center;gap:14px}
.res-num{display:flex;align-items:baseline;gap:4px;line-height:1;font-variant-numeric:tabular-nums}
.res-num small{font-size:16px;color:var(--s-ink3);font-weight:500}
.res-num .nt.big{font-size:40px;padding:2px 10px;border-radius:10px}
.res-lab{font-size:12.5px;color:var(--s-ink2);margin-top:2px}
.res-k{font-size:11px;letter-spacing:.08em;text-transform:uppercase;color:var(--s-ink3);font-weight:700;margin-bottom:2px}
.res-ref,.res-int{font-size:14px}
.cls{min-width:28px;height:28px;padding:0 6px;display:inline-grid;place-items:center;font:700 19px/1 system-ui,-apple-system,"Segoe UI",sans-serif;color:var(--ink);background:var(--s-paper);border:1.5px solid var(--ink);border-radius:8px}
.cls.prov{border-style:dashed}
.cls.none{color:var(--s-ink3);border-color:var(--s-line)}
.nt{font-weight:700;font-variant-numeric:tabular-nums;padding:1px 7px;border-radius:6px}
.nt.r{color:var(--s-red);background:var(--s-red-bg)} .nt.a{color:var(--s-amber);background:var(--s-amber-bg)} .nt.g{color:var(--s-ok);background:var(--s-ok-bg)} .nt.u{color:var(--s-ink3)}
.clsline{display:flex;align-items:center;gap:8px;font-size:13px;color:var(--s-ink2);margin-top:4px}
.clsline .prov{font-size:12px;color:var(--s-ink3)}
.ic{display:inline-grid;place-items:center;vertical-align:-3px;margin-right:6px;color:var(--ink)}
.ic svg{width:16px;height:16px}
.syms{grid-column:1/-1;display:grid;grid-template-columns:repeat(5,minmax(0,1fr));gap:8px;border-top:1px solid var(--s-line);padding-top:10px}
@media (max-width:1099px){.syms{grid-template-columns:repeat(2,minmax(0,1fr))}}
.sym{display:flex;flex-direction:column;gap:2px;min-width:0}
.sym .lab{display:flex;align-items:center;font-size:12.5px;font-weight:650;white-space:nowrap}
.sym .lab .ic svg{width:18px;height:18px}
.sym .st{font-size:12px;line-height:1.3;font-weight:650}
.sym .who{font-size:11.5px;color:var(--s-ink2);line-height:1.3}
.st.s{color:var(--s-red)} .st.m{color:var(--s-amber)} .st.l{color:var(--s-ink2)} .st.a{color:var(--s-ok)} .st.u,.st.nk{color:var(--s-ink3)}
.starnote{grid-column:1/-1;margin:0;font-size:11.5px;color:var(--s-ink2)}
.tbl{width:100%;border-collapse:collapse;font-size:14px}
.tbl th,.tbl td{overflow-wrap:normal}
.tbl th{white-space:nowrap;font-size:11px;letter-spacing:.08em;text-transform:uppercase;color:var(--s-ink3);text-align:left;font-weight:700;padding:6px 10px;border-bottom:1px solid var(--ink)}
.tbl td{padding:6px 10px;border-bottom:1px solid var(--s-line);vertical-align:middle}
.tbl td:first-child{font-variant-numeric:tabular-nums;color:var(--s-ink2);width:34px}
.tbl .n{text-align:right;font-variant-numeric:tabular-nums;white-space:nowrap}
.tbl .t{font-weight:600}
.tbl .af{color:var(--s-ink2);font-size:13px;white-space:nowrap}
.th-sub{text-transform:none;letter-spacing:0;font-weight:500}
.zero{color:var(--s-ink3)}
.pill{font-size:12px;font-weight:700;border-radius:999px;padding:2px 9px;white-space:nowrap}
.pill.r{background:var(--s-red-bg);color:var(--s-red)} .pill.a{background:var(--s-amber-bg);color:var(--s-amber)}
.sh-reco .rrow{display:flex;flex-wrap:wrap;gap:12px 24px;align-items:center;justify-content:space-between}
.sh-reco p{margin:0;font-size:14.5px;max-width:62ch;flex:1;min-width:280px}
.ways{display:grid;grid-template-columns:minmax(0,270px) minmax(0,190px);gap:12px}
@media (max-width:700px){.ways{grid-template-columns:minmax(0,1fr)}}
.way{display:flex;flex-direction:column;gap:4px;align-items:stretch}
.btn{font:inherit;font-weight:650;font-size:15px;border-radius:12px;padding:12px 20px;cursor:pointer;border:1px solid transparent;white-space:nowrap;display:inline-flex;align-items:center;justify-content:center;text-decoration:none;min-height:44px}
.way .btn{border:1.5px solid var(--s-brand);color:var(--s-brand);background:transparent;height:44px}
.way .btn:hover{background:var(--s-brand-bg)}
.way .btn.p,.way .btn.p:hover{background:var(--ink);color:var(--s-paper);border-color:var(--ink)}
.way small{font-size:12px;color:var(--s-ink2);text-align:center}
.sh-foot{display:flex;flex-wrap:wrap;justify-content:space-between;gap:6px 20px;border-top:1px solid var(--s-line);padding-top:10px;font-size:12px;color:var(--s-ink3)}
.sheetnotes{max-width:1120px;margin:0 auto 8px}
.k{font-size:12px;letter-spacing:.07em;text-transform:uppercase;color:var(--s-ink2);font-weight:650}
.back{color:var(--s-brand);font-size:14px;font-weight:650;cursor:pointer;white-space:nowrap;text-decoration:none}
.arrow{color:var(--s-ink3)}
.view{border-top:1px solid var(--s-line);background:var(--bg)}
.js .view{display:none}
.js.alt .view.on{display:block}
.js.alt .rep{display:none}
.pc{display:grid;grid-template-columns:minmax(0,320px) minmax(0,1fr);min-height:723px}
@media (max-width:1099px){.pc{grid-template-columns:minmax(0,1fr)}}
.pc-side{border-right:1px solid var(--s-line);background:var(--s-paper);padding:22px 26px;display:flex;flex-direction:column;gap:16px;min-width:0}
.pc-side h2{font-size:24px;line-height:1.2;margin:2px 0 0;text-wrap:balance}
.q{display:flex;flex-direction:column;gap:6px}
.q label{font-size:15px;text-align:left;display:flex;align-items:center;gap:10px;border:1px solid var(--s-line);background:var(--bg);color:var(--ink);border-radius:10px;padding:0 12px;height:44px;cursor:pointer}
.q input,.card input{position:absolute;opacity:0;width:1px;height:1px}
.radio{width:18px;height:18px;border-radius:50%;border:2px solid var(--s-ink3);flex:none;display:grid;place-items:center}
.q .radio{width:16px;height:16px}
.q label:has(input:checked){border:2px solid var(--s-brand);padding:0 11px;background:var(--s-brand-bg);font-weight:650}
.q label:has(input:focus-visible),.card:has(input:focus-visible){outline:3px solid var(--focus);outline-offset:2px}
.q input:checked+.radio,.card:has(input:checked) .radio{border-color:var(--s-brand)}
.q input:checked+.radio::after{content:"";width:7px;height:7px;border-radius:50%;background:var(--s-brand)}
.ba{border-top:1px solid var(--s-line);padding-top:14px;display:flex;flex-direction:column;gap:8px}
.ba .line{flex:none;height:auto;background:none;opacity:1;margin:0;display:flex;align-items:center;gap:8px}
.ba .line::after{content:none}
.ba p{margin:0;font-size:13px;color:var(--s-ink2)}
.pc-main{padding:20px 32px;display:flex;flex-direction:column;gap:14px;min-width:0}
.pc-main h3{margin:0;font-size:19px}
.plist{background:var(--s-paper);border:1px solid var(--s-line);border-radius:14px;overflow:hidden}
.prow{display:grid;grid-template-columns:130px 70px 230px minmax(0,1fr) 70px;gap:14px;align-items:center;padding:12px 16px;border-top:1px solid var(--s-line)}
.prow:first-child{border-top:0}
.prow.hd{background:var(--bg);padding:7px 16px;font-size:10.5px;letter-spacing:.09em;text-transform:uppercase;font-weight:700;color:var(--s-ink2)}
@media (max-width:900px){.prow{grid-template-columns:minmax(0,1fr) auto}.prow.hd{display:none}.prow .nc2{grid-column:1/-1}}
.prow .ag{font-weight:650;font-size:15px}
.prow .now{display:flex;align-items:center;gap:6px;font-size:12.5px;color:var(--s-ink2)}
.prow .nc2{font-size:12.5px;line-height:1.4;color:var(--ink);background:var(--bg);border-radius:8px;padding:6px 10px}
.prow .nc2.empty{background:none}
.prow .chgl{color:var(--s-brand);font-weight:650;font-size:13.5px;text-align:right;text-decoration:none}
.chg{display:flex;flex-direction:column;gap:2px}
.chg .c1{display:flex;align-items:center;gap:6px;font-size:13px;font-weight:650}
.chg .c2{font-size:12px;color:var(--s-ink2)}
.prow .chg .c1{color:var(--s-brand)}
.prow .chg .c2{font-weight:650;color:var(--ink)}
.apply .nb b,.proof b{color:var(--ink)}
.soon{color:var(--s-ink2);font-size:13px}
.prow.oth .olist{grid-column:3/-1;display:flex;flex-direction:column;gap:4px;min-width:0}
.onames{display:flex;flex-wrap:wrap;align-items:center;gap:4px 0;font-size:13.5px;font-weight:650}
.oname{display:inline-flex;align-items:center;gap:4px;white-space:nowrap}
.onames .sep{color:var(--s-ink3);font-weight:400;white-space:pre}
.cls.sm{min-width:22px;height:22px;font-size:14px;border-radius:6px;padding:0 4px;border-width:1.5px}
.onames .nt{font-size:12px;height:22px;display:inline-grid;place-items:center;padding:0 6px}
.apply{display:grid;grid-template-columns:minmax(0,1fr) auto;gap:10px 20px;align-items:center;background:var(--s-brand-bg);border:1px solid color-mix(in srgb,var(--s-brand) 35%,transparent);border-radius:14px;padding:12px 18px}
@media (max-width:760px){.apply{grid-template-columns:minmax(0,1fr)}}
.steps{display:flex;flex-wrap:wrap;gap:4px 18px;font-size:13.5px}
.steps span{display:inline-flex;gap:6px;align-items:baseline}
.steps i{font-style:normal;font-weight:700;color:var(--s-brand)}
.apply .nb{font-size:13px;color:var(--s-ink2);margin-top:4px}
.apply .bt{display:flex;gap:10px}
.apply button.btn{height:44px;font-size:15px}
.apply button.btn.s{background:transparent;border-color:var(--s-line);color:var(--ink)}
.apply button.btn.p{background:var(--ink);color:var(--s-paper)}
.ptext{grid-column:1/-1;margin:0;font-size:12.5px;line-height:1.5;white-space:pre-wrap;background:var(--s-paper);border-radius:8px;padding:10px 12px;max-height:240px;overflow:auto;user-select:text}
.js .ptext:not(.open){display:none}
.apply .live{grid-column:1/-1;margin:0;font-size:13px}
.apply .live:empty{display:none}
.proof{font-size:12.5px;color:var(--s-ink2);margin:0}
.pf{display:grid;grid-template-columns:minmax(0,270px) minmax(0,1fr);min-height:723px}
@media (max-width:1099px){.pf{grid-template-columns:minmax(0,1fr)}}
.pf-side{border-right:1px solid var(--s-line);background:var(--s-paper);padding:24px;display:flex;flex-direction:column;gap:16px;min-width:0}
.pf-side h2{font-size:23px;line-height:1.2;margin:2px 0 0;text-wrap:balance}
.pf-side p{margin:0;font-size:13.5px;color:var(--s-ink2)}
.pf-side .nb{display:flex;align-items:center;gap:6px;font-size:13px;color:var(--s-ink2)}
.pf-main{padding:20px 32px;display:flex;flex-direction:column;gap:14px;min-width:0}
.uses{display:flex;flex-wrap:wrap;align-items:center;gap:8px}
.uses .k{margin-right:4px}
.owner{font-size:13px;color:var(--s-ink2);white-space:nowrap}
.cards{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:14px}
@media (max-width:900px){.cards{grid-template-columns:minmax(0,1fr)}}
.card{background:var(--s-paper);border:1px solid var(--s-line);border-radius:14px;padding:12px 14px;display:flex;flex-direction:column;gap:8px;cursor:pointer;min-width:0;position:relative}
.card:has(input:checked){border:2px solid var(--s-brand);padding:11px 13px}
.card:has(input:checked) .radio::after{content:"";width:8px;height:8px;border-radius:50%;background:var(--s-brand)}
.card .top{display:flex;align-items:center;gap:10px}
.card h4{margin:0;font-size:16.5px;flex:1}
.rectag{align-self:flex-start;font-size:11.5px;font-weight:700;color:var(--s-brand);background:var(--s-brand-bg);border-radius:999px;padding:2px 9px;margin-top:-2px}
.rows{margin:0;display:flex;flex-direction:column;gap:2px}
.rows dt:not(:first-child){margin-top:6px}
.rows dt{font-size:10.5px;letter-spacing:.09em;text-transform:uppercase;font-weight:700;color:var(--ink)}
.rows dd{margin:0 0 2px;font-size:13px;line-height:1.4;color:var(--ink);display:flex;align-items:flex-start}
.rows dd .ic{margin-top:2px}
.rows dd .ic svg{width:14px;height:14px}
.rows dd.empty{color:var(--s-ink3)}
.rows .nc{background:var(--bg);border-radius:8px;padding:8px 10px;margin-top:2px}
[hidden]{display:none!important}
@media (max-width:1099px){.bar{padding:0 16px}.sheetwrap{padding:16px}.sheet{padding:18px 16px 12px}.res{grid-template-columns:minmax(0,1fr)}}`

// sheetJS opens and closes the views, follows the chosen use and profile,
// and shows or copies a profile's text (BV-236 §2–§3). When the browser
// refuses to copy, the text shows, selected, with C1 next to it. No
// network, no storage.
const sheetJS = `(function(){var de=document.documentElement;
function show(id){document.querySelectorAll('.view').forEach(function(v){v.classList.toggle('on',v.id===id)});de.classList.toggle('alt',!!id);window.scrollTo(0,0)}
document.addEventListener('click',function(e){var a=e.target.closest('[data-view]');if(!a)return;var id=a.getAttribute('data-view');if(!id){if(de.classList.contains('alt')){show('')}if(a.getAttribute('href')==='#top'){e.preventDefault()}return}e.preventDefault();show(id)});
var bar=document.querySelector('a.spacego');if(bar){bar.addEventListener('click',function(){show('')})}
var use='code',chosen=null;
function prof(){if(chosen)return chosen;var r=document.querySelector('input[name="use"][value="'+use+'"]');return r?r.getAttribute('data-profile'):'P1'}
function render(){var p=prof();document.querySelectorAll('.view [data-p]').forEach(function(el){el.hidden=el.getAttribute('data-p')!==p});document.querySelectorAll('.view [data-use]').forEach(function(el){el.hidden=el.getAttribute('data-use')!==use});document.querySelectorAll('input[name="cprofile"]').forEach(function(r){r.checked=r.value===p})}
document.querySelectorAll('input[name="use"]').forEach(function(r){r.addEventListener('change',function(){use=r.value;chosen=null;render()})});
document.querySelectorAll('input[name="cprofile"]').forEach(function(r){r.addEventListener('change',function(){chosen=r.value;render()})});
function selectText(p){var r=document.createRange();r.selectNodeContents(p);var s=window.getSelection();s.removeAllRanges();s.addRange(r)}
document.querySelectorAll('button[data-see]').forEach(function(b){b.addEventListener('click',function(){var p=document.getElementById(b.getAttribute('data-see'));var o=p.classList.toggle('open');b.setAttribute('aria-expanded',o?'true':'false');if(o){selectText(p)}})});
document.querySelectorAll('button[data-copy]').forEach(function(b){b.addEventListener('click',function(){var p=document.getElementById(b.getAttribute('data-copy'));var l=p.nextElementSibling;function fail(){p.classList.add('open');selectText(p);l.textContent=b.getAttribute('data-fail')}try{navigator.clipboard.writeText(p.textContent).then(function(){l.textContent=b.getAttribute('data-done');setTimeout(function(){l.textContent=''},1500)},fail)}catch(err){fail()}})});
render()})();`

// boldLead is s with its lead, up to and including the first sep, in bold
// (the prototype's emphasis); s unchanged without sep.
func boldLead(s, sep string) string {
	i := strings.Index(s, sep)
	if i < 0 {
		return s
	}
	n := i + len(strings.TrimRight(sep, " "))
	return "<b>" + s[:n] + "</b>" + s[n:]
}

// sheetKeys are the catalog codes of the sheet and the profile views
// (BV-236 §7).
func sheetKeys() []string {
	return []string{
		"sheet.title", "sheet.sub", "sheet.meta.date", "sheet.meta.computer", "sheet.meta.scope", "sheet.meta.program", "sheet.scope",
		"sheet.scope.agents.one", "sheet.scope.agents.other", "sheet.scope.projects.one", "sheet.scope.projects.other", "sheet.result",
		"sheet.result.label", "sheet.provisional", "sheet.class.A", "sheet.class.B", "sheet.class.C", "sheet.class.D", "sheet.reference",
		"sheet.reference.value", "sheet.reading", "sheet.reading.L1", "sheet.reading.L2.one", "sheet.reading.L2.other", "sheet.reading.L3.one",
		"sheet.reading.L3.other", "sheet.reading.L4", "sheet.reading.L5", "sheet.reading.L6", "sheet.reading.L7", "sheet.kind.money",
		"sheet.kind.sends_out", "sheet.kind.keys", "sheet.kind.runs_commands", "sheet.kind.changes_files", "sheet.state.bounded",
		"sheet.state.none_known", "sheet.who.agents", "sheet.assumed.note", "sheet.findings", "sheet.findings.count.first",
		"sheet.findings.count", "sheet.col.no", "sheet.col.finding", "sheet.col.affects", "sheet.col.level", "sheet.col.change",
		"sheet.col.change.sub", "sheet.would", "sheet.act", "sheet.act.A1", "sheet.act.profile", "sheet.act.profile.sub", "sheet.act.findings",
		"sheet.act.findings.sub", "sheet.foot.P1", "sheet.foot.P2", "sheet.foot.P2.final", "pc.back", "pc.kicker", "pc.q", "pc.use.code",
		"pc.use.docs", "pc.use.mail", "pc.use.tasks", "pc.computer", "pc.B1", "pc.B1.noprofile", "pc.B2", "pc.B3", "pc.suggest", "pc.col.agent",
		"pc.col.today", "pc.col.suggest", "pc.col.nc", "pc.change", "pc.soon", "pc.N1", "pc.N2", "pc.score", "pc.class", "cc.title", "cc.lead",
		"cc.today", "cc.use", "cc.use.where", "cc.name.P0", "cc.name.P1", "cc.name.P3", "cc.rec", "cc.use.lc.code", "cc.use.lc.docs",
		"cc.use.lc.mail", "cc.use.lc.tasks", "cc.row.does", "cc.row.asks", "cc.row.no", "cc.row.nc", "cc.P0.does.1", "cc.P0.no.1", "cc.P0.no.2",
		"cc.P0.no.3", "cc.P0.nc", "cc.P1.does.1", "cc.P1.does.2", "cc.P1.asks.1", "cc.P1.asks.2", "cc.P1.no.1", "cc.P1.no.2", "cc.P1.nc",
		"cc.P1b.nc", "cc.P3.does.1", "cc.P3.no.1", "cc.P3.no.2", "cc.P3.no.3", "cc.P3.nc", "cc.nomeasure", "cc.S1", "cc.S2", "cc.S3", "cc.S4",
		"cc.see.text", "cc.copy.text", "cc.S5", "cc.S6", "cc.S7", "cc.see.cmd", "cc.copy.cmd", "cc.T1", "copy.C1", "cc.copied", "cc.lab",
		"cc.R1", "report.accepted.title", "report.accepted.on", "cc.back", "sheet.findings.count.one", "pc.others",
	}
}

// sheetScore is the score the sheet shows: the machine's (index.machine);
// when the machine has none because an agent present was not evaluated
// (I5) but another one was, the lowest evaluated agent's, with its class and
// factory score, as the old step 1 showed it (v3 §3 point 6).
func (c htmlCtx) sheetScore() index.MachineScore {
	m := c.v.Index.Machine
	if m.Value != nil {
		return m
	}
	a, ok := c.scored()
	if !ok {
		return m
	}
	out := index.MachineScore{Score: index.Score{Value: a.Value, Band: a.Band}, AgentID: a.AgentID}
	l := a.Letter
	out.Letter = &l
	out.FactoryValue = a.Factory.Value
	return out
}
