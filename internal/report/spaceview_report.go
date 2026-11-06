package report

import (
	"strconv"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan"
	"github.com/qtorb/rosmarops/internal/untrusted"
)

// BV-177: the local report, also in the space. BuildSpaceView fills the
// optional `report` field of space-view.v1 with the same texts the local HTML
// report paints (internal/report/html.go), in every available language, written
// by the program — the space paints them with textContent, never as HTML. No
// new text is written here: every string comes from the same catalog keys the
// report uses. The design and the field's shape are in rosmarops-vigil
// docs/research/2026-11-04-informe-en-el-espacio.md (schema BA-54). Every text
// and list is clamped to its schema limit (clampReport) so an oversize report
// never stops the package from being sent.

// SVT is a text in both languages (schemas/v2 $defs t*). Both are required.
type SVT struct {
	EN string `json:"en"`
	ES string `json:"es"`
}

// SVReport is the `report` field: the local report's sections.
type SVReport struct {
	Step1      SVStep1 `json:"step1"`
	Step2      SVStep2 `json:"step2"`
	Rows       []SVRow `json:"rows"`
	HowFixBody SVT     `json:"how_fix_body"`
	AIExport   SVT     `json:"ai_export"`
	Coverage   []SVT   `json:"coverage"`
	Signals    []SVT   `json:"signals"`
}

// SVStep1 is «your situation».
type SVStep1 struct {
	Headline SVT   `json:"headline"`
	Counts   SVT   `json:"counts"`
	Meta     SVT   `json:"meta"`
	Warnings []SVT `json:"warnings"`
}

// SVStep2 is «what to do first»: the action's title and its lines.
type SVStep2 struct {
	ActionTitle SVT   `json:"action_title"`
	Lines       []SVT `json:"lines"`
}

// SVRow is one finding's extra row content (the per-finding texts the space
// already has in findings[]; this adds the verdict, locations, confidence,
// assumptions, fix steps and gain the report shows).
type SVRow struct {
	FindingID   string   `json:"finding_id"`
	Verdict     *SVT     `json:"verdict"`
	Locations   []string `json:"locations"`
	More        int      `json:"more"`
	Confidence  *SVT     `json:"confidence"`
	Assumptions []SVT    `json:"assumptions"`
	FixSteps    []SVT    `json:"fix_steps"`
	Gain        *SVT     `json:"gain"`
}

// Schema limits (BA-54 $defs and maxItems).
const (
	svMax300    = 300
	svMax600    = 600
	svMax1000   = 1000
	svMax200    = 200
	svMax400    = 400
	svMax49152  = 49152
	svMaxWarn   = 8
	svMaxLines  = 6
	svMaxRows   = 500
	svMaxLocs   = 5
	svMaxAssum  = 5
	svMaxFix    = 6
	svMaxCov    = 50
	svMaxSignal = 20
)

// buildReport builds the `report` field from the per-language htmlCtx the
// space-view builder already made. It never returns nil (the program always
// fills report); every field is clamped to its schema limit.
func buildReport(v V2, ctxs map[string]htmlCtx, home string) *SVReport {
	r := &SVReport{
		Step1: SVStep1{
			Headline: svtOf(ctxs, svHeadline),
			Counts:   svtOf(ctxs, svCounts),
			Meta:     svtOf(ctxs, svMeta),
			Warnings: zipT(ctxs, svWarnings),
		},
		Step2: SVStep2{
			ActionTitle: svtOf(ctxs, svStep2Title),
			Lines:       zipT(ctxs, svStep2Lines),
		},
		Rows:       svRows(v, ctxs, home),
		HowFixBody: svtOf(ctxs, func(c htmlCtx) string { return c.cat.Text("html.agentfix.body", nil) }),
		AIExport:   svtOf(ctxs, func(c htmlCtx) string { return RenderAIExport(v, c.cat, home) }),
		Coverage:   zipT(ctxs, svCoverage),
		Signals:    zipT(ctxs, svSignals),
	}
	clampReport(r)
	return r
}

// svtOf builds a T from a per-language text function. English is always
// present (svLangs guarantees it); Spanish falls back to English when absent.
func svtOf(ctxs map[string]htmlCtx, fn func(htmlCtx) string) SVT {
	en := fn(ctxs["en"])
	es := en
	if c, ok := ctxs["es"]; ok {
		es = fn(c)
	}
	return SVT{EN: en, ES: es}
}

// svtPtr is svtOf for a nullable T: nil when the English text is empty.
func svtPtr(ctxs map[string]htmlCtx, fn func(htmlCtx) string) *SVT {
	t := svtOf(ctxs, fn)
	if t.EN == "" && t.ES == "" {
		return nil
	}
	return &t
}

// zipT builds a list of T from a per-language list function. The lists are
// data-driven, so both languages have the same length; a mismatch pads with
// English so no T is half-empty.
func zipT(ctxs map[string]htmlCtx, fn func(htmlCtx) []string) []SVT {
	en := fn(ctxs["en"])
	es := en
	if c, ok := ctxs["es"]; ok {
		es = fn(c)
	}
	out := make([]SVT, 0, len(en))
	for i, e := range en {
		s := e
		if i < len(es) {
			s = es[i]
		}
		out = append(out, SVT{EN: e, ES: s})
	}
	return out
}

// --- step 1 (html.go renderStep1) ---

// svHeadline is the report's h1 headline (html.go §3): the scored agent's first
// finding, else the report's first; the perfect line at 100; else the status.
// For a finding whose rule has no html.headline key (the bespoke rules), it
// falls back to the finding's title, the same text the report shows.
func svHeadline(c htmlCtx) string {
	v := c.v
	headline := c.cat.Text("html.status."+v.Status, nil)
	a, scored := c.scored()
	switch {
	case len(v.Findings) > 0:
		f := v.Findings[0]
		if scored {
			for _, x := range v.Findings {
				if c.ofScoredAgent(x, a) {
					f = x
					break
				}
			}
		}
		if f.Rule == rules.RulePackageKnownMalicious {
			return c.cat.Text("finding.package_known_malicious.title", malParams(f))
		}
		key := "html.headline." + f.Rule
		if f.Rule == rules.RuleCredInGit && c.countRule(rules.RuleCredInGit) == 1 {
			key = "html.headline.cred_in_git.one"
		}
		if !c.cat.Has(key) {
			return c.titleText(f) // bespoke rule: no html.headline key
		}
		headline = c.cat.Text(key, map[string]string{"agent": f.Params.Agent})
	case scored && a.Value != nil && *a.Value == 100:
		headline = c.cat.Text("html.index.perfect", nil)
	}
	return headline
}

// svCounts is the step-1 counts line (html.step.1.counts[.multi]).
func svCounts(c htmlCtx) string {
	red, yellow := RowCounts(c.v)
	key := "html.step.1.counts"
	if len(c.v.Index.Scope.Evaluated) > 1 {
		key = "html.step.1.counts.multi"
	}
	return c.cat.Text(key, map[string]string{"red": strconv.Itoa(red), "yellow": strconv.Itoa(yellow)})
}

// svMeta is the projects-checked line (html.meta.projects). BV-184: when the
// person has dismissed findings this run, the same note the local report shows
// is appended, so the space says the score leaves them out — without changing
// the space-view schema.
func svMeta(c htmlCtx) string {
	meta := c.cat.Plural("html.meta.projects", c.v.repos, map[string]string{
		"projects": strconv.Itoa(c.v.repos), "minutes": strconv.Itoa(c.v.limitMinutes),
	})
	if len(c.v.Dismissed) > 0 {
		meta += " · " + c.cat.Text("report.dismissed.note", nil)
	}
	return meta
}

// svWarnings are the step-1 warning lines, each only when it fires (html.go
// renderStep1 §3, BV-158): silenced test data, hidden by configuration, the
// time limit reached, the integrity not checked, and knowledge older than 14
// days.
func svWarnings(c htmlCtx) []string {
	v := c.v
	var out []string
	if n := v.Silenced.Count; n > 0 {
		out = append(out, c.cat.Plural("html.silenced", n, map[string]string{"count": strconv.Itoa(n)}))
	}
	if n := v.HiddenCount(); n > 0 {
		out = append(out, c.cat.Plural("html.silenced.config", n, map[string]string{"count": strconv.Itoa(n)}))
	}
	if v.limitReached {
		out = append(out, c.cat.Text("html.limit.reached", map[string]string{
			"left": strconv.Itoa(v.notReviewed), "projects": strconv.Itoa(v.repos),
		}))
	}
	if v.integrityNotChecked {
		out = append(out, c.cat.Text("html.integrity.not_checked", nil))
	}
	if knowledgeOld(c.local, knowledge.ReviewedOn) {
		out = append(out, c.cat.Text("html.knowledge.old", map[string]string{"date": knowledgeDate(c.cat, knowledge.ReviewedOn)}))
	}
	return out
}

// --- step 2 (html.go renderStep2) ---

// todayFinding is the run's one-thing-to-do finding, and whether there is one.
func todayFinding(v V2) (rules.Finding, bool) {
	for _, f := range v.Findings {
		if f.ID == v.Today.FindingID {
			return f, true
		}
	}
	return rules.Finding{}, false
}

// todayAction is today's action of finding f (the one Today.ActionID names).
func todayAction(v V2, f rules.Finding) (rules.Action, bool) {
	for _, a := range f.Actions {
		if a.ID == v.Today.ActionID {
			return a, true
		}
	}
	return rules.Action{}, false
}

// svStep2Title is the action card's title (html.go renderStep2): the action's
// title for an auto-action, the finding's title for a bespoke rule, and the
// «nothing to do» line when there is no action today.
func svStep2Title(c htmlCtx) string {
	v := c.v
	f, ok := todayFinding(v)
	if !ok {
		return svNone(c)
	}
	if isBespokeFixRule(f.Rule) {
		return c.titleText(f)
	}
	if a, ok := todayAction(v, f); ok {
		return c.cat.Text("html.action."+a.Code+".title", c.params(f))
	}
	return svNone(c)
}

// svStep2Lines are the action card's lines below the steps (html.go
// renderStep2): the score projection when the action raises it, and the
// «after» line of the action. For a bespoke rule, the fix steps themselves.
func svStep2Lines(c htmlCtx) []string {
	v := c.v
	f, ok := todayFinding(v)
	if !ok {
		return nil
	}
	if isBespokeFixRule(f.Rule) {
		return svFixStepsText(c, f)
	}
	a, ok := todayAction(v, f)
	if !ok {
		return nil
	}
	var lines []string
	if s, ok := c.agentScore(f); ok && s.Value != nil && s.Projection != nil &&
		s.Projection.Value > *s.Value && a.Completeness != rules.CompletenessPartial {
		key := "html.today.projection"
		if len(v.Index.Scope.Evaluated) > 1 {
			key = "html.today.projection.agent"
		}
		lines = append(lines, c.cat.Text(key, map[string]string{
			"agent": s.Agent, "now": strconv.Itoa(*s.Value), "then": strconv.Itoa(s.Projection.Value),
		}))
	}
	after := map[string]string{
		rules.CompletenessFull: "html.after.full", rules.CompletenessRadiusOnly: "html.after.radius",
		rules.CompletenessPartial: "html.after.partial",
	}[a.Completeness]
	if a.Code == rules.ActionRotateCredential {
		after = "html.after.rotate"
	}
	if after != "" {
		lines = append(lines, c.cat.Text(after, c.params(f)))
	}
	return lines
}

// svNone is the «nothing to do today» line (html.none, or the did-not-finish
// line when the graph was not built).
func svNone(c htmlCtx) string {
	if !c.v.Coverage.GraphBuilt {
		return c.cat.Text("html.today.incomplete", nil)
	}
	return c.cat.Text("html.none", nil)
}

// isBespokeFixRule reports the rules whose fix the report renders from their
// own finding.* texts, not from a generic action.
func isBespokeFixRule(rule string) bool {
	switch rule {
	case rules.RulePackageKnownMalicious, rules.RuleProjectHookRunsRepoScript,
		rules.RuleMCPExternalAction, rules.RuleN8NMCPBridge, rules.RuleN8NKeyWithData:
		return true
	}
	return false
}

// svFixStepsText is a finding's fix steps as plain text (html.go renderRow /
// renderStep2), reusing the same catalog texts the report shows: the bespoke
// rules' own fix text, and the auto-action's prompt and «after» lines.
func svFixStepsText(c htmlCtx, f rules.Finding) []string {
	p := c.params(f)
	switch f.Rule {
	case rules.RulePackageKnownMalicious:
		return []string{c.cat.Text("finding.package_known_malicious.action", malParams(f))}
	case rules.RuleProjectHookRunsRepoScript:
		return []string{c.cat.Text("finding.project_hook_runs_repo_script.fix", p)}
	case rules.RuleMCPExternalAction:
		return []string{mcpAction(c.cat, p)}
	case rules.RuleN8NMCPBridge:
		return []string{c.cat.Text("finding.n8n_mcp_bridge.step1", p), c.cat.Text("finding.n8n_mcp_bridge.step2", p)}
	case rules.RuleN8NKeyWithData:
		return []string{c.cat.Text("finding.n8n_key_with_data.step1", p)}
	}
	steps, _ := svSteps(c, f, c.home)
	return steps
}

// --- rows (html.go renderRow) ---

func svRows(v V2, ctxs map[string]htmlCtx, home string) []SVRow {
	en := ctxs["en"]
	out := make([]SVRow, 0, len(v.Findings))
	for _, f := range v.Findings {
		f := f
		locs := svRowLocations(en, f, home)
		more := 0
		if len(locs) > svMaxLocs {
			more = len(locs) - svMaxLocs
			locs = locs[:svMaxLocs]
		}
		out = append(out, SVRow{
			FindingID:   f.ID,
			Verdict:     svtPtr(ctxs, func(c htmlCtx) string { return c.verdict(f) }),
			Locations:   locs,
			More:        more,
			Confidence:  svtPtr(ctxs, func(c htmlCtx) string { return c.cat.Text("html.tech.confidence."+f.Confidence, c.params(f)) }),
			Assumptions: zipT(ctxs, func(c htmlCtx) []string { return svAssumptions(c, f) }),
			FixSteps:    zipT(ctxs, func(c htmlCtx) []string { return svFixStepsText(c, f) }),
			Gain:        svtPtr(ctxs, func(c htmlCtx) string { return svGain(c, f) }),
		})
	}
	return out
}

// svRowLocations are the finding's locations with the home folder shown as ~
// and sanitized (html.go rowLocations + userPath), language-independent.
func svRowLocations(c htmlCtx, f rules.Finding, home string) []string {
	raw := c.rowLocations(f)
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		out = append(out, untrusted.PlainText(userPath(l, home, "~")))
	}
	return out
}

// svAssumptions are the finding's own (non-shared) assumptions as text
// (html.go assumptions).
func svAssumptions(c htmlCtx, f rules.Finding) []string {
	p := c.params(f)
	var out []string
	for _, a := range f.Evidence.Assumptions {
		if !sharedAssumption[a] {
			out = append(out, c.cat.Text("unknown."+a, p))
		}
	}
	return out
}

// svGain is the finding's gain line (html.go gain): the score without it, or
// the score after a radius-only action; empty when there is no score.
func svGain(c htmlCtx, f rules.Finding) string {
	a, ok := c.agentScore(f)
	if !ok || a.Value == nil {
		return ""
	}
	for _, wt := range a.Weights {
		if wt.FindingID != f.ID {
			continue
		}
		then := wt.Without
		key := "html.gain"
		act, has := rules.BestAction(f, c.v.Findings)
		switch {
		case has && act.Completeness == rules.CompletenessFull:
		case has && act.Completeness == rules.CompletenessRadiusOnly:
			v, ok := c.v.radiusProj[a.AgentID][act.ID]
			if !ok {
				return ""
			}
			then = v
		default:
			key += ".if_gone"
		}
		return c.cat.Text(key, map[string]string{"agent": f.Params.Agent, "now": strconv.Itoa(*a.Value), "then": strconv.Itoa(then)})
	}
	return ""
}

// --- coverage and signals (html.go renderCoverage / renderSignals) ---

// svCoverage are «what we could not read» lines (html.go renderCoverage): a
// failed source as «path (code)», an unsupported agent as «path (platform)»,
// a scan error, and the graph file when it was not built. Paths are sanitized.
func svCoverage(c htmlCtx) []string {
	cov := c.v.Coverage
	var out []string
	for _, f := range cov.SourcesFailed {
		code := f.Reason
		if i := indexColonSpace(f.Reason); i >= 0 {
			code = f.Reason[:i]
		}
		out = append(out, untrusted.PlainText(f.Path)+" ("+untrusted.PlainText(code)+")")
	}
	for _, a := range cov.AgentsUnsupported {
		out = append(out, untrusted.PlainText(a.ConfigPath)+" ("+untrusted.PlainText(a.Platform)+")")
	}
	for _, s := range cov.ScanErrors {
		if s != scan.GlobalTimeoutScope {
			out = append(out, untrusted.PlainText(s))
		}
	}
	if !cov.GraphBuilt {
		out = append(out, graph.FileName)
	}
	if cov.BinarySkipped > 0 { // BV-186: the binary files not opened
		out = append(out, c.cat.Plural("coverage.binary_skipped", cov.BinarySkipped, map[string]string{"n": strconv.Itoa(cov.BinarySkipped)}))
	}
	return out
}

// svSignals are «other signals on this computer» lines (html.go renderSignals):
// the signal's category text, with «· N» when the count is more than one.
func svSignals(c htmlCtx) []string {
	var out []string
	for _, sg := range c.v.Signals {
		line := c.cat.Text("html.signal."+sg.Category, nil)
		if sg.Count > 1 {
			line += " · " + strconv.Itoa(sg.Count)
		}
		out = append(out, line)
	}
	return out
}

// --- clamping (BV-177 point 3) ---

// clampReport clamps every text to its schema maxLength (cut at the end with
// «…») and every list to its maxItems (keeping the first), so an oversize
// report is always schema-valid and the package is never dropped for size.
func clampReport(r *SVReport) {
	clampT(&r.Step1.Headline, svMax300)
	clampT(&r.Step1.Counts, svMax300)
	clampT(&r.Step1.Meta, svMax300)
	r.Step1.Warnings = clampTList(r.Step1.Warnings, svMaxWarn, svMax600)

	clampT(&r.Step2.ActionTitle, svMax300)
	r.Step2.Lines = clampTList(r.Step2.Lines, svMaxLines, svMax600)

	if len(r.Rows) > svMaxRows {
		r.Rows = r.Rows[:svMaxRows]
	}
	for i := range r.Rows {
		row := &r.Rows[i]
		clampTPtr(row.Verdict, svMax200)
		clampTPtr(row.Confidence, svMax300)
		clampTPtr(row.Gain, svMax300)
		if len(row.Locations) > svMaxLocs {
			row.Locations = row.Locations[:svMaxLocs]
		}
		for j := range row.Locations {
			row.Locations[j] = clipRunes(row.Locations[j], svMax400)
		}
		row.Assumptions = clampTList(row.Assumptions, svMaxAssum, svMax600)
		row.FixSteps = clampTList(row.FixSteps, svMaxFix, svMax1000)
	}

	clampT(&r.HowFixBody, svMax600)
	clampT(&r.AIExport, svMax49152)
	r.Coverage = clampTList(r.Coverage, svMaxCov, svMax600)
	r.Signals = clampTList(r.Signals, svMaxSignal, svMax300)
}

func clampT(t *SVT, max int) {
	t.EN = clipRunes(t.EN, max)
	t.ES = clipRunes(t.ES, max)
}

func clampTPtr(t *SVT, max int) {
	if t != nil {
		clampT(t, max)
	}
}

func clampTList(in []SVT, maxItems, maxLen int) []SVT {
	if len(in) > maxItems {
		in = in[:maxItems]
	}
	for i := range in {
		clampT(&in[i], maxLen)
	}
	return in
}

// clipRunes cuts s to at most max runes, replacing the tail with «…» (which
// counts toward the limit) when it does not fit.
func clipRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	rs := []rune(s)
	if len(rs) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(rs[:max-1]) + "…"
}

func indexColonSpace(s string) int {
	for i := 0; i+1 < len(s); i++ {
		if s[i] == ':' && s[i+1] == ' ' {
			return i
		}
	}
	return -1
}
