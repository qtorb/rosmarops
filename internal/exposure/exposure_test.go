package exposure

import (
	"reflect"
	"testing"

	"github.com/qtorb/rosmarops/internal/agents"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/rules"
)

// TestComplexityRegistryMissing (CTO-31 M1): an entry not marked Complete, or
// one with no sources, gives complexity no data (registry_missing), never a
// level — Cursor declares no source and must not come out as level 0. Today no
// entry is Complete, so every product has no complexity data.
func TestComplexityRegistryMissing(t *testing.T) {
	for _, e := range agents.All() {
		if e.Complete {
			t.Errorf("%s: registry entry marked Complete before the full registry (CTO-31 §a)", e.Product)
		}
		ax := complexity(e.Product)
		if ax.Level != nil || ax.NoData != NoDataRegistryMissing {
			t.Errorf("%s: complexity = level %v, no_data %q; want no level and registry_missing", e.Product, ax.Level, ax.NoData)
		}
	}
	if e, ok := agents.EntryOf("cursor"); !ok || len(e.Sources) != 0 {
		t.Fatalf("cursor is the no-sources case: entry %+v", e)
	}
	if ax := complexity("no-such-product"); ax.Level != nil || ax.NoData != NoDataRegistryMissing {
		t.Errorf("unknown product: complexity = %+v, want registry_missing", ax)
	}
}

// TestNotEvaluated (CTO-31 M3 i; CTO-26 §5, §6): a detected-only agent has
// opacity 3, every other axis with no level and no_data not_evaluated, and no
// class nor class status — so the JSON, whose class and class_status are
// omitempty, carries neither.
func TestNotEvaluated(t *testing.T) {
	a := Compute(Inputs{Product: "cursor", Evaluated: false, FactoryBand: "exposed"})
	op := a.Axes[AxisOpacity]
	if op.Level == nil || *op.Level != 3 || op.NoData != "" {
		t.Errorf("opacity = %+v, want level 3", op)
	}
	for _, ax := range AxisOrder {
		if ax == AxisOpacity {
			continue
		}
		if got := a.Axes[ax]; got.Level != nil || got.NoData != NoDataNotEvaluated {
			t.Errorf("%s = level %v, no_data %q; want no level and not_evaluated", ax, got.Level, got.NoData)
		}
	}
	if a.Class != "" || a.ClassStatus != "" {
		t.Errorf("class %q, class_status %q; want both empty (omitted in the JSON)", a.Class, a.ClassStatus)
	}
	if !a.CoherentWithBand() {
		t.Error("a not-evaluated agent must be vacuously coherent with its band")
	}
}

// TestOneAxisMissingIsProvisional (CTO-31 M3 ii): one axis with no data makes
// the class provisional, with that axis — and only it — in missing_axes; all
// six with data make it final.
func TestOneAxisMissingIsProvisional(t *testing.T) {
	full := func() map[string]Axis {
		m := map[string]Axis{}
		for _, ax := range AxisOrder {
			m[ax] = lvl(1)
		}
		return m
	}
	if c, st, miss, _ := classify(full()); st != "final" || len(miss) != 0 || c == "" {
		t.Fatalf("all axes with data: class %q status %q missing %v; want a final class", c, st, miss)
	}
	for _, ax := range AxisOrder {
		axes := full()
		axes[ax] = noData(NoDataRegistryMissing)
		c, st, miss, _ := classify(axes)
		if st != "provisional" || !reflect.DeepEqual(miss, []string{ax}) {
			t.Errorf("%s with no data: status %q missing %v; want provisional [%s]", ax, st, miss, ax)
		}
		if c == "" {
			t.Errorf("%s with no data: no class; a provisional class is still computed over the rest", ax)
		}
	}
}

// contagionGraph is a graph where client "a" has C1 (an instruction file it
// shares with "b") and/or C2 (a reads_config_of edge to "b").
func contagionGraph(c1, c2 bool) graph.Graph {
	g := graph.Graph{Nodes: []graph.Node{
		{ID: "a", Kind: graph.KindAgent, Type: graph.AgentAIClient},
		{ID: "b", Kind: graph.KindAgent, Type: graph.AgentAIClient},
	}}
	if c1 {
		for _, cl := range []string{"a", "b"} {
			g.Nodes = append(g.Nodes, graph.Node{
				ID: "instr_" + cl, Kind: graph.KindResource, Type: graph.ResourceAgentInstructions,
				Path: "/repo/AGENTS.md", Configures: cl,
			})
		}
	}
	if c2 {
		g.Edges = append(g.Edges, graph.Edge{ID: "e", Type: graph.EdgeReadsConfigOf, From: "a", To: "b"})
	}
	return g
}

// TestContagionC1C2 (CTO-31 M3 iii, signed): only C1 or only C2 → 1; both → 2;
// neither → 0 (no C3 here).
func TestContagionC1C2(t *testing.T) {
	for _, c := range []struct {
		name   string
		c1, c2 bool
		want   int
	}{
		{"neither", false, false, 0},
		{"only C1", true, false, 1},
		{"only C2", false, true, 1},
		{"C1 and C2", true, true, 2},
	} {
		ax := contagion(rules.OverviewAgent{}, contagionGraph(c.c1, c.c2), "a")
		if ax.Level == nil || *ax.Level != c.want {
			t.Errorf("%s: contagion = %+v, want level %d", c.name, ax, c.want)
		}
	}
}
