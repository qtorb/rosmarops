//go:build windows

package credfiles_test

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/scan/credfiles"
)

// BV-120, point 3: with go 1.24 and godebug winsymlink=0 (go.mod), a junction
// inside a project reads as a symbolic link, as with Go 1.22: it is not
// followed and is noted in coverage as a link not followed. With
// winsymlink=1 it would read as ModeIrregular and be skipped without a note.
func TestCollect_JunctionNotFollowed(t *testing.T) {
	repo, outside := t.TempDir(), t.TempDir()
	write(t, filepath.Join(outside, "inner.env"), "GITHUB_TOKEN="+ghp(1)+"\n")
	link := filepath.Join(repo, ".env.local")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, outside).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v\n%s", err, out)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("junction mode: %v", info.Mode())
	if info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("junction reads as %v, not as a symbolic link (winsymlink=0)", info.Mode())
	}
	scan := credfiles.Collect(t.TempDir(), []string{repo}, noEnv)
	if len(scan.Entries) != 0 {
		t.Errorf("junction followed: %+v", scan.Entries)
	}
	if len(scan.Failed) != 1 || scan.Failed[0].Path != link ||
		scan.Failed[0].Reason != graph.ReasonSourceUnsupported+": symbolic link not followed" {
		t.Errorf("coverage: %+v", scan.Failed)
	}
	if len(scan.Failed) > 0 && strings.Contains(scan.Failed[0].Reason, ghp(1)) {
		t.Error("coverage holds the value")
	}
}
