package report

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/rules"
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
	}
	return sv, nil
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
	weight, after := weightAndAfter(v)
	out := make([]SVFinding, 0, len(v.Findings))
	for _, f := range v.Findings {
		sf := SVFinding{
			ID: f.ID, Rule: f.Rule, Severity: f.Severity,
			Weight: weight[f.ID], ScoreAfterFix: after[f.ID],
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
			sf.Text[lang] = SVText{
				Title: c.titleText(f), WhatItMeans: c.impactText(f),
				Steps: steps, PasteBlock: paste,
			}
		}
		out = append(out, sf)
	}
	return out
}

// weightAndAfter maps each finding id to its weight and the score of its
// agent once the finding is gone (score_after_fix), from the index.
func weightAndAfter(v V2) (map[string]float64, map[string]*int) {
	weight := map[string]float64{}
	after := map[string]*int{}
	for _, ai := range v.Index.Agents {
		for _, w := range ai.Weights {
			weight[w.FindingID] = w.Weight
			without := w.Without
			after[w.FindingID] = &without
		}
	}
	return weight, after
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

// svChanges is what is new against the previous run: each finding the memory
// marks "new", with the finding's own title as its text (the catalog text the
// report already shows for it).
//
// Fuera de alcance (BV-119): the "fixed" (no-longer-observed) changes. A gone
// finding keeps only its rule, severity and dates in the memory, not the
// params its title needs, and the one catalog line for it
// (memory.no_longer_observed.line) both names the run's report file and can
// exceed the schema's 200-character text limit. A short per-change "fixed"
// label is not in the catalog, so writing one here would be new copy. Left
// for a signed short text.
func svChanges(v V2, ctxs map[string]htmlCtx) []SVChange {
	out := []SVChange{}
	for _, f := range v.Findings {
		if f.Memory == nil || f.Memory.Verdict != memory.VerdictNew {
			continue
		}
		id := f.ID
		text := map[string]string{}
		for lang, c := range ctxs {
			text[lang] = c.titleText(f)
		}
		out = append(out, SVChange{Kind: "new", FindingID: &id, Text: text})
	}
	return out
}

// svTopAction is the one thing to do today and the score it would give, from
// the index projection of the agent whose action is today's; null when there
// is no today action or no projection for it.
func svTopAction(v V2) *SVTopAction {
	if v.Today.FindingID == "" || v.Today.ActionID == "" {
		return nil
	}
	for _, ai := range v.Index.Agents {
		if ai.Value == nil || ai.Projection == nil || ai.Projection.ActionID != v.Today.ActionID {
			continue
		}
		return &SVTopAction{FindingID: v.Today.FindingID, ScoreFrom: *ai.Value, ScoreTo: ai.Projection.Value}
	}
	return nil
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
