package report

import (
	"encoding/json"
	"fmt"
	"html"
	"strconv"

	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/profiles"
	"github.com/qtorb/rosmarops/internal/rules"
)

// The guided profiles (BV-238, CTO-50 §2–§3, CTO-46 §2): Cursor, Grok Build
// and Grok Bot. A guided profile gives the exact steps and, where there is
// one, a text to paste, but no class or score before and after: the engine
// does not measure it. Nothing is read, written or evaluated for them.

// guidedProfile is one guided profile: its name, its steps, its four rows
// and what it copies.
type guidedProfile struct {
	id, name string         // id as the state («P0», «cloud»…), name's catalog key
	steps    []string       // catalog keys, in order
	rows     [3][][2]string // does on its own, asks you, doesn't do: {kind, key}
	nc       string         // «Doesn't cover»
	sentence string         // the sentence before the block (T2, T3); empty for a command
	cmd      bool           // copies a launch command (P3)
}

// guidedAgent is an agent with guided profiles, and what each use proposes
// for it: a profile id, or the catalog key of the sentence that says there
// is none.
type guidedAgent struct {
	key, product string // profiles.Guided*, the knowledge product ("" for Grok Bot)
	side         string // the side text's key
	g3           string // the cards' «can't measure» sentence's key
	docsRead     [3]int // year, month, day the agent's documentation was read (G4, BV-239)
	uses         map[string]string
	profiles     []guidedProfile
}

const noProfile = "!" // a use's value starting with it is a sentence key

var guidedAgents = []guidedAgent{
	{
		key: profiles.GuidedCursor, product: knowledge.CursorProduct, side: "gp.side", g3: "gp.G3", docsRead: [3]int{2026, 11, 15},
		uses: map[string]string{"code": profiles.P1, "docs": noProfile + "gp.cursor.use.none", "mail": noProfile + "gp.cursor.use.none", "tasks": noProfile + "gp.cursor.use.tasks"},
		profiles: []guidedProfile{
			{
				id: profiles.P0, name: "cc.name.P0",
				steps: []string{"gp.cursor.P0.step.1", "gp.cursor.step.read", "gp.cursor.P0.step.3", "gp.cursor.step.paste"},
				rows: [3][][2]string{
					{{rules.KindKeys, "gp.cursor.P0.does.1"}},
					{{rules.KindRunsCommands, "gp.cursor.P0.asks.1"}},
					{{rules.KindChangesFiles, "gp.cursor.P0.no.1"}, {rules.KindKeys, "gp.cursor.no.outside"}},
				},
				nc: "gp.cursor.P0.nc", sentence: "gp.cursor.T2",
			},
			{
				id: profiles.P1, name: "cc.name.P1",
				steps: []string{"gp.cursor.P1.step.1", "gp.cursor.step.read", "gp.cursor.P1.step.3", "gp.cursor.step.paste"},
				rows: [3][][2]string{
					{{rules.KindChangesFiles, "gp.cursor.P1.does.1"}, {rules.KindRunsCommands, "gp.cursor.P1.does.2"}},
					{{rules.KindRunsCommands, "gp.cursor.P1.asks.1"}, {rules.KindChangesFiles, "gp.cursor.P1.asks.2"}},
					{{rules.KindKeys, "gp.cursor.no.outside"}},
				},
				nc: "gp.cursor.P1.nc", sentence: "gp.cursor.T2",
			},
		},
	},
	{
		key: profiles.GuidedGrok, product: knowledge.GrokBuildProduct, side: "gp.side", g3: "gp.G3", docsRead: [3]int{2026, 11, 15},
		uses: map[string]string{"code": profiles.P1, "docs": noProfile + "gp.grok.use.none", "mail": noProfile + "gp.grok.use.none", "tasks": profiles.P3},
		profiles: []guidedProfile{
			{
				id: profiles.P0, name: "cc.name.P0", steps: []string{"gp.grok.step.paste"},
				rows: [3][][2]string{
					{{rules.KindKeys, "gp.grok.P0.does.1"}},
					{{rules.KindRunsCommands, "gp.grok.P0.asks.1"}},
					{{rules.KindChangesFiles, "gp.grok.P0.no.1"}, {rules.KindRunsCommands, "gp.grok.P0.no.2"}, {rules.KindSendsOut, "gp.grok.P0.no.3"}, {rules.KindRunsCommands, "gp.grok.no.hooks"}},
				},
				nc: "gp.grok.P0.nc", sentence: "gp.grok.T3",
			},
			{
				id: profiles.P1, name: "cc.name.P1", steps: []string{"gp.grok.step.paste"},
				rows: [3][][2]string{
					{{rules.KindRunsCommands, "gp.grok.P1.does.1"}},
					{{rules.KindChangesFiles, "gp.grok.P1.asks.1"}, {rules.KindRunsCommands, "gp.grok.P1.asks.2"}},
					{{rules.KindSendsOut, "gp.grok.no.web"}, {rules.KindRunsCommands, "gp.grok.no.hooks"}},
				},
				nc: "gp.grok.P1.nc", sentence: "gp.grok.T3",
			},
			{
				id: profiles.P3, name: "cc.name.P3", steps: []string{"gp.grok.P3.step.1", "gp.grok.P3.step.2"},
				rows: [3][][2]string{
					{{rules.KindRunsCommands, "gp.grok.P3.does.1"}},
					{},
					{{rules.KindRunsCommands, "gp.grok.P3.no.1"}, {rules.KindSendsOut, "gp.grok.no.web"}},
				},
				nc: "gp.grok.P3.nc", cmd: true,
			},
		},
	},
	{
		key: profiles.GuidedBot, product: knowledge.GrokBotProduct, side: "gp.bot.side", g3: "gp.bot.G3", docsRead: [3]int{2026, 11, 14},
		uses: map[string]string{"code": "cloud", "docs": "cloud", "mail": "cloud", "tasks": noProfile + "gp.bot.use.tasks"},
		profiles: []guidedProfile{
			{
				id: "cloud", name: "gp.bot.name.cloud", steps: []string{"gp.bot.cloud.step.1", "gp.bot.step.review", "gp.bot.step.ask"},
				rows: [3][][2]string{
					{{rules.KindSendsOut, "gp.bot.cloud.does.1"}},
					{{rules.KindMoney, "gp.bot.cloud.asks.1"}},
					{{rules.KindRunsCommands, "gp.bot.cloud.no.1"}},
				},
				nc: "gp.bot.cloud.nc",
			},
			{
				id: "local", name: "gp.bot.name.local", steps: []string{"gp.bot.local.step.1", "gp.bot.step.review", "gp.bot.step.ask"},
				rows: [3][][2]string{
					{{rules.KindSendsOut, "gp.bot.local.does.1"}},
					{{rules.KindRunsCommands, "gp.bot.local.asks.1"}},
					{},
				},
				nc: "gp.bot.local.nc",
			},
		},
	},
}

// grokBotName is Grok Bot's name: RosmarOps does not detect it, so it is not
// a graph name.
const grokBotName = "Grok Bot"

// state is what a use shows for the agent: a profile id, or «x-<use>» for a
// use with no profile.
func (g guidedAgent) state(use string) string {
	v := g.uses[use]
	if len(v) > 0 && v[:1] == noProfile {
		return "x-" + use
	}
	return v
}

func (g guidedAgent) profile(id string) (guidedProfile, bool) {
	for _, p := range g.profiles {
		if p.id == id {
			return p, true
		}
	}
	return guidedProfile{}, false
}

// states are every state of the agent, profiles first, each once.
func (g guidedAgent) states() []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range g.profiles {
		out, seen[p.id] = append(out, p.id), true
	}
	for _, u := range sheetUses {
		if s := g.state(u.id); !seen[s] {
			out, seen[s] = append(out, s), true
		}
	}
	return out
}

// guidedPresent are the guided agents shown: Cursor and Grok Build when
// evaluated, by their overview agent, and Grok Bot always.
func (c htmlCtx) guidedPresent() []struct {
	g    guidedAgent
	name string
	o    *rules.OverviewAgent
} {
	var out []struct {
		g    guidedAgent
		name string
		o    *rules.OverviewAgent
	}
	ev := c.evaluated()
	for _, g := range guidedAgents {
		bot := g.key == profiles.GuidedBot
		if bot && !c.hasEvaluated(g.product) {
			if c.v.GrokBotNoRow { // BV-248: B0 answered or Grok Bot detected
				continue
			}
			out = append(out, struct {
				g    guidedAgent
				name string
				o    *rules.OverviewAgent
			}{g, grokBotName, nil})
			continue
		}
		for i := range ev {
			if ev[i].Product == g.product {
				o := ev[i]
				out = append(out, struct {
					g    guidedAgent
					name string
					o    *rules.OverviewAgent
				}{g, o.Agent, &o})
				break
			}
		}
	}
	return out
}

// hasEvaluated reports an evaluated agent of product.
func (c htmlCtx) hasEvaluated(product string) bool {
	for _, o := range c.evaluated() {
		if o.Product == product {
			return true
		}
	}
	return false
}

// guidedMeasured is an agent's score and class with a profile measured on its
// declared settings (BV-248 decision 9), when there is one.
func (c htmlCtx) guidedMeasured(o *rules.OverviewAgent, id string) (index.Profile, bool) {
	if o == nil {
		return index.Profile{}, false
	}
	return claudeProfile(c.score[o.AgentID], id)
}

// guidedG3Rows are the rows of a measured profile the engine still does not
// measure (BV-248 decision 9): Cursor's Ask mode (P0) and «Delete files» (P1).
var guidedG3Rows = map[string]bool{"gp.cursor.P0.no.1": true, "gp.cursor.P1.asks.2": true}

// guidedProposes reports whether a use proposes a guided profile for some
// guided agent shown (G2 under «Your computer»).
func (c htmlCtx) guidedProposes(use string) bool {
	for _, x := range c.guidedPresent() {
		if _, ok := x.g.profile(x.g.state(use)); ok {
			return true
		}
	}
	return false
}

// renderGuidedRow is a guided agent's row of «Computer profile» (BV-238 §1):
// the proposed profile with G1, its «Doesn't cover» and «See the steps»; or,
// for a use with no profile, its sentence and no link. Grok Bot's row asks
// whether you use it and shows nothing under «Today».
func (c htmlCtx) renderGuidedRow(g guidedAgent, name string, o *rules.OverviewAgent) {
	def := g.state(sheetUses[0].id)
	if o == nil {
		c.w(`<div class="prow gbot" data-ga="%s"><span class="ag">%s</span><span class="now"></span>`, g.key, c.t("gp.bot.q", nil))
	} else {
		now, val, band := c.agentNow(o.AgentID)
		c.w(`<div class="prow" data-ga="%s"><span class="ag">%s</span><span class="now">%s%s</span>`, g.key, html.EscapeString(name), cls(now, ""), ntOf(val, band))
	}
	c.w(`<div class="gsug">`)
	for _, s := range g.states() {
		if p, ok := g.profile(s); ok {
			if mp, ok := c.guidedMeasured(o, p.id); ok { // BV-248: measured
				now, val, _ := c.agentNow(o.AgentID)
				c.w(`<div class="chg" data-ga="%s" data-gs="%s"%s><span class="c1">%s · %s<span class="arrow">→</span>%s</span><span class="c2">%s</span></div>`,
					g.key, s, hide(s == def), c.t(p.name, nil), cls(now, ""), cls(mp.Letter, ""),
					c.t("pc.score", map[string]string{"a": scoreText(val), "b": strconv.Itoa(mp.Value)}))
				continue
			}
			c.w(`<div class="chg" data-ga="%s" data-gs="%s"%s><span class="c1">%s <span class="gtag">%s</span></span></div>`, g.key, s, hide(s == def), c.t(p.name, nil), c.t("gp.G1", nil))
			continue
		}
		u := s[len("x-"):]
		c.w(`<span class="soon" data-ga="%s" data-gs="%s"%s>%s</span>`, g.key, s, hide(s == def), c.t(g.uses[u][len(noProfile):], nil))
	}
	c.w(`</div><div class="gnc">`)
	if o != nil {
		for _, p := range g.profiles {
			c.w(`<div class="nc2" data-ga="%s" data-gs="%s"%s>%s</div>`, g.key, p.id, hide(p.id == def), c.t(p.nc, nil))
		}
	}
	c.w(`</div><span class="glink">`)
	for _, p := range g.profiles {
		c.w(`<a class="chgl" href="#v-%s" data-view="v-%s" data-ga="%s" data-gs="%s"%s>%s</a>`, g.key, g.key, g.key, p.id, hide(p.id == def), c.t("gp.see_steps", nil))
	}
	c.w(`</span></div>`)
}

// agentNow is an agent's class and score today.
func (c htmlCtx) agentNow(id string) (cl index.Class, val *int, band string) {
	cl = index.NoClass()
	if a := c.score[id]; a != nil {
		cl, val, band = a.Letter, a.Value, a.Band
	}
	return cl, val, band
}

// renderGuidedDetail is a guided agent's detail view (BV-238 §2): like
// Claude Code's, with G3 in place of class and score, the numbered steps,
// the copy buttons when there is a text, and the lab line with G4.
func (c htmlCtx) renderGuidedDetail(g guidedAgent, name string, o *rules.OverviewAgent) {
	def := g.state(sheetUses[0].id)
	m := map[string]string{}
	for _, u := range sheetUses {
		m[u.id] = g.state(u.id)
	}
	js, _ := json.Marshal(m)
	ag := map[string]string{"agent": name}
	g4 := map[string]string{"agent": name, "date": c.docsDate(g)}
	c.w(`<section class="view gv" id="v-%s" data-ga="%s" data-gmap="%s" aria-labelledby="gt-%s"><div class="pf"><div class="pf-side">`, g.key, g.key, html.EscapeString(string(js)), g.key)
	c.w(`<div><a class="back" href="#v-pc" data-view="v-pc">%s</a></div><div><div class="k">%s</div><h2 id="gt-%s">%s</h2></div><p>%s</p>`,
		c.t("cc.back", nil), html.EscapeString(name), g.key, c.t("gp.title", ag), c.t(g.side, ag))
	if o != nil {
		now, val, band := c.agentNow(o.AgentID)
		c.w(`<div class="nb">%s %s %s</div>`, c.t("cc.today", nil), cls(now, ""), ntOf(val, band))
	}
	c.w(`</div><div class="pf-main"><div class="uses"><span class="k">%s</span>`, c.t("cc.use", nil))
	for _, u := range sheetUses {
		c.w(`<span class="owner" data-use="%s"%s>%s</span>`, u.id, hide(u.id == sheetUses[0].id), c.t("cc.use.where", map[string]string{"use": c.cat.Text("pc.use."+u.id, nil)}))
	}
	two := ""
	if len(g.profiles) == 2 {
		two = " two"
	}
	c.w(`</div><div class="cards%s" role="radiogroup" aria-labelledby="gt-%s">`, two, g.key)
	for _, p := range g.profiles {
		checked := ""
		if p.id == def {
			checked = " checked"
		}
		c.w(`<label class="card"><input type="radio" name="gp-%s" value="%s"%s><div class="top"><span class="radio" aria-hidden="true"></span><h4>%s</h4></div>`,
			g.key, p.id, checked, c.t(p.name, nil))
		mp, measured := c.guidedMeasured(o, p.id)
		if measured { // BV-248: measured on the declared settings
			now, val, _ := c.agentNow(o.AgentID)
			c.w(`<div class="chg"><span class="c1">%s %s<span class="arrow">→</span>%s</span><span class="c2">%s</span></div>`, c.t("pc.class", nil), cls(now, ""), cls(mp.Letter, ""),
				c.t("pc.score", map[string]string{"a": scoreText(val), "b": strconv.Itoa(mp.Value)}))
		} else {
			c.w(`<p class="g3">%s</p>`, c.t(g.g3, nil))
		}
		for _, u := range sheetUses {
			if g.uses[u.id] == p.id {
				c.w(`<span class="rectag" data-use="%s"%s>%s</span>`, u.id, hide(u.id == sheetUses[0].id), c.t("cc.rec", map[string]string{"use": c.cat.Text("cc.use.lc."+u.id, nil)}))
			}
		}
		c.renderRowsOf(p.rows, p.nc, measured, g.g3)
		c.w(`</label>`)
	}
	c.w(`</div>`)
	for _, p := range g.profiles {
		pid := "gv-" + g.key + "-" + p.id
		c.w(`<div class="apply gapply" data-ga="%s" data-gs="%s"%s><div><ol class="gsteps">`, g.key, p.id, hide(p.id == def))
		for _, s := range p.steps {
			c.w(`<li>%s</li>`, c.t(s, nil))
		}
		c.w(`</ol></div>`)
		if text := c.guidedText(g, p); text != "" {
			see, cp := "cc.see.text", "cc.copy.text"
			if p.cmd {
				see, cp = "cc.see.cmd", "cc.copy.cmd"
			}
			c.w(`<div class="bt"><button type="button" class="btn s" data-see="%s" aria-controls="%s" aria-expanded="false">%s</button><button type="button" class="btn p" data-copy="%s" data-done="%s" data-fail="%s">%s</button></div>`,
				pid, pid, c.t(see, nil), pid, c.t("cc.copied", nil), c.t("copy.C1", nil), c.t(cp, nil))
			c.w(`<pre class="ptext" id="%s" tabindex="-1">%s</pre><span class="live" aria-live="polite"></span>`, pid, html.EscapeString(text))
		}
		c.w(`</div>`)
		c.w(`<p class="proof" data-ga="%s" data-gs="%s"%s>%s %s</p>`, g.key, p.id, hide(p.id == def),
			boldLead(c.t("cc.lab", map[string]string{"profile": c.cat.Text(p.name, nil)}), ": "), c.t("gp.G4", g4))
	}
	c.w(`</div></div></section>`)
}

// docsDate is the day the agent's documentation was read, as G4 writes it:
// 15-11-2026 in Spanish, 2026-11-15 otherwise.
func (c htmlCtx) docsDate(g guidedAgent) string {
	y, m, d := g.docsRead[0], g.docsRead[1], g.docsRead[2]
	if c.cat.Lang() == "es" {
		return fmt.Sprintf("%02d-%02d-%04d", d, m, y)
	}
	return fmt.Sprintf("%04d-%02d-%02d", y, m, d)
}

// guidedText is what a guided profile copies: the sentence and its block
// (Cursor's JSON, Grok Build's config.toml), the command (P3), or nothing.
func (c htmlCtx) guidedText(g guidedAgent, p guidedProfile) string {
	block := profiles.GuidedBlock(g.key, p.id)
	if block == "" {
		return ""
	}
	if p.cmd {
		return block
	}
	return c.cat.Text(p.sentence, nil) + "\n\n" + block
}

// renderRowsOf is a ProfileCard's four rows; in a measured profile, a row the
// engine does not measure keeps the g3 sentence.
func (c htmlCtx) renderRowsOf(r [3][][2]string, nc string, measured bool, g3 string) {
	c.w(`<dl class="rows">`)
	for i, head := range []string{"cc.row.does", "cc.row.asks", "cc.row.no"} {
		c.w(`<dt>%s</dt>`, c.t(head, nil))
		if len(r[i]) == 0 {
			c.w(`<dd class="empty">—</dd>`)
		}
		for _, l := range r[i] {
			if measured && guidedG3Rows[l[1]] {
				c.w(`<dd>%s<span>%s <span class="g3">%s</span></span></dd>`, kindSVG(l[0]), c.t(l[1], nil), c.t(g3, nil))
				continue
			}
			c.w(`<dd>%s<span>%s</span></dd>`, kindSVG(l[0]), c.t(l[1], nil))
		}
	}
	c.w(`<div class="nc"><dt>%s</dt><dd>%s</dd></div></dl>`, c.t("cc.row.nc", nil), c.t(nc, nil))
}

// guidedKeys are the catalog codes of the guided profiles (BV-238 §4).
func guidedKeys() []string {
	keys := []string{"gp.G1", "gp.G2", "gp.G3", "gp.bot.G3", "gp.G4", "gp.see_steps", "gp.bot.q", "gp.title", "gp.side", "gp.bot.side"}
	seen := map[string]bool{}
	for _, k := range keys {
		seen[k] = true
	}
	add := func(k string) {
		if k != "" && !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for _, g := range guidedAgents {
		for _, v := range g.uses {
			if len(v) > 0 && v[:1] == noProfile {
				add(v[len(noProfile):])
			}
		}
		for _, p := range g.profiles {
			if p.name[:3] == "gp." {
				add(p.name)
			}
			for _, s := range p.steps {
				add(s)
			}
			for _, r := range p.rows {
				for _, l := range r {
					add(l[1])
				}
			}
			add(p.nc)
			add(p.sentence)
		}
	}
	return keys
}
