package report

import (
	"html"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/untrusted"
)

// «Your orchestrators» (0.19, signed design of 23-11): a card that explains
// how the orchestrators on this computer start the other agents. It asks for
// no action, so it carries the «To understand» treatment. Everything in it
// comes from the graph's launches edges, the per-agent score the report
// already computes and the orchestrator_skips_approvals findings: no rule,
// weight or schema changes, and nothing of it goes into the JSON.

// fleetRow is one launches edge of the graph, as the card shows it.
type fleetRow struct {
	edgeID      string
	orchID      string
	orch        string // the orchestrator's display name
	agentID     string
	agent       string // the started agent's display name
	override    string // approval_override
	certainty   string
	file        string // the base name of an observed edge's source file
	env         bool   // env_passthrough
	assumptions []string
}

// fleetOf are the graph's launches edges from an orchestrator, sorted by
// orchestrator name and then by agent name.
func fleetOf(g graph.Graph) []fleetRow {
	nodes := map[string]graph.Node{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	var out []fleetRow
	for _, e := range g.Edges {
		if e.Type != graph.EdgeLaunches {
			continue
		}
		orch, ok := nodes[e.From]
		if !ok || orch.Kind != graph.KindAgent || orch.Type != graph.AgentOrchestrator {
			continue
		}
		agent := nodes[e.To]
		r := fleetRow{
			edgeID: e.ID, orchID: orch.ID, orch: orch.Name, agentID: agent.ID, agent: agent.Name,
			override: e.ApprovalOverride, certainty: e.Certainty, env: e.EnvPassthrough,
			assumptions: append([]string{}, e.Assumptions...),
		}
		// Only the file's name, never its path; the knowledge base is no file.
		if e.Certainty == graph.CertaintyObserved && e.Source != "" && !strings.HasPrefix(e.Source, "kb:") {
			r.file = path.Base(strings.ReplaceAll(e.Source, `\`, "/"))
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].orch != out[j].orch {
			return out[i].orch < out[j].orch
		}
		if out[i].orchID != out[j].orchID {
			return out[i].orchID < out[j].orchID
		}
		return out[i].agent < out[j].agent
	})
	return out
}

// undEyebrow is the «To understand» line above a card's title, with its icon
// (three joined boxes, stroke 2).
func (c htmlCtx) undEyebrow() string {
	return `<p class="r3-und-eb"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="9" y="2" width="6" height="6" rx="1"/><rect x="2" y="16" width="6" height="6" rx="1"/><rect x="16" y="16" width="6" height="6" rx="1"/><path d="M12 8v4M5 16v-2h14v2"/></svg>` +
		c.t("html.und.eyebrow", nil) + `</p>`
}

// keyTag is the «Key» tag of a problem whose resource is a credential.
func (c htmlCtx) keyTag() string {
	return `<span class="r3-tag"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><circle cx="7.5" cy="15.5" r="5.5"/><path d="M11.5 11.5 21 2M17 6l3 3M14 9l2 2"/></svg>` +
		c.t("html.tag.key", nil) + `</span>`
}

// fleetWarnings are, per launches edge, the number of the problem that holds
// its orchestrator_skips_approvals finding. A dismissed finding is not among
// the findings and an accepted one is not in any problem, so neither has one.
func (c htmlCtx) fleetWarnings(iss []issue) map[string]int {
	out := map[string]int{}
	for i, is := range iss {
		for _, f := range is.findings {
			if f.Rule != rules.RuleOrchestratorSkipsApprovals {
				continue
			}
			for _, e := range f.Evidence.Edges {
				if _, ok := out[e]; !ok {
					out[e] = i + 1
				}
			}
		}
	}
	return out
}

// renderFleet is the «Your orchestrators» card, only when the graph has an
// orchestrator with at least one launches edge.
func (c htmlCtx) renderFleet(iss []issue) {
	rows := c.v.fleet
	if len(rows) == 0 {
		return
	}
	var groups [][]fleetRow
	for i, r := range rows {
		if i == 0 || r.orchID != rows[i-1].orchID {
			groups = append(groups, nil)
		}
		groups[len(groups)-1] = append(groups[len(groups)-1], r)
	}
	// The header counts distinct agents, not rows: Claude Code started by
	// three orchestrators is one agent (advisor, 23-11).
	agents := map[string]bool{}
	for _, r := range rows {
		agents[r.agentID] = true
	}
	counts := c.t("html.fleet.counts", map[string]string{
		"orchestrators": c.cat.Plural("html.fleet.counts.orchestrators", len(groups), map[string]string{"count": strconv.Itoa(len(groups))}),
		"agents":        c.cat.Plural("html.fleet.counts.agents", len(agents), map[string]string{"count": strconv.Itoa(len(agents))}),
	})
	c.w(`<section class="r3-card r3-und r3-fl" aria-labelledby="r3-fl-h">%s<div class="r3-top"><h2 class="r3-h" id="r3-fl-h">%s</h2><p class="r3-counts">%s</p></div><p class="r3-fl-intro">%s</p>`,
		c.undEyebrow(), c.t("html.fleet.title", nil), counts, c.t("html.fleet.intro", nil))
	warn := c.fleetWarnings(iss)
	cols := []string{c.t("html.fleet.col.agent", nil), c.t("html.fleet.col.asks", nil), c.t("html.fleet.col.source", nil), c.t("html.fleet.col.score", nil)}
	for _, g := range groups {
		orch := untrusted.PlainText(g[0].orch)
		c.w(`<div class="r3-fl-o"><div class="r3-fl-oh"><h3>%s<span>%s</span></h3></div>`,
			html.EscapeString(orch), html.EscapeString(c.cat.Plural("html.fleet.starts", len(g), map[string]string{"count": strconv.Itoa(len(g))})))
		c.w(`<table class="r3-tbl"><thead><tr><th scope="col" class="c-a">%s</th><th scope="col" class="c-q">%s</th><th scope="col" class="c-s">%s</th><th scope="col" class="c-g">%s</th><th scope="col" class="c-f"></th></tr></thead><tbody>`,
			cols[0], cols[1], cols[2], cols[3])
		var notes []string
		seen := map[string]bool{}
		for _, r := range g {
			env := ""
			if r.env {
				env = `<span class="r3-fl-env">` + c.t("html.fleet.env", map[string]string{"orchestrator": orch}) + `</span>`
			}
			c.w(`<tr><th scope="row" class="c-a">%s%s</th><td class="c-q"><span class="r3-fl-l">%s</span>%s</td><td class="c-s"><span class="r3-fl-l">%s</span>%s</td><td class="c-g"><span class="r3-fl-l">%s</span>%s</td><td class="c-f">%s</td></tr>`,
				html.EscapeString(untrusted.PlainText(r.agent)), env,
				cols[1], c.fleetAsks(r.override), cols[2], c.fleetSource(r), cols[3], c.fleetScore(r.agentID), c.fleetWarning(warn, r.edgeID))
			for _, a := range r.assumptions {
				if !seen[a] && !sharedAssumption[a] {
					seen[a] = true
					notes = append(notes, a)
				}
			}
		}
		c.w(`</tbody></table>`)
		for _, a := range notes {
			c.w(`<p class="r3-fl-note">%s</p>`, c.t("unknown."+a, map[string]string{"agent": orch}))
		}
		c.w(`</div>`)
	}
	c.w(`</section>`)
}

// fleetAsks is the «Does it ask before acting?» pill of an approval_override.
func (c htmlCtx) fleetAsks(override string) string {
	switch override {
	case graph.ApprovalSkipsAll:
		return `<span class="r3-pill red">` + c.t("html.fleet.asks.skips_all", nil) + `</span>`
	case graph.ApprovalKeepsAgentSettings:
		return `<span class="r3-pill green">` + c.t("html.fleet.asks.keeps_agent_settings", nil) + `</span>`
	}
	return `<span class="r3-pill amber">` + c.t("html.fleet.asks.unknown", nil) + `</span>`
}

// fleetSource is «How we know»: the file it was read in, by name only and as
// text read from the disk, or the factory setting.
func (c htmlCtx) fleetSource(r fleetRow) string {
	if r.certainty == graph.CertaintyObserved && r.file != "" {
		code := `<code>` + html.EscapeString(untrusted.PlainText(r.file)) + `</code>`
		return strings.Replace(c.t("html.fleet.source.observed", map[string]string{"file": "\x00"}), "\x00", code, 1)
	}
	return c.t("html.fleet.source.inferred", nil)
}

// fleetScore is the started agent's score and level on its own, as the report
// shows them; empty when the agent has no score.
func (c htmlCtx) fleetScore(agentID string) string {
	a := c.score[agentID]
	if a == nil || a.Value == nil {
		return ""
	}
	return `<span class="r3-pill ` + bandTone(a.Band) + `">` + strconv.Itoa(*a.Value) + `/100 · ` + c.t("band."+a.Band, nil) + `</span>`
}

// fleetWarning links to the problem with the edge's warning, or nothing.
func (c htmlCtx) fleetWarning(warn map[string]int, edgeID string) string {
	n, ok := warn[edgeID]
	if !ok {
		return ""
	}
	return `<a class="r3-fix" href="#p-` + strconv.Itoa(n) + `">` + c.t("html.fleet.warning_link", nil) + `</a>`
}

// renderDetail is the frontier between the summary and the problems' steps,
// only when there is at least one problem.
func (c htmlCtx) renderDetail(iss []issue) {
	if len(iss) == 0 {
		return
	}
	c.w(`<section class="r3-detail" aria-labelledby="r3-detail-h"><div><h2 id="r3-detail-h">%s</h2><p>%s</p></div></section>`,
		c.t("html.detail.title", nil), c.t("html.detail.intro", nil))
}

// fleetKeys are the catalog codes of the card, the tag and the frontier.
func fleetKeys() []string {
	return []string{
		"html.fleet.title", "html.fleet.counts", "html.fleet.counts.orchestrators.one", "html.fleet.counts.orchestrators.other",
		"html.fleet.counts.agents.one", "html.fleet.counts.agents.other", "html.fleet.intro", "html.fleet.starts.one", "html.fleet.starts.other",
		"html.fleet.col.agent", "html.fleet.col.asks", "html.fleet.col.source", "html.fleet.col.score",
		"html.fleet.asks.skips_all", "html.fleet.asks.keeps_agent_settings", "html.fleet.asks.unknown",
		"html.fleet.source.observed", "html.fleet.source.inferred", "html.fleet.env", "html.fleet.warning_link",
		"html.und.eyebrow", "html.tag.key", "html.detail.title", "html.detail.intro",
	}
}

const fleetRules = `
.r3-und{background:color-mix(in srgb,var(--ds-brand) 5%,var(--ds-raised));border-color:color-mix(in srgb,var(--ds-brand) 24%,var(--ds-line))}
.r3-und-eb{margin:0 0 -6px;display:flex;align-items:center;gap:8px;font-size:12px;line-height:1.3;font-weight:700;letter-spacing:.06em;text-transform:uppercase;color:var(--ds-brand)}
.r3-und-eb svg{width:18px;height:18px}
.r3-und .r3-fl-o{border-top-color:color-mix(in srgb,var(--ds-brand) 18%,var(--ds-line))}
.r3-und .r3-tbl tbody tr{border-bottom-color:color-mix(in srgb,var(--ds-brand) 14%,var(--ds-line))}
.r3-tag{display:inline-flex;align-items:center;gap:5px;vertical-align:2px;margin-right:8px;font-size:12px;line-height:1.3;font-weight:700;border:1px solid var(--ds-line);background:var(--ds-surface);color:var(--ds-ink-2);border-radius:6px;padding:1px 7px;white-space:nowrap}
.r3-tag svg{width:12px;height:12px}
.r3-p h2 .r3-tag{font-size:13px;margin-right:0}
.r3-detail{max-width:1320px;margin:40px auto 0;padding:0 32px}
.js.alt .r3-detail{display:none}
.r3-detail div{border-top:2px solid var(--ds-ink);padding-top:20px}
.r3-detail h2{margin:0;font-size:22px;line-height:1.3;font-weight:700}
.r3-detail p{margin:6px 0 0;font-size:15px;line-height:1.5;color:var(--ds-muted);max-width:640px}
@media (max-width:700px){.r3-detail{margin-top:28px;padding:0 16px}.r3-detail h2{font-size:20px}}
.r3-fl-intro{margin:0;font-size:16px;line-height:1.5}
.r3-fl-o{display:flex;flex-direction:column;gap:6px;border-top:1px solid var(--ds-line);padding-top:16px}
.r3-fl-oh{display:flex;justify-content:space-between;align-items:baseline;gap:4px 16px;flex-wrap:wrap}
.r3-fl-oh h3{margin:0;font-size:17px;line-height:1.3;font-weight:700}
.r3-fl-oh h3 span{font-size:14px;font-weight:400;color:var(--ds-muted);margin-left:8px}
.r3-fl-oh a{font-size:14px;font-weight:600;color:var(--ds-brand);white-space:nowrap}
.r3-fl .r3-tbl .c-a{width:auto;padding-left:0}
.r3-fl .r3-tbl .c-q{width:220px}
.r3-fl .r3-tbl .c-s{width:196px;color:var(--ds-ink-2)}
.r3-fl .r3-tbl .c-g{width:190px}
.r3-fl .r3-tbl .c-f{width:134px;text-align:right;white-space:nowrap}
.r3-fl-env{display:block;font-size:14px;font-weight:400;color:var(--ds-muted);margin-top:2px}
.r3-fl-note{margin:0;font-size:14px;line-height:1.5;color:var(--ds-muted)}
.r3-fl-l{display:none}
@media (max-width:700px){
.r3-fl-o{padding-top:12px}
.r3-fl .r3-tbl td.c-q,.r3-fl .r3-tbl td.c-s,.r3-fl .r3-tbl td.c-g{display:flex;gap:8px;align-items:baseline;margin-top:6px;width:auto}
.r3-fl .r3-tbl .c-f{text-align:left}
.r3-fl .r3-tbl td .r3-pill{white-space:normal}
.r3-fl-l{display:inline;flex:none;width:104px;font-size:13px;font-weight:700;color:var(--ds-muted)}
}`
