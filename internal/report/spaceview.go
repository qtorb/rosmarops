package report

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/scheduledtasks"
	"github.com/qtorb/rosmarops/internal/schemas"
	"github.com/qtorb/rosmarops/internal/schemaval"
	"github.com/qtorb/rosmarops/internal/untrusted"
)

// SpaceView mirrors schemas/v2/space-view.v1.schema.json (BV-119/BV-122,
// AP-17 §2): everything the customer area paints, already written by the
// program from its catalog in every available language. It carries no prose
// the program did not take from the catalog, no secret value and no trimmed
// view of a key. It is the content the rosmarops-results v1 package seals.
type SpaceView struct {
	SchemaVersion    string       `json:"schema_version"`
	RosmaropsVersion string       `json:"rosmarops_version"`
	GeneratedAt      string       `json:"generated_at"`
	HostID           string       `json:"host_id"`
	OS               string       `json:"os"`
	Agents           []SVAgent    `json:"agents"`
	Findings         []SVFinding  `json:"findings"`
	Keys             []SVKey      `json:"keys"`
	Changes          []SVChange   `json:"changes"`
	TopAction        *SVTopAction `json:"top_action"`

	// The optional fields BA-49/BV-158 added so the space keeps what leaves the
	// report: the score's evolution, how each agent works, what changed and the
	// analysis details. All optional in the schema; the program always fills
	// them. None carries any path, machine name or secret.
	Since    *SVSince     `json:"since"`
	Weekly   *SVWeekly    `json:"weekly"`
	Overview []SVOverview `json:"overview"`
	Analysis *SVAnalysis  `json:"analysis"`
	CardSVG  *string      `json:"card_svg"`

	// Report is the local report, also in the space (BV-177): the optional
	// `report` field of space-view.v1. The program always fills it.
	Report *SVReport `json:"report"`
}

// SVSince is «since the last check» as the report computes it: whether this run
// is comparable with the previous one (or the first), the finding counts, the
// per-agent score changes, and how many agents and MCP servers are new.
type SVSince struct {
	Comparable   bool            `json:"comparable"`
	First        bool            `json:"first"`
	New          int             `json:"new"`
	Gone         int             `json:"gone"`
	Still        int             `json:"still"`
	ScoreChanges []SVScoreChange `json:"score_changes"`
	NewAgents    int             `json:"new_agents"`
	NewMCP       int             `json:"new_mcp"`
}

// SVScoreChange is one agent whose score changed against the previous run,
// by its product slug (agents[].id).
type SVScoreChange struct {
	AgentID string `json:"agent_id"`
	From    int    `json:"from"`
	To      int    `json:"to"`
}

// SVWeekly is the automatic review's state, and its day and time when it is
// this computer's own task. It never carries the other copy's path or the
// executable's path (trust test, BV-158).
type SVWeekly struct {
	State string  `json:"state"`
	Day   *string `json:"day"`
	Time  *string `json:"time"`
}

// SVOverview is one agent's «how your agents work», the five verbs split by who
// decides each one, by the same criteria as the report's summary line.
type SVOverview struct {
	AgentID string   `json:"agent_id"`
	Nobody  []string `json:"nobody"`
	Model   []string `json:"model"`
	You     []string `json:"you"`
	Blocked []string `json:"blocked"`
}

// SVAnalysis is «how this analysis was made»: the threat-data stamp, when the
// agents' knowledge was reviewed, and the test/template-key count.
type SVAnalysis struct {
	IntelVersion        *string `json:"intel_version"`
	IntelDate           *string `json:"intel_date"`
	KnowledgeReviewedOn *string `json:"knowledge_reviewed_on"`
	TestdataCount       int     `json:"testdata_count"`
}

// SVAgent is one detected agent: its product slug, name, score and band when
// evaluated, the reason when not, its worst finding and what it is connected
// to.
type SVAgent struct {
	ID                 string      `json:"id"`
	Name               string      `json:"name"`
	Evaluated          bool        `json:"evaluated"`
	Score              *int        `json:"score"`
	Band               *string     `json:"band"`
	NotEvaluatedReason *string     `json:"not_evaluated_reason"`
	TopFindingID       *string     `json:"top_finding_id"`
	Services           []SVService `json:"services"`
}

// SVService is one external service an agent is configured to reach (the
// agents_overview services list, BV-121), without the radius scope.
type SVService struct {
	Service   string  `json:"service"`
	Category  string  `json:"category"`
	Access    string  `json:"access"`
	Approval  string  `json:"approval"`
	FindingID *string `json:"finding_id"`
}

// SVFinding is one finding with its texts by language, exactly as the report
// writes them from the catalog.
type SVFinding struct {
	ID            string            `json:"id"`
	Rule          string            `json:"rule"`
	Severity      string            `json:"severity"`
	AgentID       *string           `json:"agent_id"`
	Weight        float64           `json:"weight"`
	ScoreAfterFix *int              `json:"score_after_fix"`
	File          *string           `json:"file"`
	Text          map[string]SVText `json:"text"`
}

// SVText is a finding's text in one language (the four parts the report shows).
type SVText struct {
	Title       string   `json:"title"`
	WhatItMeans string   `json:"what_it_means"`
	Steps       []string `json:"steps"`
	PasteBlock  *string  `json:"paste_block"`
}

// SVKey is one credential of the machine: its fingerprint (keyed with the
// user salt, so the same key has the same fingerprint on every computer of
// the user), its class, the agents that reach it and its label by language.
type SVKey struct {
	Fingerprint string            `json:"fingerprint"`
	Access      string            `json:"access"`
	AgentIDs    []string          `json:"agent_ids"`
	Label       map[string]string `json:"label"`
}

// SVChange is one change against the previous run: a new or a fixed finding.
type SVChange struct {
	Kind       string            `json:"kind"`
	FindingID  *string           `json:"finding_id"`
	ScoreDelta *int              `json:"score_delta"`
	Text       map[string]string `json:"text"`
}

// SVTopAction is the one thing to do today and the score it would give.
type SVTopAction struct {
	FindingID string `json:"finding_id"`
	ScoreFrom int    `json:"score_from"`
	ScoreTo   int    `json:"score_to"`
}

// svLangs are the languages a space-view may carry: the schema's text and
// label objects allow only en and es (en required). A catalog language
// outside these is skipped, so the view never fails the schema.
func svLangs() []string {
	var out []string
	for _, l := range catalog.Langs() {
		if l == "en" || l == "es" {
			out = append(out, l)
		}
	}
	if len(out) == 0 {
		out = []string{catalog.DefaultLang}
	}
	return out
}

// svCtx is a per-language htmlCtx for the text helpers (titleText, impactText)
// the HTML report already uses, built once per language.
func svCtxs(v V2, home string) (map[string]htmlCtx, error) {
	ctxs := map[string]htmlCtx{}
	for _, lang := range svLangs() {
		cat, err := catalog.Load(lang)
		if err != nil {
			return nil, fmt.Errorf("message catalog %s: %w", lang, err)
		}
		c := htmlCtx{v: v, cat: cat, home: home, score: map[string]*index.AgentIndex{}}
		for i := range v.Index.Agents {
			c.score[v.Index.Agents[i].AgentID] = &v.Index.Agents[i]
		}
		c.rows = credRows(v)
		ctxs[lang] = c
	}
	return ctxs, nil
}

// BuildSpaceView builds the space-view v1 content from the run's v2 report,
// its graph and the version string. version is cut to major.minor.patch for
// the schema's strict pattern (a build suffix like "-dev" is dropped).
// It reads only what the report already holds; it writes nothing.
func BuildSpaceView(v V2, g *graph.Graph, home, version string) (SpaceView, error) {
	ctxs, err := svCtxs(v, home)
	if err != nil {
		return SpaceView{}, err
	}
	hostID := ""
	if g != nil {
		// graph.HostID is "host_<16 hex>"; the space view carries the bare
		// hex (its own pattern), so drop the prefix.
		hostID = strings.TrimPrefix(g.Provenance.HostID, "host_")
	}
	cardCtx := svPickCtx(ctxs)
	sv := SpaceView{
		SchemaVersion:    "space-view.v1",
		RosmaropsVersion: svVersion(version),
		GeneratedAt:      v.GeneratedAt,
		HostID:           hostID,
		OS:               v.OS,
		Agents:           svAgents(v, ctxs),
		Findings:         svFindings(v, ctxs, home),
		Keys:             svKeys(g, ctxs),
		Changes:          svChanges(v, ctxs),
		TopAction:        svTopAction(v),
		Since:            svSince(v, cardCtx),
		Weekly:           svWeekly(v),
		Overview:         svOverview(v),
		Analysis:         svAnalysis(v),
		CardSVG:          svCard(v, cardCtx),
		Report:           buildReport(v, ctxs, home),
	}
	return sv, nil
}

// svPickCtx returns the English htmlCtx when there is one, else any (the
// card_svg is one string, written in the source language).
func svPickCtx(ctxs map[string]htmlCtx) htmlCtx {
	if c, ok := ctxs[catalog.DefaultLang]; ok {
		return c
	}
	for _, c := range ctxs {
		return c
	}
	return htmlCtx{}
}

// svProductByAgentID maps an overview agent id to its product slug (agents[].id),
// the same id svAgents uses, so score_changes and overview point at agents[].
func svProductByAgentID(v V2) map[string]string {
	m := map[string]string{}
	for _, oa := range v.AgentsOverview {
		id := oa.Product
		if id == "" {
			id = oa.AgentID
		}
		m[oa.AgentID] = id
	}
	return m
}

// svSince is «since the last check» (BA-49 point 1): comparable/first from the
// memory, the finding counts as renderSince computes them, the per-agent score
// changes (max 20), and how many agents and MCP servers are new.
func svSince(v V2, c htmlCtx) *SVSince {
	m := v.Memory
	s := &SVSince{
		First:        !v.memoryPrev && !v.memoryLost,
		Comparable:   v.memoryPrev && m.Reason == "",
		New:          m.Counts[memory.VerdictNew] + m.Counts[memory.VerdictObservedAgain],
		Gone:         m.Counts[memory.VerdictNoLongerObserved],
		Still:        m.Counts[memory.VerdictStillObserved],
		ScoreChanges: []SVScoreChange{},
		NewAgents:    len(v.NewAgents),
		NewMCP:       len(v.NewMCP),
	}
	slug := svProductByAgentID(v)
	for _, a := range c.scoredAgents() {
		cmp := a.Comparison
		if cmp.Status != index.CompareDirect || cmp.Previous == nil || a.Value == nil || *cmp.Previous == *a.Value {
			continue
		}
		id := slug[a.AgentID]
		if id == "" {
			id = a.AgentID
		}
		s.ScoreChanges = append(s.ScoreChanges, SVScoreChange{AgentID: id, From: *cmp.Previous, To: *a.Value})
		if len(s.ScoreChanges) >= 20 {
			break
		}
	}
	return s
}

// svWeeklyDay maps scheduledtasks' full English weekday ("friday") to the
// space view's three-letter code ("fri"); "" for anything else.
var svWeeklyDay = map[string]string{
	"monday": "mon", "tuesday": "tue", "wednesday": "wed", "thursday": "thu",
	"friday": "fri", "saturday": "sat", "sunday": "sun",
}

// svWeekly is the automatic review's state, with the day and time only when it
// is this computer's own task. It never carries the other copy's path.
func svWeekly(v V2) *SVWeekly {
	w := v.weekly
	out := &SVWeekly{State: w.State}
	if out.State != scheduledtasks.WeeklyOwn && out.State != scheduledtasks.WeeklyOther && out.State != scheduledtasks.WeeklyNone {
		out.State = scheduledtasks.WeeklyNone
	}
	if w.State == scheduledtasks.WeeklyOwn {
		if d := svWeeklyDay[strings.ToLower(w.Day)]; d != "" {
			out.Day = &d
		}
		if w.Time != "" {
			t := w.Time
			out.Time = &t
		}
	}
	return out
}

// svOverview is «how your agents work» for the space (max 20): one entry per
// overview agent, its five verbs split by who decides each (nobody/model/you/
// blocked), from the same cells the report's summary line uses. The row and
// decider codes are exactly the schema's verbs and groups.
func svOverview(v V2) []SVOverview {
	slug := svProductByAgentID(v)
	out := []SVOverview{}
	for _, oa := range v.AgentsOverview {
		if len(out) >= 20 {
			break
		}
		id := slug[oa.AgentID]
		if id == "" {
			id = oa.AgentID
		}
		o := SVOverview{AgentID: id, Nobody: []string{}, Model: []string{}, You: []string{}, Blocked: []string{}}
		for _, cell := range oa.Rows {
			bucket := svOverviewBucket(&o, cell.Decider)
			if bucket == nil {
				continue // a decider outside the four groups: not shown (never happens with the rules' constants)
			}
			if !svContains(*bucket, cell.Row) {
				*bucket = append(*bucket, cell.Row)
			}
		}
		out = append(out, o)
	}
	return out
}

// svOverviewBucket returns the verb list of o for a decider code, or nil for an
// unknown one.
func svOverviewBucket(o *SVOverview, decider string) *[]string {
	switch decider {
	case rules.DeciderNobody:
		return &o.Nobody
	case rules.DeciderModel:
		return &o.Model
	case rules.DeciderYou:
		return &o.You
	case rules.DeciderBlocked:
		return &o.Blocked
	}
	return nil
}

func svContains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

// svAnalysis is «how this analysis was made»: the threat-data stamp, when the
// agents' knowledge was reviewed, and the test/template-key count. A missing
// intel stamp is null; the knowledge date is always known.
func svAnalysis(v V2) *SVAnalysis {
	a := &SVAnalysis{TestdataCount: v.TestData.Count}
	if s := v.Provenance.IntelVersion; s != "" {
		a.IntelVersion = &s
	}
	if s := v.Provenance.IntelPublishedAt; s != "" {
		a.IntelDate = &s
	}
	if s := knowledge.ReviewedOn; s != "" {
		a.KnowledgeReviewedOn = &s
	}
	return a
}

// svCard is the share card SVG of the lowest-scored agent, the same SVG the
// report's «Save my card» button saves, for the package's date. It carries
// nothing of the machine (index.Card's closed list; no_identity_in_card_test).
// null when no agent has a score.
func svCard(v V2, c htmlCtx) *string {
	a, ok := c.scored()
	if !ok {
		return nil
	}
	day, err := time.Parse(time.RFC3339, v.GeneratedAt)
	if err != nil {
		day = time.Now().UTC()
	}
	svg, _, ok := index.Card(*a, v.Silenced.Count, day, c.cat)
	if !ok {
		return nil
	}
	return &svg
}

// svVersion keeps the major.minor.patch of version (the schema pattern): a
// build or pre-release suffix after the first '-' is dropped.
func svVersion(version string) string {
	if i := strings.IndexByte(version, '-'); i >= 0 {
		return version[:i]
	}
	return version
}

// svBand maps the index band (exposed/open_gaps/contained) to the space
// view's red/yellow/green; an unknown band is null.
func svBand(b string) *string {
	var s string
	switch b {
	case index.BandExposed:
		s = "red"
	case index.BandOpenGaps:
		s = "yellow"
	case index.BandContained:
		s = "green"
	default:
		return nil
	}
	return &s
}

// svAccess maps a credential class to the keys list's access bucket (the
// signed schema has three, not four): high stays high; medium and low are
// both medium («Limited access» in the area, CTO-11 point 9); unclassified
// stays unclassified, only for a key the program cannot classify.
func svAccess(class string) string {
	switch class {
	case rules.ClassHigh:
		return "high"
	case rules.ClassMedium, rules.ClassLow:
		return "medium"
	}
	return "unclassified"
}

// svAgents is one entry per detected agent (agents_overview), with the score
// and band of the evaluated ones (from the index) and the services list.
// The id is the product slug (claude-code, cursor, …); a product seen twice
// is emitted once, the evaluated entry preferred.
func svAgents(v V2, ctxs map[string]htmlCtx) []SVAgent {
	byAgentID := map[string]*index.AgentIndex{}
	for i := range v.Index.Agents {
		byAgentID[v.Index.Agents[i].AgentID] = &v.Index.Agents[i]
	}
	top := topFindingByAgent(v)
	seen := map[string]int{} // product slug → position in out
	out := []SVAgent{}
	for _, oa := range v.AgentsOverview {
		id := oa.Product
		if id == "" {
			id = oa.AgentID
		}
		a := SVAgent{
			ID: id, Name: untrusted.PlainText(oa.Agent), Evaluated: oa.Evaluated,
			NotEvaluatedReason: oa.NotEvaluatedReason,
			Services:           []SVService{},
		}
		if ai := byAgentID[oa.AgentID]; ai != nil && ai.Value != nil {
			a.Score, a.Band = ai.Value, svBand(ai.Band)
		}
		if id, ok := top[oa.AgentID]; ok {
			a.TopFindingID = &id
		}
		for _, s := range oa.Services {
			a.Services = append(a.Services, SVService{
				Service: s.Service, Category: s.Category, Access: s.Access,
				Approval: s.Approval, FindingID: s.FindingID,
			})
		}
		if pos, ok := seen[id]; ok {
			// Same product twice: keep the evaluated one.
			if a.Evaluated && !out[pos].Evaluated {
				out[pos] = a
			}
			continue
		}
		seen[id] = len(out)
		out = append(out, a)
	}
	return out
}

// topFindingByAgent is each evaluated agent's worst finding by id: the one
// whose weight is highest, ties by the lowest finding id.
func topFindingByAgent(v V2) map[string]string {
	best := map[string]string{}
	weight := map[string]float64{}
	for _, ai := range v.Index.Agents {
		for _, w := range ai.Weights {
			cur, ok := best[ai.AgentID]
			if !ok || w.Weight > weight[ai.AgentID] || (w.Weight == weight[ai.AgentID] && w.FindingID < cur) {
				best[ai.AgentID], weight[ai.AgentID] = w.FindingID, w.Weight
			}
		}
	}
	return best
}

// svFindings is every v2 finding with its texts by language, written with the
// report's own catalog helpers (titleText, impactText) and the action's steps
// and paste block, so the space view says exactly what the report says.
func svFindings(v V2, ctxs map[string]htmlCtx, home string) []SVFinding {
	out := make([]SVFinding, 0, len(v.Findings))
	for _, f := range v.Findings {
		weight, after := findingWeightAndAfter(v, f)
		sf := SVFinding{
			ID: f.ID, Rule: f.Rule, Severity: f.Severity,
			Weight: weight, ScoreAfterFix: after,
			Text: map[string]SVText{},
		}
		if !rules.IsMachineFinding(f) && len(f.Evidence.Nodes) > 0 {
			agentID := f.Evidence.Nodes[0]
			sf.AgentID = &agentID
		}
		if file := svFile(f, home); file != "" {
			sf.File = &file
		}
		for lang, c := range ctxs {
			steps, paste := svSteps(c, f, home)
			sf.Text[lang] = clampFindingText(SVText{
				Title: c.titleText(f), WhatItMeans: c.impactText(f),
				Steps: steps, PasteBlock: paste,
			})
		}
		out = append(out, sf)
	}
	return out
}

// clampFindingText clamps a finding's text to the finding_text limits of
// space-view.v1 (title 200, what_it_means 600, each step 1000, paste_block
// 16000), with the same clipRunes rule clampReport uses for the report field
// (cut at the end with «…»). BV-201: without this an unbounded value — e.g. the
// {apps} list of a computer_use_app_allowed finding with many apps — overflowed
// findings[].text.*.title and the whole package was silently dropped.
func clampFindingText(t SVText) SVText {
	t.Title = clipRunes(t.Title, svMax200)
	t.WhatItMeans = clipRunes(t.WhatItMeans, svMax600)
	for i := range t.Steps {
		t.Steps[i] = clipRunes(t.Steps[i], svMax1000)
	}
	if t.PasteBlock != nil {
		p := clipRunes(*t.PasteBlock, svMaxPaste)
		t.PasteBlock = &p
	}
	return t
}

// findingWeightAndAfter is a finding's weight and the score of its agent once
// the finding is gone (score_after_fix), read from the agent the finding
// resolves to by the one shared rule (agentOfFinding). A machine finding
// (cred_in_git) weighs in every agent's score, so it reports the lowest-scored
// agent's figures — the same agent the report and svTopAction use — not an
// arbitrary one (CTO-30 A1). A finding with no scored agent, or an agent that
// carries no weight for it, gives (0, nil). A non-machine finding is in exactly
// its own agent's weights, so its figures are unchanged.
func findingWeightAndAfter(v V2, f rules.Finding) (float64, *int) {
	ai, ok := agentOfFinding(v, f)
	if !ok {
		return 0, nil
	}
	for _, w := range ai.Weights {
		if w.FindingID == f.ID {
			without := w.Without
			return w.Weight, &without
		}
	}
	return 0, nil
}

// svFile is the finding's file as the space view shows it: its first
// location in ~ form, with any deceptive character shown as ⟨U+XXXX⟩ (CTO-11
// C3: a path is read from disk); empty when it has none.
func svFile(f rules.Finding, home string) string {
	if len(f.Evidence.Locations) == 0 {
		return ""
	}
	return untrusted.PlainText(userPath(f.Evidence.Locations[0], home, "~"))
}

// svSteps are a finding's numbered steps and its paste block, from the same
// catalog texts the HTML and the AI export show for the finding's best
// action (aiAction): the prompt/how line, then what remains; the value to
// paste goes in the paste block, never in the steps. A remove-from-disk
// action has no value, so no paste block.
func svSteps(c htmlCtx, f rules.Finding, home string) ([]string, *string) {
	a, ok := rules.BestAction(f, c.v.Findings)
	if !ok {
		return []string{}, nil
	}
	p := c.params(f)
	switch a.Code {
	case rules.ActionHermesIsolatedBackend:
		pp := copyParams(p)
		pp["file"] = userPath(a.File, c.home, "~")
		return []string{
			c.cat.Text("html.action.hermes_isolated_backend.how", pp),
			c.cat.Text("html.after.full", p),
		}, nil
	case rules.ActionRemoveFromDisk:
		return []string{c.cat.Text("html.fix.remove", p)}, nil
	}
	prompt := "html.agent.prompt.merge"
	if !a.FileExists {
		prompt = "html.agent.prompt.create"
	}
	if a.Code == rules.ActionRotateCredential {
		prompt = "html.agent.prompt.rotated"
	}
	pp := copyParams(p)
	pp["file"] = userPath(a.File, c.home, "~")
	after := map[string]string{
		rules.CompletenessFull: "html.after.full", rules.CompletenessRadiusOnly: "html.after.radius",
		rules.CompletenessPartial: "html.after.partial",
	}[a.Completeness]
	if a.Code == rules.ActionRotateCredential {
		after = "html.after.rotate"
	}
	steps := []string{c.cat.Text(prompt, pp)}
	if after != "" {
		steps = append(steps, c.cat.Text(after, p))
	}
	// The paste block carries values read from the configuration (paths,
	// server commands): show any deceptive character as ⟨U+XXXX⟩, keeping only
	// newline and tab (CTO-11 C3).
	paste := untrusted.VisiblePaste(homeInRules(a.Value, c.home))
	return steps, &paste
}

// svKeys is every credential of the graph: its user-salt fingerprint, its
// class bucket, the agents that reach it (by product slug) and its label by
// language. No secret value, no trimmed view.
func svKeys(g *graph.Graph, ctxs map[string]htmlCtx) []SVKey {
	if g == nil {
		return []SVKey{}
	}
	product := map[string]string{} // agent node id → product slug
	scopes := map[string]string{}  // credential id → observed scope
	reach := map[string]map[string]bool{}
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent {
			product[n.ID] = n.Platform
		}
	}
	for _, e := range g.Edges {
		switch e.Type {
		case graph.EdgeAuthenticatesTo:
			if e.ObservedScope != "" && scopes[e.From] == "" {
				scopes[e.From] = e.ObservedScope
			}
		case graph.EdgeReaches:
			if reach[e.To] == nil {
				reach[e.To] = map[string]bool{}
			}
			if slug := product[e.From]; slug != "" {
				reach[e.To][slug] = true
			}
		}
	}
	out := []SVKey{}
	for _, n := range g.Nodes {
		if n.Kind != graph.KindCredential {
			continue
		}
		fp := strings.TrimPrefix(n.Fingerprint, "hmac-sha256:")
		if fp == "" {
			continue // nothing stable to show; never invent one
		}
		agentIDs := make([]string, 0, len(reach[n.ID]))
		for slug := range reach[n.ID] {
			agentIDs = append(agentIDs, slug)
		}
		sort.Strings(agentIDs)
		label := map[string]string{}
		for lang, c := range ctxs {
			label[lang] = c.cat.Text("html.cred."+svCredKey(c.cat, n, scopes[n.ID])+".name", nil)
		}
		out = append(out, SVKey{
			Fingerprint: fp, Access: svAccess(rules.ClassOf(n, scopes[n.ID])),
			AgentIDs: agentIDs, Label: label,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Fingerprint < out[j].Fingerprint })
	return out
}

// svCredKey picks the html.cred.<key>.name selector for a credential node,
// the same way the report's credKey picks it for a finding.
func svCredKey(cat catalog.Catalog, n graph.Node, scope string) string {
	switch n.Type {
	case graph.CredentialDBCredential:
		return "db"
	case graph.CredentialPrivateKey:
		return "private_key"
	}
	svc := n.Service
	if scope != "" && cat.Has("html.cred."+svc+"."+scope+".name") {
		return svc + "." + scope
	}
	if cat.Has("html.cred." + svc + ".name") {
		return svc
	}
	return "generic"
}

// svChanges is what changed against the previous run: each finding the memory
// marks "new", with the finding's own title as its text; and one "fixed"
// element per finding no longer observed this run, with the parameterless
// catalog line space.change_fixed (BV-128b). A gone finding keeps no title
// params in the memory, so its text names no finding; finding_id is null.
func svChanges(v V2, ctxs map[string]htmlCtx) []SVChange {
	out := []SVChange{}
	for _, f := range v.Findings {
		if f.Memory == nil || f.Memory.Verdict != memory.VerdictNew {
			continue
		}
		id := f.ID
		text := map[string]string{}
		for lang, c := range ctxs {
			// changes[].text is capped at 200 by the schema; clip with the same
			// rule as the report fields (BV-201), so a long title — e.g. a new
			// computer_use_app_allowed finding's {apps} list — never drops the
			// package.
			text[lang] = clipRunes(c.titleText(f), svMax200)
		}
		out = append(out, SVChange{Kind: "new", FindingID: &id, Text: text})
	}
	for range v.Memory.NoLongerObserved {
		text := map[string]string{}
		for lang, c := range ctxs {
			text[lang] = c.cat.Text("space.change_fixed", nil)
		}
		out = append(out, SVChange{Kind: "fixed", FindingID: nil, Text: text})
	}
	return out
}

// svTopAction is the one thing to do today and the score it would give, from
// the index projection of the agent today's finding belongs to; null when there
// is no today action, no such scored agent or no projection for it.
func svTopAction(v V2) *SVTopAction {
	if v.Today.FindingID == "" || v.Today.ActionID == "" {
		return nil
	}
	ai := svAgentOfToday(v)
	if ai == nil || ai.Value == nil || ai.Projection == nil {
		return nil
	}
	return &SVTopAction{FindingID: v.Today.FindingID, ScoreFrom: *ai.Value, ScoreTo: ai.Projection.Value}
}

// TodayFigures are the before/after scores the report shows for today's action:
// today's finding resolved to its agent by the one shared rule (agentOfFinding),
// then that agent's current score and its projection for today's action. ok is
// false when there is no today finding, no scored agent for it, or that agent
// has no projection. svTopAction is built from the same resolution, so the two
// must agree; this exported form lets the full-machine test compare the space
// against the report's own code, not a copy of it (CTO-30 A3 / §4 point 4).
func TodayFigures(v V2) (from, to int, ok bool) {
	// Resolve through agentOfFinding directly — the report's path (agentScore),
	// not svAgentOfToday — so a test comparing the space's top action against
	// this function catches the space resolving a different agent than the
	// report.
	for i := range v.Findings {
		if v.Findings[i].ID != v.Today.FindingID {
			continue
		}
		ai, ok := agentOfFinding(v, v.Findings[i])
		if !ok || ai.Value == nil || ai.Projection == nil {
			return 0, 0, false
		}
		return *ai.Value, ai.Projection.Value, true
	}
	return 0, 0, false
}

// svAgentOfToday resolves the agent whose score today's finding moves, through
// the one function the report also uses (agentOfFinding). BV-203 point 9: the
// agent is resolved from today's finding, never from v.Today.ActionID, which is
// a hash of the action's content (code, file, key, value, locations) and so
// does not identify an agent. A machine finding (cred_in_git) is in every
// agent's score and resolves to the lowest-scored agent, ties by name, exactly
// as the report's card does — so on Albert's machine it is «8 to 9» (Claude
// Code, the lowest), not «21 to 25» (whichever agent happened to come first in
// the index). Returns nil when no scored agent matches.
func svAgentOfToday(v V2) *index.AgentIndex {
	for i := range v.Findings {
		if v.Findings[i].ID == v.Today.FindingID {
			ai, _ := agentOfFinding(v, v.Findings[i])
			return ai
		}
	}
	return nil
}

// SchemaErrorFields extracts the field paths from a space-view schema-validation
// error (the error RenderSpaceViewJSON returns), and nothing else: each reported
// violation is a line "  <path>: <message>", and only the <path> before ": " is
// returned. BV-201 point 3: the sends log records why a package could not be
// built with the field path only — never the offending value, which lives after
// the ": " and is dropped here. Returns nil for any other error.
func SchemaErrorFields(err error) []string {
	if err == nil {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(err.Error(), "\n") {
		i := strings.Index(line, ": ")
		if i <= 0 {
			continue
		}
		path := strings.TrimSpace(line[:i])
		if path == "" || strings.ContainsAny(path, " \t") || seen[path] {
			continue
		}
		seen[path] = true
		out = append(out, path)
	}
	return out
}

// RenderSpaceViewJSON serialises sv and validates it against the embedded
// space-view v1 schema: an invalid view is an error, never sealed.
func RenderSpaceViewJSON(sv SpaceView) ([]byte, error) {
	data, err := json.Marshal(sv)
	if err != nil {
		return nil, fmt.Errorf("marshal space view: %w", err)
	}
	schema, err := schemaval.Compile(schemas.SpaceViewSchema)
	if err != nil {
		return nil, fmt.Errorf("compile space-view schema: %w", err)
	}
	result, err := schema.Validate(data)
	if err != nil {
		return nil, fmt.Errorf("validate space view: %w", err)
	}
	if !result.Valid() {
		msgs := ""
		for _, e := range result.Errors() {
			msgs += "\n  " + e.String()
		}
		return nil, fmt.Errorf("space view fails schema validation:%s", msgs)
	}
	return data, nil
}
