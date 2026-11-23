package trust_test

// Trust test (informe HTML v1, §9): the visual report is self-contained:
// no URL, no external resource, no network or storage API in its script.

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

func TestNoExternalInHTML(t *testing.T) {
	day := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	home, repo := "/home/alice", "/home/alice/projects/app"
	entries := []platform.CredentialEntry{credEntry(repo+"/.env", 1, "GITHUB_TOKEN", fakeGitHubPAT)}
	salt := []byte("0123456789abcdef0123456789abcdef")
	noExternalInHTML(t, scenarioV2(t, salt, home, repo, entries, day), home)
	// BV-278: with orchestrators, the «Your orchestrators» card is on the page
	// and still links nowhere (its Observatory link was removed, signed 23-11).
	withOrch := scenarioV2With(t, salt, home, repo, entries, day, func(g *graph.Graph) {
		claude := ""
		for _, n := range g.Nodes {
			if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient {
				claude = n.ID
			}
		}
		for _, o := range []struct{ id, name, certainty, source, override string }{
			{"agent_orch_paseo", "Paseo", graph.CertaintyObserved, home + "/.paseo/config.json", graph.ApprovalSkipsAll},
			{"agent_orch_orca", "Orca", graph.CertaintyInferred, "kb:orca", graph.ApprovalSkipsAll},
		} {
			g.Nodes = append(g.Nodes, graph.Node{ID: o.id, Kind: graph.KindAgent, Type: graph.AgentOrchestrator, Name: o.name, ConfigPath: home + "/." + o.id})
			g.Edges = append(g.Edges, graph.Edge{
				ID: "edge_launches_" + o.id, Type: graph.EdgeLaunches, From: o.id, To: claude, Source: o.source,
				Certainty: o.certainty, ApprovalOverride: o.override, EnvPassthrough: true,
				Assumptions: []string{graph.AssumeOrchestratorSettingsNotRead},
			})
		}
	})
	noExternalInHTML(t, withOrch, home)
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		if page := report.RenderHTML(withOrch, cat, home, time.UTC); !strings.Contains(page, `class="r3-card r3-und r3-fl"`) {
			t.Fatalf("%s: no orchestrators card — the case would be vacuous", lang)
		}
	}
}

// noExternalInHTML is the check itself, on one report.
func noExternalInHTML(t *testing.T, v report.V2, home string) {
	t.Helper()
	// BV-158: not connected, the «Your space» section shows the «Create your
	// space» button (the exact-URL exception below), and the card is in the
	// folded «More information» (the SVG-namespace exception). Both exceptions
	// are exercised on one unconnected page; connected, neither is in the report.
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		page := report.RenderHTML(v, cat, home, time.UTC)
		if !strings.Contains(page, `id="card-button"`) || len(v.Findings) == 0 {
			t.Fatalf("%s: no card or no finding — test would be vacuous", lang)
		}
		// The one exception (BV-41): the card's namespace attribute, at most
		// once, on the root <svg> of the card only; removed before the
		// search.
		root := `<template id="card-svg"><svg ` + index.SVGNamespaceAttr + " "
		if n := strings.Count(page, index.SVGNamespaceAttr); n > 1 || n == 1 && !strings.Contains(page, root) {
			t.Errorf("%s: the namespace attribute is not only on the card's root <svg> (%d)", lang, n)
		}
		page = strings.Replace(page, root, "", 1)

		// The second exception (BV-137 point 7, signed): the exact
		// href="https://app.rosmarops.com/space" of the «Your space» button,
		// at most once and only inside that section; removed before the
		// search, like the SVG attribute. Any other URL, and any other
		// app.rosmarops.com URL, still fails.
		const spaceHref = `href="https://app.rosmarops.com/space"`
		spaceStart := strings.Index(page, `<section class="space">`)
		spaceEnd := -1
		if spaceStart >= 0 {
			if rel := strings.Index(page[spaceStart:], `</section>`); rel >= 0 {
				spaceEnd = spaceStart + rel + len(`</section>`)
			}
		}
		switch n := strings.Count(page, spaceHref); {
		case n == 0:
			t.Errorf("%s: the «Your space» button URL is missing — exception not exercised", lang)
		case n > 1:
			t.Errorf("%s: the «Your space» URL appears %d times; at most once allowed", lang, n)
		default:
			if i := strings.Index(page, spaceHref); spaceStart < 0 || spaceEnd < 0 || i < spaceStart || i >= spaceEnd {
				t.Errorf("%s: the «Your space» URL is not inside the «Your space» section", lang)
			}
		}
		page = strings.Replace(page, spaceHref, "", 1)
		// Any other app.rosmarops.com URL (scheme + host) is forbidden; the AI
		// export's ai.what mentions the bare domain in prose, which loads
		// nothing, so only a schemed URL counts (the general scheme ban below
		// catches it too).
		if strings.Contains(page, "https://app.rosmarops.com") || strings.Contains(page, "http://app.rosmarops.com") {
			t.Errorf("%s: an app.rosmarops.com URL other than the allowed one is present", lang)
		}

		for _, banned := range []string{"http://", "https://", "src=", "<link", "fetch(", "XMLHttpRequest", "import(", "localStorage"} {
			if strings.Contains(page, banned) {
				t.Errorf("%s: the HTML contains %q", lang, banned)
			}
		}
	}
}
