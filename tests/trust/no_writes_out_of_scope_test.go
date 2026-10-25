package trust_test

// Trust test (CLAUDE.md invariant 2): running the real binary — init,
// baseline, run — writes nothing outside <HOME>/.rosmarops/, including writes
// made by the child processes it starts (npm, pnpm, git, code) when present.

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestNoWritesOutOfScope(t *testing.T) {
	s := newSandbox(t)
	allowed := filepath.Join(s.home, ".rosmarops")

	before := snapshot(t, s.root)
	s.runAll(t, nil)
	after := snapshot(t, s.root)

	var culprits []string
	for _, change := range changedPaths(before, after) {
		_, path, _ := strings.Cut(change, ": ")
		if !pathUnder(path, allowed) {
			culprits = append(culprits, change)
		}
	}
	if len(culprits) > 0 {
		t.Fatalf("%d path(s) changed outside %s:\n%s", len(culprits), allowed, strings.Join(culprits, "\n"))
	}
}
