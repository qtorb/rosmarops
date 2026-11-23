package report_test

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-278 (0.19, signed design of 23-11): the «Your orchestrators» card, the
// «Key» tag of credential problems and the frontier before the detail.

// fleetGraph is v2Graph with three orchestrators starting Claude Code (the
// scored agent) and an agent with no score:
//   - Paseo, read in its config.json (keeps the agent's settings, with its
//     environment variables), with a file name a person would not write;
//   - Orca, from its factory default (skips every question);
//   - Paperclip, unknown, on the agents it has configured.
func fleetGraph(t *testing.T) graph.Graph {
	t.Helper()
	g := v2Graph(t)
	claude := ""
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Platform == knowledge.ClaudeCodeProduct {
			claude = n.ID
		}
	}
	if claude == "" {
		t.Fatal("no Claude Code in the graph")
	}
	other := "agent_ext_unscored"
	g.Nodes = append(g.Nodes, graph.Node{ID: other, Kind: graph.KindAgent, Type: graph.AgentExtension, Name: "Some Extension"})
	for _, o := range []struct {
		id, name, override, certainty, source string
		env                                   bool
		assume                                []string
		to                                    string
	}{
		{"agent_orch_paseo", "Paseo", graph.ApprovalKeepsAgentSettings, graph.CertaintyObserved, `/h/.paseo/con` + "\u202e" + `fig<b>.json`, true, nil, claude},
		{"agent_orch_orca", "Orca", graph.ApprovalSkipsAll, graph.CertaintyInferred, "kb:orca", false, []string{graph.AssumeProcessRunsAsUser, graph.AssumeOrchestratorSettingsNotRead}, claude},
		{"agent_orch_orca", "Orca", graph.ApprovalSkipsAll, graph.CertaintyInferred, "kb:orca", false, []string{graph.AssumeOrchestratorSettingsNotRead}, other},
		{"agent_orch_paperclip", "Paperclip", graph.ApprovalOverrideUnknown, graph.CertaintyInferred, "kb:paperclip", false, []string{graph.AssumeOrchestratorAgentConfigured}, claude},
	} {
		found := false
		for _, n := range g.Nodes {
			found = found || n.ID == o.id
		}
		if !found {
			g.Nodes = append(g.Nodes, graph.Node{ID: o.id, Kind: graph.KindAgent, Type: graph.AgentOrchestrator, Name: o.name, ConfigPath: "/h/." + strings.ToLower(o.name)})
		}
		g.Edges = append(g.Edges, graph.Edge{
			ID: "edge_launches_" + o.id + "_" + o.to, Type: graph.EdgeLaunches, From: o.id, To: o.to, Source: o.source,
			Certainty: o.certainty, ApprovalOverride: o.override, EnvPassthrough: o.env, Basis: "knowledge_base",
			DerivedFrom: []string{o.id, o.to}, Assumptions: o.assume,
		})
	}
	return g
}

func buildFleet(t *testing.T, g graph.Graph, dismissed []config.AllowEntryDismissed, accepted []config.AllowEntryAccepted) report.V2 {
	t.Helper()
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("ab", 32), Base: base, RepoRoots: []string{"/src/a", "/src/b"},
		Families: [][]string{{"/src/a"}, {"/src/b"}}, V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: v2Now,
		Dismissed: dismissed, Accepted: accepted,
	})
	return v
}

// fleetCard is the card's HTML, or "".
func fleetCard(page string) string {
	_, card, ok := strings.Cut(page, `<section class="r3-card r3-und r3-fl"`)
	if !ok {
		return ""
	}
	card, _, _ = strings.Cut(card, `</section>`)
	return card
}

// fleetGroup is one orchestrator's group in the card.
func fleetGroup(card, name string) string {
	_, g, ok := strings.Cut(card, `<div class="r3-fl-o"><div class="r3-fl-oh"><h3>`+name+`<span>`)
	if !ok {
		return ""
	}
	g, _, _ = strings.Cut(g, `<div class="r3-fl-o">`)
	return g
}

// Without an orchestrator the card is not there, and the report stays as it
// was.
func TestFleet_AbsentWithoutOrchestrators(t *testing.T) {
	g := v2Graph(t)
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	for _, lang := range []string{"en", "es"} {
		if page := renderHTML(t, lang, v); strings.Contains(page, `class="r3-card r3-und r3-fl"`) || strings.Contains(page, `<p class="r3-und-eb">`) {
			t.Errorf("%s: the card is drawn with no orchestrator", lang)
		}
	}
}

func TestFleet_Card(t *testing.T) {
	v := buildFleet(t, fleetGraph(t), nil, nil)
	page := renderHTML(t, "en", v)
	card := fleetCard(page)
	if card == "" {
		t.Fatal("no card")
	}
	// After block 3, before the folds.
	tpl, fl, folds := strings.Index(page, `class="r3-card r3-tpl"`), strings.Index(page, `r3-und r3-fl`), strings.Index(page, `<div class="r3-folds">`)
	if !(tpl < fl && fl < folds) {
		t.Errorf("card out of place: template %d, card %d, folds %d", tpl, fl, folds)
	}
	for _, want := range []string{
		`<p class="r3-und-eb"><svg`, `To understand</p>`, `>Your orchestrators</h2>`,
		// Distinct agents, not rows: Claude Code is started by all three.
		`<p class="r3-counts">3 orchestrators · 2 agents they can start</p>`,
		`An orchestrator puts other agents to work for you.`,
	} {
		if !strings.Contains(card, want) {
			t.Errorf("card has no %q", want)
		}
	}
	// No URL in the card: the Observatory link was removed (signed 23-11).
	if strings.Contains(card, "http") || strings.Contains(card, "What it does by default") {
		t.Error("the card links out")
	}
	// Ordered by orchestrator name.
	if o, p, q := strings.Index(card, "<h3>Orca"), strings.Index(card, "<h3>Paperclip"), strings.Index(card, "<h3>Paseo"); !(o < p && p < q) {
		t.Errorf("orchestrators not sorted: %d %d %d", o, p, q)
	}

	// Paseo: observed, keeps the settings, environment line, the file's
	// name only and shown, not obeyed.
	paseo := fleetGroup(card, "Paseo")
	for _, want := range []string{
		`starts 1 agent</span></h3>`, `r3-pill green">Yes: keeps its own settings`,
		`<span class="r3-fl-env">Gets Paseo&#39;s environment variables.</span>`,
		`Read in <code>con⟨U+202E⟩fig&lt;b&gt;.json</code>`,
	} {
		if !strings.Contains(paseo, want) {
			t.Errorf("Paseo has no %q:\n%s", want, paseo)
		}
	}
	if strings.Contains(paseo, "/h/.paseo") || strings.Contains(paseo, "\u202e") || strings.Contains(paseo, "<b>") {
		t.Error("Paseo shows the path or the raw file name")
	}
	if strings.Contains(paseo, `class="r3-fl-note"`) || strings.Contains(paseo, "See the warning") {
		t.Error("Paseo has a note or a warning link")
	}

	// Orca: inferred, skips all, its assumption once though two edges carry
	// it, the shared assumption left out; the warning link to its problem
	// only on the agent that has a warning.
	orca := fleetGroup(card, "Orca")
	if !strings.Contains(orca, `starts 2 agents</span></h3>`) || strings.Count(orca, `r3-pill red">No: started with no questions`) != 2 ||
		strings.Count(orca, "Assumed: its factory setting") != 2 {
		t.Errorf("Orca rows:\n%s", orca)
	}
	if n := strings.Count(orca, `<p class="r3-fl-note">`); n != 1 || !strings.Contains(orca, `<p class="r3-fl-note">Assumes Orca launches agents with its factory setting`) {
		t.Errorf("Orca notes: %d\n%s", n, orca)
	}
	if strings.Contains(orca, "runs as your user account") {
		t.Error("the shared assumption is in the note")
	}
	// The scored agent's pill; the unscored one's cell is empty.
	a := v.Index.Agents[0]
	if !regexp.MustCompile(`<td class="c-g"><span class="r3-fl-l">The agent&#39;s score on its own</span><span class="r3-pill (red|amber|green)">` + itoa(*a.Value) + `/100 · `).MatchString(orca) {
		t.Errorf("Claude Code's pill is not %d/100:\n%s", *a.Value, orca)
	}
	if !strings.Contains(orca, `<td class="c-g"><span class="r3-fl-l">The agent&#39;s score on its own</span></td>`) {
		t.Errorf("the unscored agent's score cell is not empty:\n%s", orca)
	}
	links := regexp.MustCompile(`<a class="r3-fix" href="#p-(\d+)">See the warning ›</a>`).FindAllStringSubmatch(orca, -1)
	if len(links) != 2 {
		t.Fatalf("Orca warning links: %v", links)
	}
	for _, l := range links {
		sec, ok := cutSection(page, "p-"+l[1])
		if !ok || !strings.Contains(sec, "Orca") {
			t.Errorf("link #p-%s does not lead to Orca's warning", l[1])
		}
	}

	// Paperclip: unknown, amber, its own assumption filled with its name.
	pc := fleetGroup(card, "Paperclip")
	if !strings.Contains(pc, `r3-pill amber">Unknown`) || !strings.Contains(pc, `<p class="r3-fl-note">Assumes you have set up in Paperclip an agent that uses it.`) {
		t.Errorf("Paperclip:\n%s", pc)
	}

	// Spanish: the literal texts.
	es := fleetCard(renderHTML(t, "es", v))
	for _, want := range []string{
		"Para entender</p>", ">Tus orquestadores</h2>", "3 orquestadores · 2 agentes que pueden lanzar",
		"lanza 2 agentes", "No: lo lanza sin preguntas", "Sí: mantiene sus ajustes", "No se sabe", "Leído en <code>", "Supuesto: su valor de fábrica",
		"Recibe las variables de entorno de Paseo.", "Ver el aviso ›", "Agente que lanza", "¿Pregunta antes de actuar?", "Cómo lo sabemos", "Nota del agente por su cuenta",
	} {
		if !strings.Contains(es, want) {
			t.Errorf("es: no %q", want)
		}
	}
}

// A dismissed or accepted warning leaves the cell empty.
func TestFleet_DismissedOrAcceptedWarning(t *testing.T) {
	g := fleetGraph(t)
	ids := []string{}
	for _, f := range buildFleet(t, g, nil, nil).Findings {
		if f.Rule == rules.RuleOrchestratorSkipsApprovals && strings.Contains(strings.Join(f.Evidence.Nodes, ","), "agent_orch_orca") {
			ids = append(ids, f.ID)
		}
	}
	if len(ids) != 2 {
		t.Fatalf("Orca findings: %v", ids)
	}
	v := buildFleet(t, g, []config.AllowEntryDismissed{{ID: ids[0], Date: "2026-11-23"}}, []config.AllowEntryAccepted{{ID: ids[1], Date: "2026-11-23"}})
	orca := fleetGroup(fleetCard(renderHTML(t, "en", v)), "Orca")
	if strings.Contains(orca, "See the warning") {
		t.Errorf("a dismissed or accepted warning is linked:\n%s", orca)
	}
	if strings.Count(orca, `<td class="c-f"></td>`) != 2 {
		t.Errorf("the warning cells are not empty:\n%s", orca)
	}
}

// The «Key» tag: before the title in the table, after it in the detail, only
// on credential problems; the frontier before the problems' steps.
func TestKeyTagAndFrontier(t *testing.T) {
	g := v2Graph(t)
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	for _, lang := range []string{"en", "es"} {
		page := renderHTML(t, lang, v)
		key := map[string]string{"en": "Key", "es": "Clave"}[lang]
		tag := `<span class="r3-tag"><svg`
		if !regexp.MustCompile(`<td class="c-p"><span class="r3-m">[^<]*<span class="r3-pill [a-z]+">[^<]*</span></span><span class="r3-tag"><svg[^>]*>.*?</svg>` + key + `</span><b>`).MatchString(page) {
			t.Errorf("%s: no tag before a problem's title in the table", lang)
		}
		if !regexp.MustCompile(`<h2 id="p-\d+-h"><span class="r3-n"[^>]*>\d+</span><span>[^<]*</span><span class="r3-tag"><svg`).MatchString(page) {
			t.Errorf("%s: no tag after a problem's title in the detail", lang)
		}
		// Only credential problems carry it: the config problem does not.
		_, tbl, _ := strings.Cut(page, `<table class="r3-tbl">`)
		tbl, _, _ = strings.Cut(tbl, `</table>`)
		for _, row := range strings.Split(tbl, "<tr>")[2:] {
			cred := strings.Contains(row, "within reach") || strings.Contains(row, "al alcance")
			if cred != strings.Contains(row, tag) {
				t.Errorf("%s: tag on a row %v: %.200s", lang, cred, row)
			}
		}
		// The frontier, once, right before the steps.
		title := map[string]string{"en": "The detail, problem by problem", "es": "El detalle, problema a problema"}[lang]
		if strings.Count(page, `<section class="r3-detail"`) != 1 || !strings.Contains(page, `<h2 id="r3-detail-h">`+title+`</h2>`) ||
			!strings.Contains(page, `</p></div></section><main class="rep"><div id="todo"><div class="r3-steps">`) {
			t.Errorf("%s: frontier", lang)
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }

// cutSection is the problem section with this id, up to the next one.
func cutSection(page, id string) (string, bool) {
	_, s, ok := strings.Cut(page, `<section class="r3-p" id="`+id+`"`)
	if !ok {
		return "", false
	}
	s, _, _ = strings.Cut(s, `<section class="r3-p"`)
	return s, true
}

// One agent started by three orchestrators counts once in the header.
func TestFleet_CountsDistinctAgents(t *testing.T) {
	g := fleetGraph(t)
	var edges []graph.Edge
	for _, e := range g.Edges {
		if e.Type != graph.EdgeLaunches || e.To != "agent_ext_unscored" {
			edges = append(edges, e)
		}
	}
	g.Edges = edges
	v := buildFleet(t, g, nil, nil)
	if c := fleetCard(renderHTML(t, "en", v)); !strings.Contains(c, `<p class="r3-counts">3 orchestrators · 1 agent it can start</p>`) {
		t.Errorf("header:\n%.400s", c)
	}
	if c := fleetCard(renderHTML(t, "es", v)); !strings.Contains(c, `<p class="r3-counts">3 orquestadores · 1 agente que puede lanzar</p>`) {
		t.Errorf("es header:\n%.400s", c)
	}
}
