package graph_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-237 (CTO-50 §5.3): blockReadsOutsideWorkingDirectories set in one
// project's settings counts only in that project's sessions. With that
// project alone the read-only Bash commands do not reach the credential
// outside it; with a second project, whose sessions are not blocked, they do.
func TestBV237ProjectBlockReadsOnlyInItsProject(t *testing.T) {
	settings := settingsOf(projectFile(platform.ClaudePermissions{BlockReadsOutsideWorkingDirectories: true}))
	reachesOutside := func(roots []string) bool {
		f, _, err := graph.FromScan(inferScan(), settings, testSalt, scanTime)
		if err != nil {
			t.Fatal(err)
		}
		g, err := graph.Infer(graph.Build(f, testProvenance, scanTime), roots, inferHome)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range inferredEdges(t, g) {
			if e.typ == graph.EdgeReaches && e.to == claudeJSON && e.via == "shell_readonly_bash_commands" {
				return true
			}
		}
		return false
	}
	if reachesOutside([]string{permRepo}) {
		t.Error("one project with blockReads: Bash reaches the credential outside it")
	}
	if !reachesOutside([]string{permRepo, "/src/other"}) {
		t.Error("a second project without blockReads: Bash does not reach the credential outside the projects")
	}
}
