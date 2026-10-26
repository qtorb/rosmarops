package graph

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/knowledge"
)

// Annex 2, point 1 (decision 2026-10-25, cómo trabajan tus agentes,
// cálculo): no probe name matches a protected or ignored path of the bases,
// on Windows and POSIX paths, inside a root and outside them all.
func TestProbe_NamesNotProtectedNorIgnored(t *testing.T) {
	cc, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	cu, err := knowledge.Cursor()
	if err != nil {
		t.Fatal(err)
	}
	prot, _ := cc.Entry(kbProtectedPaths)
	cprot, _ := cu.Entry(kbCursorProtected)
	cign, _ := cu.Entry(kbCursorIgnored)
	if len(prot.Paths) == 0 || len(cprot.Paths) == 0 || len(cign.Paths) == 0 {
		t.Fatal("no protected or ignored paths: test would be vacuous")
	}
	for _, home := range []string{"/home/alice", `C:\Users\alice`} {
		roots := []string{joinLike(home, "src"), joinLike(joinLike(home, "src"), "app")}
		targets, _ := probeTargets(append(roots, joinLike(home, probeName+"-root")), home)
		p := protectedPaths{entry: prot, configDir: joinLike(home, ".claude"), home: home}
		for _, tg := range targets {
			if p.protected(tg.Path) {
				t.Errorf("%s is a protected path of Claude Code", tg.Path)
			}
			name := tg.Path[strings.LastIndexAny(tg.Path, `/\`)+1:]
			if matchesAny(cprot.Paths, name) || ignoredAnyDepth(cign.Paths, name) {
				t.Errorf("%s is protected or ignored in Cursor", tg.Path)
			}
		}
	}
	for _, n := range ProbeNames() {
		if !strings.HasPrefix(n, probeName) || matchesAny(cprot.Paths, n) || ignoredAnyDepth(cign.Paths, n) {
			t.Errorf("probe name %q", n)
		}
	}
}

// A root that contains home leaves no target outside; otherwise there is
// exactly one, under home and outside every root.
func TestProbe_TargetsOutside(t *testing.T) {
	ts, noOutside := probeTargets([]string{"/home/alice/src/a", "/home/alice/src/b"}, "/home/alice")
	if noOutside || len(ts) != 3 || !ts[2].Outside || ts[2].Path != "/home/alice/rosmarops-probe-outside" {
		t.Fatalf("targets %+v %v", ts, noOutside)
	}
	ts, noOutside = probeTargets([]string{"/home"}, "/home/alice")
	if !noOutside || len(ts) != 1 || ts[0].Outside {
		t.Fatalf("root over home: %+v %v", ts, noOutside)
	}
}
