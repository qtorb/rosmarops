package graph_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

// probeVias are the capabilities of a hit's edges, by rule or knowledge
// entry (as inferredEdges names them).
func probeVias(g graph.Graph, h graph.ProbeHit) []string {
	nodes := map[string]graph.Node{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	var out []string
	for _, e := range h.Edges {
		via := nodes[e.DerivedFrom[1]]
		name := via.Rule
		if via.Basis == graph.BasisProductDefault {
			name = strings.SplitN(via.Source[strings.Index(via.Source, "/")+1:], "@", 2)[0]
		}
		out = append(out, name)
	}
	return out
}

func agentOf(g graph.Graph, platform string) string {
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient && n.Platform == platform {
			return n.ID
		}
	}
	return ""
}

// Annex 2, point 3 (decision 2026-10-25, cómo trabajan tus agentes,
// cálculo): Cursor's file tools write a file that is no agent resource
// inside a root and outside sandbox_protected_paths; not outside the roots.
// The probe adds nothing to the graph.
func TestProbe_CursorWritesWorkspaceFiles(t *testing.T) {
	g := cursorGraph(t, cursorFixture{})
	edges := len(g.Edges)
	probe, err := graph.Probe(g, []string{permRepo}, inferHome)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Edges) != edges {
		t.Fatal("the probe changed the graph")
	}
	ap := probe[agentOf(g, "cursor")]
	if len(ap.Write) != 2 || ap.Write[0].Target.Outside || !ap.Write[1].Target.Outside {
		t.Fatalf("write targets %+v", ap.Write)
	}
	if got := probeVias(g, ap.Write[0]); !contains(got, "file_tools_write_workspace") {
		t.Errorf("inside: %v", got)
	}
	if got := probeVias(g, ap.Write[1]); contains(got, "file_tools_write_workspace") {
		t.Errorf("outside: %v", got)
	}
	for _, hits := range [][]graph.ProbeHit{ap.Read, ap.Write} {
		for _, h := range hits {
			for _, e := range h.Edges {
				if !strings.HasPrefix(e.To, "probe_") {
					t.Errorf("probe edge to %s", e.To)
				}
			}
		}
	}
}

// Annex 2, point 1: one target per Edit allow in force, at a path its
// pattern covers; the target follows the rule's anchor.
func TestProbe_EditAllowTargets(t *testing.T) {
	user := userFile(platform.ClaudePermissions{Allow: []string{"Edit(//home/albert/.cursor/**)", "Edit(**/*.mdc)", "Edit(src/**)"}})
	g := inferGraph(t, settingsOf(user))
	probe, err := graph.Probe(g, []string{permRepo}, inferHome)
	if err != nil {
		t.Fatal(err)
	}
	ap := probe[agentOf(g, "claude-code")]
	got := map[string]string{}
	for _, h := range ap.Write {
		if h.Target.Edit == "" {
			continue
		}
		vias := probeVias(g, h)
		if len(vias) == 0 {
			t.Errorf("%s: no way", h.Target.Path)
		}
		got[h.Target.Path] = strings.Join(vias, ",")
	}
	want := map[string]string{
		"/home/albert/.cursor/rosmarops-probe": "Edit(//home/albert/.cursor/**)",
		"/src/app/rosmarops-probe.mdc":         "Edit(**/*.mdc)",
		"/src/app/src/rosmarops-probe":         "Edit(src/**)",
	}
	for p, w := range want {
		if !strings.Contains(got[p], w) {
			t.Errorf("%s: %q, want %s", p, got[p], w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("edit targets %v", got)
	}
	for _, h := range ap.Read {
		if h.Target.Edit != "" {
			t.Errorf("an Edit target among the reads: %s", h.Target.Path)
		}
	}
	if !reflect.DeepEqual(len(ap.Read), 2) {
		t.Errorf("%d read targets, want the root's and the outside one", len(ap.Read))
	}
}
