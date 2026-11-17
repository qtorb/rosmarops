package graph_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

// startupEdges infers settings without dropping rule (b-iii)'s edges and
// returns Claude Code's reaches edges that carry startup_file_runs_later, by
// credential location.
func startupEdges(t *testing.T, settings platform.ClaudeSettingsScan) map[string]bool {
	t.Helper()
	f, _, err := graph.FromScan(inferScan(), settings, testSalt, scanTime)
	if err != nil {
		t.Fatal(err)
	}
	g, err := graph.Infer(graph.Build(f, testProvenance, scanTime), []string{permRepo}, inferHome)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graph.Encode(g); err != nil {
		t.Fatal(err)
	}
	nodes := map[string]graph.Node{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	out := map[string]bool{}
	for _, e := range g.Edges {
		if e.Type == graph.EdgeReaches && nodes[e.From].Platform == "claude-code" && contains(e.Assumptions, graph.AssumeStartupFileRunsLater) {
			if e.Via == "" || e.Certainty != graph.CertaintyInferred {
				t.Errorf("startup edge %+v", e)
			}
			for _, f := range graph.CredentialFiles(nodes[e.To]) {
				out[f] = true
			}
		}
	}
	return out
}

// BV-249 §4 (CTO-59 §4.3): a mode that writes anywhere without a person
// (bypassPermissions, or auto, which the classifier judges) writes a startup
// file outside the projects, and so reaches every credential, outside the
// roots too, even under blockReads; acceptEdits writes only in the project
// and the plan mode writes nothing.
func TestInfer_StartupFileReachesEverything(t *testing.T) {
	for _, c := range []struct {
		name  string
		perms platform.ClaudePermissions
		want  bool
	}{
		{"bypass with blockReads", platform.ClaudePermissions{DefaultMode: "bypassPermissions", BlockReadsOutsideWorkingDirectories: true}, true},
		{"auto", platform.ClaudePermissions{DefaultMode: "auto"}, true},
		{"acceptEdits, auto disabled", platform.ClaudePermissions{DefaultMode: "acceptEdits", DisableAutoMode: "disable", DisableAutoModePointer: "/permissions/disableAutoMode"}, false},
		{"plan, auto disabled", platform.ClaudePermissions{DefaultMode: "plan", DisableAutoMode: "disable", DisableAutoModePointer: "/permissions/disableAutoMode"}, false},
	} {
		got := startupEdges(t, settingsOf(userFile(c.perms)))
		for _, loc := range []string{claudeJSON, repoMCPJSON} {
			if got[loc] != c.want {
				t.Errorf("%s: startup edge to %s %v, want %v", c.name, loc, got[loc], c.want)
			}
		}
	}
}
