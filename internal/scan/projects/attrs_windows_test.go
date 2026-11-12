//go:build windows

package projects

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// P12 (Windows): the cloud placeholder and offline attributes are cloud; a
// reparse point is a link.
func TestAttrReasonWindows(t *testing.T) {
	for a, want := range map[uint32]Reason{
		attrRecallOnDataAccess: ReasonCloud, attrRecallOnOpen: ReasonCloud, attrOffline: ReasonCloud,
		attrReparsePoint: ReasonLinks, 0x10: "",
	} {
		if got := attrReason(a); got != want {
			t.Errorf("%#x: %q, want %q", a, got, want)
		}
	}
}

// P4 (Windows): a junction is not followed and is listed as a link.
func TestProjectsJunction(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "Trabajo", "x")
	repo(t, target)
	j := filepath.Join(home, "Desktop", "j")
	if err := os.MkdirAll(filepath.Dir(j), 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", j, home).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
	res := Search(Options{Home: home, SearchHome: true})
	if got := paths(res.Items); len(got) != 1 || got[0] != target {
		t.Fatalf("found %v", got)
	}
	if n := skipped(res, ReasonLinks); n == nil || n.Paths[0] != j {
		t.Errorf("links %+v", n)
	}
}
