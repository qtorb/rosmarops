package report_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

// BV-88 (decision 2026-10-27, agentes nuevos, N1): with Claude Code
// evaluated and Codex detected, Codex is in «Detected, not evaluated yet»
// and the machine score is unavailable (agents_not_evaluated); Claude
// Code keeps its own score.
func TestIndex_CodexDetectedNotEvaluated(t *testing.T) {
	salt := []byte("0123456789abcdef0123456789abcdef")
	g := withoutCursor(v2Graph(t))
	// Without Codex, Claude Code alone: the machine has a score.
	alone := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	if len(alone.Index.Scope.NotEvaluated) != 0 || alone.Index.Machine.Unavailable != "" {
		t.Fatalf("without Codex: scope %+v, machine %+v", alone.Index.Scope, alone.Index.Machine)
	}
	present, _, err := graph.FromScans(platform.MCPScan{Present: []platform.AgentPresence{{Platform: "codex", ConfigDir: "/h/.codex", Marker: "/h/.codex/config.toml"}}},
		platform.ClaudeSettingsScan{}, platform.CredentialFileScan{}, salt, v2Now)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range present.Nodes {
		if n.Type == graph.AgentAIClient && n.Platform == "codex" {
			g.Nodes = append(g.Nodes, n)
		}
	}
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	if strings.Join(v.Index.Scope.NotEvaluated, ",") != "Codex" || !contains(v.Index.Scope.Evaluated, "Claude Code") ||
		v.Index.Machine.Unavailable != index.ReasonAgentsNotEvaluated {
		t.Fatalf("scope %+v, machine %+v", v.Index.Scope, v.Index.Machine)
	}
	var claude bool
	for _, a := range v.Index.Agents {
		claude = claude || a.Agent == "Claude Code"
	}
	if !claude {
		t.Errorf("Claude Code has no score: %+v", v.Index.Agents)
	}
	js, err := report.RenderV2JSON(v)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(js), `"unavailable": "`+index.ReasonAgentsNotEvaluated+`"`) {
		t.Error("report.v2.json: the machine score is not agents_not_evaluated")
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		prefix := htmlEscape(strings.SplitN(cat.Text("html.step.1.coverage", nil), "{", 2)[0])
		i := strings.Index(page, prefix)
		if i < 0 {
			t.Fatalf("%s: no coverage line", lang)
		}
		line := page[i:]
		line = line[:strings.Index(line, "<")]
		if !strings.Contains(line, "Codex") || !strings.Contains(line, "Claude Code") {
			t.Errorf("%s: coverage line %q", lang, line)
		} else {
			t.Logf("%s: %s", lang, line)
		}
	}
}

// withoutCursor drops Cursor's client and everything tied to it, so Claude
// Code is the only agent.
func withoutCursor(g graph.Graph) graph.Graph {
	drop := map[string]bool{}
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Platform == "cursor" {
			drop[n.ID] = true
		}
	}
	for _, n := range g.Nodes {
		if drop[n.Configures] {
			drop[n.ID] = true
		}
	}
	out := g
	out.Nodes, out.Edges = nil, nil
	for _, n := range g.Nodes {
		if !drop[n.ID] {
			out.Nodes = append(out.Nodes, n)
		}
	}
	for _, e := range g.Edges {
		if !drop[e.From] && !drop[e.To] {
			out.Edges = append(out.Edges, e)
		}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
