package trust_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/scan"
	"github.com/qtorb/rosmarops/internal/scan/projects"
	"github.com/qtorb/rosmarops/internal/scan/repos"
)

// recordFS records what the project search asks of the file system.
type recordFS struct {
	projects.FS
	mu    sync.Mutex
	calls []string // "ReadDir <path>", "Lstat <path>", "Info <path>"
}

func (r *recordFS) add(op, p string) {
	r.mu.Lock()
	r.calls = append(r.calls, op+" "+p)
	r.mu.Unlock()
}

func (r *recordFS) ReadDir(dir string) ([]fs.DirEntry, error) {
	r.add("ReadDir", dir)
	return r.FS.ReadDir(dir)
}

func (r *recordFS) Lstat(p string) (fs.FileInfo, error) {
	r.add("Lstat", p)
	return r.FS.Lstat(p)
}

func (r *recordFS) Info(dir string, e fs.DirEntry) (fs.FileInfo, error) {
	r.add("Info", filepath.Join(dir, e.Name()))
	return r.FS.Info(dir, e)
}

func plant(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(p, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
}

// searchTree is a user folder with repositories next to every forced
// exclusion, each holding a repository of its own.
func searchTree(t *testing.T) string {
	home := t.TempDir()
	plant(t, filepath.Join(home, "code", "app"))
	for _, x := range []string{"node_modules", "dist", "build", "target", "venv", "__pycache__", "coverage"} {
		plant(t, filepath.Join(home, "code", x, "x"))
		plant(t, filepath.Join(home, "work", "proj", x, "inner"))
	}
	if err := os.WriteFile(filepath.Join(home, "code", "notes.txt"), []byte("not opened"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

// P3 (invariant 5): no ReadDir of the project search receives a path with a
// forced exclusion as a component, and a .git inside node_modules/x is not
// found.
func TestSearchNeverEntersForcedExclusions(t *testing.T) {
	home := searchTree(t)
	rec := &recordFS{FS: projects.OS()}
	res := projects.Search(projects.Options{Home: home, SearchHome: true, FS: rec})
	for _, c := range rec.calls {
		op, p, _ := strings.Cut(c, " ")
		if op != "ReadDir" {
			continue
		}
		rel, _ := filepath.Rel(home, p)
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			if repos.IsForcedExclusion(part) {
				t.Errorf("the search read %s, inside the forced exclusion %s", p, part)
			}
		}
	}
	for _, it := range res.Items {
		if strings.Contains(it.Path, "node_modules") {
			t.Errorf("found a repository inside node_modules: %s", it.Path)
		}
	}
	if len(res.Items) != 1 {
		t.Errorf("found %d repositories, want 1 (code/app)", len(res.Items))
	}
}

// P11 (invariant 5, G3): the project search only lists folders and looks up
// entries; it never opens a file. Its file system has no Open, its source
// calls nothing that opens or reads a file, and a real search makes only
// ReadDir, Lstat and Info calls.
func TestSearchOpensNoFile(t *testing.T) {
	ft := reflect.TypeOf((*projects.FS)(nil)).Elem()
	for i := 0; i < ft.NumMethod(); i++ {
		if name := ft.Method(i).Name; strings.Contains(name, "Open") || strings.Contains(name, "Read") && name != "ReadDir" {
			t.Errorf("projects.FS has %s", name)
		}
	}
	// The package's source: no call that opens or reads a file.
	dir := filepath.Join(repoRoot(t), "internal", "scan", "projects")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{"Open": true, "OpenFile": true, "ReadFile": true, "Create": true, "WriteFile": true, "NewFile": true}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, e.Name()), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && (id.Name == "os" || id.Name == "ioutil" || id.Name == "syscall") && forbidden[sel.Sel.Name] {
				t.Errorf("%s calls %s.%s", e.Name(), id.Name, sel.Sel.Name)
			}
			return true
		})
	}
	// A real search: only listing and lookups.
	home := searchTree(t)
	rec := &recordFS{FS: projects.OS()}
	projects.Search(projects.Options{Home: home, SearchHome: true, FS: rec})
	for _, c := range rec.calls {
		op, _, _ := strings.Cut(c, " ")
		if op != "ReadDir" && op != "Lstat" && op != "Info" {
			t.Errorf("unexpected call %s", c)
		}
	}
}

// P8 (invariant 6, G2): the global deadline is the start of the project
// search plus the global limit for the projects found, never later. A search
// of 30 s that finds no project leaves 270 s, not 300.
func TestGlobalDeadlineFromSearchStart(t *testing.T) {
	start := time.Date(2026, 11, 12, 10, 0, 0, 0, time.UTC)
	for _, n := range []int{0, 1, 20, 48, 80, 100, 500} {
		if got, want := scan.DeadlineFor(start, n), start.Add(scan.GlobalTimeoutFor(n)); !got.Equal(want) {
			t.Errorf("%d projects: deadline %s, want %s", n, got, want)
		}
	}
	afterSearch := start.Add(projects.MaxTime)
	if left := scan.DeadlineFor(start, 0).Sub(afterSearch); left != 270*time.Second {
		t.Errorf("a 30 s search with 0 projects leaves %s, want 270s", left)
	}
	// The search's own limit is within the global minimum.
	if projects.MaxTime >= scan.MinGlobalTimeout {
		t.Errorf("projects.MaxTime %s is not within the global minimum %s", projects.MaxTime, scan.MinGlobalTimeout)
	}
}
