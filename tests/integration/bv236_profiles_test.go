package integration_test

import (
	"reflect"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/profiles"
	"github.com/qtorb/rosmarops/internal/report"
)

// bv236V2 is the v2 report of the full test computer with the factory pass
// and the profile passes, as rosmarops run builds them.
func bv236V2(t *testing.T) report.V2 {
	t.Helper()
	in, scans := fullMachineInput(true)
	res, err := pipeline.Assemble(in)
	if err != nil {
		t.Fatal(err)
	}
	fres, err := pipeline.Assemble(pipeline.Factory(in))
	if err != nil {
		t.Fatal(err)
	}
	var ps []report.ProfileInput
	for _, id := range profiles.Measured {
		pin, ok := pipeline.Profile(in, id)
		if !ok {
			t.Fatalf("%s: no Claude Code settings to apply it to", id)
		}
		pres, err := pipeline.Assemble(pin)
		if err != nil {
			t.Fatal(err)
		}
		ps = append(ps, report.ProfileInput{ID: id, Graph: &pres.Graph, Settings: pin.Settings})
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	g := res.Graph
	v2, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: graphSHA(res.Encoded), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: in.RepoRoots, V1: report.Build("t", nil, nil, len(in.RepoRoots), 0, 0, 0, nil), GeneratedAt: fmNow, Salt: fmSalt,
		ClaudeSettings: &scans.Settings, Home: fmHome,
		Hermes: scans.Hermes, Codex: scans.Codex, Grok: scans.Grok, Cline: scans.Cline,
		FactoryGraph: &fres.Graph, Profiles: ps,
	})
	return v2
}

// TestBV236ProfilePermissions: the blocks the report copies are the ones the
// engine measures, and the ones BV-237 measured.
func TestBV236ProfilePermissions(t *testing.T) {
	for i, id := range profiles.Measured {
		p, err := profiles.Permissions(id)
		if err != nil {
			t.Fatal(err)
		}
		var in pipeline.Input
		in.Settings.Files = append(in.Settings.Files, platform.ClaudeSettingsFile{})
		bv237Profiles()[i].apply(&in)
		want := in.Settings.Files[0].Permissions
		if !reflect.DeepEqual(p, want) {
			t.Errorf("%s: block %+v, BV-237 measured %+v", id, p, want)
		}
	}
}

// TestBV236ProfilePass: on the full test computer, the profile passes give
// BV-237's numbers (Claude Code 11 D → 32 A with P0, 32 B with P1) and the
// computer's score with each profile is the lowest among its agents.
func TestBV236ProfilePass(t *testing.T) {
	prev := graph.HostOS
	defer func() { graph.HostOS = prev }()
	graph.HostOS = "windows"
	v2 := bv236V2(t)
	want := map[string]struct {
		value int
		class string
	}{profiles.P0: {32, "A"}, profiles.P1: {32, "B"}}
	var claudeID string
	for _, o := range v2.AgentsOverview {
		if o.Product == knowledge.ClaudeCodeProduct && o.Evaluated {
			claudeID = o.AgentID
		}
	}
	for _, a := range v2.Index.Agents {
		if a.AgentID != claudeID {
			if len(a.Profiles) != 0 {
				t.Errorf("%s has profiles: %+v", a.AgentID, a.Profiles)
			}
			continue
		}
		if a.Value == nil || *a.Value != 11 || a.Letter.Class == nil || *a.Letter.Class != "D" {
			t.Errorf("Claude Code today: %v %v, want 11 D", a.Value, a.Letter.Class)
		}
		if len(a.Profiles) != 2 {
			t.Fatalf("Claude Code profiles: %+v", a.Profiles)
		}
		for _, p := range a.Profiles {
			w := want[p.Profile]
			if p.Value != w.value || p.Letter.Class == nil || *p.Letter.Class != w.class {
				t.Errorf("%s: %d %v, want %d %s", p.Profile, p.Value, p.Letter.Class, w.value, w.class)
			}
		}
	}
	if len(v2.Index.Machine.Profiles) != 2 {
		t.Fatalf("machine profiles: %+v", v2.Index.Machine.Profiles)
	}
	for _, mp := range v2.Index.Machine.Profiles {
		low := 101
		for _, a := range v2.Index.Agents {
			v := a.Value
			for _, p := range a.Profiles {
				if p.Profile == mp.Profile.Profile {
					pv := p.Value
					v = &pv
				}
			}
			if v != nil && *v < low {
				low = *v
			}
		}
		if mp.Value != low || mp.AgentID == "" {
			t.Errorf("machine %s: %d by %q, want the lowest %d", mp.Profile.Profile, mp.Value, mp.AgentID, low)
		}
	}
	// report.v2 with the profiles validates, and the space view leaves them out.
	if _, err := report.RenderV2JSON(v2); err != nil {
		t.Fatalf("report.v2 with profiles fails schema validation: %v", err)
	}
	// The passes change nothing of today's report.
	if v2.Index.Machine.Value == nil || *v2.Index.Machine.Value > 11 {
		t.Errorf("machine today: %v", v2.Index.Machine.Value)
	}
}
