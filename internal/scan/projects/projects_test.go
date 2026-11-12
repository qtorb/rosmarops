package projects

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// countFS wraps the real file system and records every call (CTO-38 §6): the
// tests prove what the search reads and what it never enters.
type countFS struct {
	FS
	mu      sync.Mutex
	readDir []string
	lstat   []string
	info    map[string]fs.FileInfo // a stand-in Info for a path (other file system)
	network map[string]bool
}

func newCountFS() *countFS {
	return &countFS{FS: OS(), info: map[string]fs.FileInfo{}, network: map[string]bool{}}
}

func (c *countFS) ReadDir(dir string) ([]fs.DirEntry, error) {
	c.mu.Lock()
	c.readDir = append(c.readDir, dir)
	c.mu.Unlock()
	return c.FS.ReadDir(dir)
}

func (c *countFS) Lstat(p string) (fs.FileInfo, error) {
	c.mu.Lock()
	c.lstat = append(c.lstat, p)
	c.mu.Unlock()
	return c.FS.Lstat(p)
}

func (c *countFS) Info(dir string, e fs.DirEntry) (fs.FileInfo, error) {
	if fi, ok := c.info[filepath.Join(dir, e.Name())]; ok {
		return fi, nil
	}
	return c.FS.Info(dir, e)
}

func (c *countFS) Network(p string) bool { return c.network[p] || c.FS.Network(p) }

// readUnder reports whether any ReadDir was under dir (dir included).
func (c *countFS) readUnder(dir string) []string {
	var out []string
	for _, d := range c.readDir {
		if d == dir || strings.HasPrefix(d, dir+string(filepath.Separator)) {
			out = append(out, d)
		}
	}
	return out
}

func mkdir(t testing.TB, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
}

// repo makes a repository at p: a .git folder with something inside, and a src
// folder the search must never read.
func repo(t testing.TB, p string) {
	t.Helper()
	mkdir(t, filepath.Join(p, ".git", "objects"))
	mkdir(t, filepath.Join(p, "src", "pkg"))
}

func paths(items []Item) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Path)
	}
	sort.Strings(out)
	return out
}

func skipped(r Result, reason Reason) *NotSearched {
	for i := range r.Skipped {
		if r.Skipped[i].Reason == reason {
			return &r.Skipped[i]
		}
	}
	return nil
}

// realCase is the BV-218 tree with fake names: 50 repositories under
// ~/Trabajo at 2, 3 and 4 levels, plus the noise of CTO-38 §3.
func realCase(t testing.TB, home string) []string {
	var want []string
	add := func(p string) { repo(t, p); want = append(want, p) }
	for i := 0; i < 20; i++ {
		add(filepath.Join(home, "Trabajo", fmt.Sprintf("solo%02d", i)))
	}
	for i := 0; i < 20; i++ {
		add(filepath.Join(home, "Trabajo", fmt.Sprintf("org%d", i%5), fmt.Sprintf("r%02d", i)))
	}
	for i := 0; i < 10; i++ {
		add(filepath.Join(home, "Trabajo", "org9", "team", fmt.Sprintf("t%02d", i)))
	}
	repo(t, filepath.Join(home, ".cache", "go-build", "x"))                   // hidden
	repo(t, filepath.Join(home, "snap", "code", "x"))                         // Linux system folder
	repo(t, filepath.Join(home, "Trabajo", "solo00x", "node_modules", "pkg")) // forced
	for i := 0; i < 30; i++ {
		mkdir(t, filepath.Join(home, "Datasets", fmt.Sprintf("d%02d", i), "a", "b"))
	}
	sort.Strings(want)
	return want
}

// P1: the real case finds the 50, all from the user folder; an old
// configuration with 4 repositories in repo_roots and no search_home gives
// the 50, not 4.
func TestProjectsRealCase(t *testing.T) {
	home := t.TempDir()
	want := realCase(t, home)
	res := Search(Options{Home: home, SearchHome: true, GOOS: "linux"})
	if got := paths(res.Items); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("found %d, want %d:\n%v", len(got), len(want), got)
	}
	for _, it := range res.Items {
		if it.Source != SourceHome {
			t.Errorf("%s: source %s, want home", it.Path, it.Source)
		}
	}
	if res.Status != StatusComplete {
		t.Errorf("status %s", res.Status)
	}
	// v0.11.2's configuration: four repositories listed, search_home absent
	// (true): the search still finds the 50; the four are listed ones.
	old := want[:4]
	res = Search(Options{Home: home, SearchHome: true, Roots: old, GOOS: "linux"})
	if len(res.Items) != 50 {
		t.Fatalf("with repo_roots of v0.11.2: %d, want 50", len(res.Items))
	}
	for i, it := range res.Items[:4] {
		if it.Path != old[i] || it.Source != SourceListed {
			t.Errorf("listed %d: %+v", i, it)
		}
	}
}

// P2: a repository planted in each forbidden place is not found, nothing
// inside is read, and each reason is counted.
func TestProjectsSkips(t *testing.T) {
	for _, c := range []struct {
		goos, place string
		reason      Reason
	}{
		{"linux", ".hidden", ReasonHidden},
		{"linux", "snap", ReasonSystem},
		{"linux", filepath.Join("go", "pkg"), ReasonSystem},
		{"windows", "AppData", ReasonSystem},
		{"windows", "scoop", ReasonSystem},
		{"darwin", "Library", ReasonSystem},
		{"darwin", "Applications", ReasonSystem},
		{"darwin", filepath.Join("code", "Thing.app"), ReasonSystem},
		{"linux", filepath.Join("code", "Pods"), ReasonForced},
		{"linux", filepath.Join("code", "bower_components"), ReasonForced},
		{"linux", filepath.Join("code", "site-packages"), ReasonForced},
		{"linux", filepath.Join("code", "DerivedData"), ReasonForced},
		{"linux", filepath.Join("code", "build"), ReasonForced},
	} {
		t.Run(c.goos+"/"+filepath.ToSlash(c.place), func(t *testing.T) {
			home := t.TempDir()
			dir := filepath.Join(home, c.place)
			repo(t, filepath.Join(dir, "planted"))
			cfs := newCountFS()
			res := Search(Options{Home: home, SearchHome: true, GOOS: c.goos, FS: cfs})
			if len(res.Items) != 0 {
				t.Fatalf("found %v", paths(res.Items))
			}
			if r := cfs.readUnder(dir); len(r) != 0 {
				t.Errorf("read inside %s: %v", c.place, r)
			}
			if skipped(res, c.reason) == nil {
				t.Errorf("no %s in %+v", c.reason, res.Skipped)
			}
		})
	}
	// skip_folders only removes places.
	home := t.TempDir()
	repo(t, filepath.Join(home, "work", "keep"))
	repo(t, filepath.Join(home, "private", "x"))
	res := Search(Options{Home: home, SearchHome: true, GOOS: "linux", Skip: []string{filepath.Join(home, "private")}})
	if got := paths(res.Items); len(got) != 1 || filepath.Base(got[0]) != "keep" {
		t.Errorf("skip_folders: %v", got)
	}
	if n := skipped(res, ReasonUserSkip); n == nil || n.Paths[0] != filepath.Join(home, "private") {
		t.Errorf("user_skip %+v", n)
	}
}

// P4: a link looping to the user folder, a link to a repository and (on
// Windows) a junction are not followed and are listed; a root of repo_roots
// that is a link is resolved and walked.
func TestProjectsLinksAndLoops(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "Trabajo", "x")
	repo(t, target)
	outside := t.TempDir()
	repo(t, filepath.Join(outside, "y"))
	links := []string{filepath.Join(home, "Trabajo", "loop"), filepath.Join(home, "Desktop", "proyecto")}
	mkdir(t, filepath.Join(home, "Desktop"))
	if err := os.Symlink(home, links[0]); err != nil {
		t.Skipf("symlinks not available: %v", err)
	}
	if err := os.Symlink(target, links[1]); err != nil {
		t.Fatal(err)
	}
	res := Search(Options{Home: home, SearchHome: true, GOOS: runtime.GOOS})
	if got := paths(res.Items); len(got) != 1 || got[0] != target {
		t.Fatalf("found %v, want only %s once", got, target)
	}
	n := skipped(res, ReasonLinks)
	if n == nil || n.Count != 2 {
		t.Fatalf("links %+v", n)
	}
	sort.Strings(n.Paths)
	sort.Strings(links)
	if strings.Join(n.Paths, "|") != strings.Join(links, "|") {
		t.Errorf("link paths %v, want %v", n.Paths, links)
	}
	// A root that is a link is resolved once and walked.
	rootLink := filepath.Join(t.TempDir(), "code")
	if err := os.Symlink(outside, rootLink); err != nil {
		t.Fatal(err)
	}
	res = Search(Options{Roots: []string{rootLink}, GOOS: runtime.GOOS})
	if got := paths(res.Items); len(got) != 1 || filepath.Base(got[0]) != "y" || res.Items[0].Source != SourceRoot {
		t.Errorf("root link: %+v", res.Items)
	}
}

// P5: a repository 6 levels below the root is found; at 7, no, and depth
// counts it.
func TestProjectsDepth(t *testing.T) {
	home := t.TempDir()
	six := filepath.Join(home, "a", "b", "c", "d", "e", "six")
	seven := filepath.Join(home, "a", "b", "c", "d", "e", "f", "seven")
	repo(t, six)
	repo(t, seven)
	res := Search(Options{Home: home, SearchHome: true, GOOS: "linux"})
	if got := paths(res.Items); len(got) != 1 || got[0] != six {
		t.Fatalf("found %v", got)
	}
	if n := skipped(res, ReasonDepth); n == nil || n.Count < 1 {
		t.Errorf("depth %+v", n)
	}
}

// P6: nothing inside a repository found is read: neither .git nor src.
func TestProjectsStopsAtRepo(t *testing.T) {
	home := t.TempDir()
	r := filepath.Join(home, "code", "app")
	repo(t, r)
	repo(t, filepath.Join(r, "vendor", "inner")) // a repository inside: not counted
	cfs := newCountFS()
	res := Search(Options{Home: home, SearchHome: true, GOOS: "linux", FS: cfs})
	if got := paths(res.Items); len(got) != 1 || got[0] != r {
		t.Fatalf("found %v", got)
	}
	for _, d := range cfs.readUnder(r) {
		if d != r {
			t.Errorf("read inside the repository: %s", d)
		}
	}
}

// fakeTree is a synthetic file system of count folders, n children each,
// for the limits (P7). It answers Lstat for the previous review's
// repositories that still exist.
type fakeTree struct {
	root    string
	fanout  int
	slow    time.Duration
	clock   *time.Time
	exists  map[string]bool
	readDir int
}

type fakeEntry struct{ name string }

func (e fakeEntry) Name() string               { return e.name }
func (e fakeEntry) IsDir() bool                { return true }
func (e fakeEntry) Type() fs.FileMode          { return fs.ModeDir }
func (e fakeEntry) Info() (fs.FileInfo, error) { return fakeInfo(e), nil }

type fakeInfo struct{ name string }

func (i fakeInfo) Name() string       { return i.name }
func (i fakeInfo) Size() int64        { return 0 }
func (i fakeInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o700 }
func (i fakeInfo) ModTime() time.Time { return time.Time{} }
func (i fakeInfo) IsDir() bool        { return true }
func (i fakeInfo) Sys() any           { return nil }

func (f *fakeTree) ReadDir(dir string) ([]fs.DirEntry, error) {
	f.readDir++
	if f.slow > 0 {
		*f.clock = f.clock.Add(f.slow)
	}
	out := make([]fs.DirEntry, f.fanout)
	for i := range out {
		out[i] = fakeEntry{fmt.Sprintf("d%d", i)}
	}
	return out, nil
}

func (f *fakeTree) Lstat(p string) (fs.FileInfo, error) {
	if strings.HasSuffix(p, ".git") {
		if f.exists[filepath.Dir(p)] {
			return fakeInfo{".git"}, nil
		}
		return nil, fs.ErrNotExist
	}
	return fakeInfo{filepath.Base(p)}, nil
}
func (f *fakeTree) Info(_ string, e fs.DirEntry) (fs.FileInfo, error) { return e.Info() }
func (f *fakeTree) EvalSymlinks(p string) (string, error)             { return p, nil }
func (f *fakeTree) Network(string) bool                               { return false }

// P7: 250,000 folders stop at 200,000; a slow ReadDir stops at 30 s. Both:
// the search is incomplete, and the previous review's repositories that still
// exist are kept with source previous, those gone are not.
func TestProjectsLimits(t *testing.T) {
	root := filepath.FromSlash("/fake/home")
	still := filepath.Join(root, "d9", "d9", "kept")
	gone := filepath.Join(root, "d9", "d8", "gone")
	for _, c := range []struct {
		name string
		slow time.Duration
		want Status
	}{{"dirs", 0, StatusDirLimit}, {"time", 200 * time.Millisecond, StatusTimeLimit}} {
		t.Run(c.name, func(t *testing.T) {
			clock := time.Unix(1_000_000, 0)
			tree := &fakeTree{root: root, fanout: 12, slow: c.slow, clock: &clock, exists: map[string]bool{still: true}}
			res := Search(Options{
				Home: root, SearchHome: true, GOOS: "linux", FS: tree,
				Now: func() time.Time { return clock }, Previous: []string{still, gone},
			})
			if res.Status != c.want || res.Complete() {
				t.Fatalf("status %s, want %s", res.Status, c.want)
			}
			if c.want == StatusDirLimit && res.DirsRead != MaxDirs {
				t.Errorf("read %d folders, want %d", res.DirsRead, MaxDirs)
			}
			if c.want == StatusTimeLimit && clock.Sub(time.Unix(1_000_000, 0)) > MaxTime+time.Second {
				t.Errorf("ran %s, past %s", clock.Sub(time.Unix(1_000_000, 0)), MaxTime)
			}
			var prev []Item
			for _, it := range res.Items {
				if it.Source == SourcePrevious {
					prev = append(prev, it)
				}
			}
			if len(prev) != 1 || prev[0].Path != still {
				t.Errorf("previous kept %+v, want only %s", prev, still)
			}
			if n := skipped(res, ReasonIncomplete); n == nil {
				t.Error("no incomplete folders")
			}
		})
	}
}

// P9: 150 repositories found: the 100 most recently used (by .git/index) are
// kept and 50 are over_max; three listed in repo_roots always enter.
func TestProjectsMaxRepos(t *testing.T) {
	home := t.TempDir()
	base := time.Now().Add(-1000 * time.Hour)
	var recent []string
	for i := 0; i < 150; i++ {
		p := filepath.Join(home, "code", fmt.Sprintf("r%03d", i))
		repo(t, p)
		idx := filepath.Join(p, ".git", "index")
		if err := os.WriteFile(idx, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		at := base.Add(time.Duration(i) * time.Hour) // r149 is the most recent
		if err := os.Chtimes(idx, at, at); err != nil {
			t.Fatal(err)
		}
		if i >= 50 {
			recent = append(recent, p)
		}
	}
	var listed []string
	for i := 0; i < 3; i++ {
		p := filepath.Join(t.TempDir(), "listed")
		repo(t, p)
		listed = append(listed, p)
	}
	res := Search(Options{Home: home, SearchHome: true, Roots: listed, GOOS: "linux"})
	if len(res.Items) != 103 || res.Found != 153 {
		t.Fatalf("%d items, %d found", len(res.Items), res.Found)
	}
	for i, p := range listed {
		if res.Items[i].Path != p || res.Items[i].Source != SourceListed {
			t.Errorf("listed %d: %+v", i, res.Items[i])
		}
	}
	got := paths(res.Items[3:])
	if strings.Join(got, "|") != strings.Join(recent, "|") {
		t.Errorf("kept the wrong 100: first %s", got[0])
	}
	if n := skipped(res, ReasonOverMax); n == nil || n.Count != 50 {
		t.Errorf("over_max %+v", n)
	}
}

// P10 (search part): with search_home false, the list is exactly
// repo_roots' repositories, as v0.11.2 checked them.
func TestProjectsRepoRootsCompat(t *testing.T) {
	home := t.TempDir()
	realCase(t, home)
	var listed []string
	for i := 0; i < 4; i++ {
		p := filepath.Join(t.TempDir(), "r")
		repo(t, p)
		listed = append(listed, p)
	}
	res := Search(Options{Home: home, SearchHome: false, Roots: listed, GOOS: "linux"})
	if got := res.Repos(); strings.Join(got, "|") != strings.Join(listed, "|") {
		t.Errorf("got %v, want %v", got, listed)
	}
	if res.DirsRead != 0 {
		t.Errorf("read %d folders with nothing to walk", res.DirsRead)
	}
}

// P15 (search part): new and gone against the previous review.
func TestProjectsNewAndGone(t *testing.T) {
	home := t.TempDir()
	a, b := filepath.Join(home, "code", "a"), filepath.Join(home, "code", "b")
	repo(t, a)
	repo(t, b)
	res := Search(Options{Home: home, SearchHome: true, GOOS: "linux"})
	if n, g := Diff(res, nil); n != 0 || g != 0 {
		t.Errorf("first review: %d new, %d gone", n, g)
	}
	if n, g := Diff(res, []string{a, filepath.Join(home, "code", "deleted")}); n != 1 || g != 1 {
		t.Errorf("%d new, %d gone; want 1 and 1", n, g)
	}
}

// An indicator (~/.claude.json, workspace.json) is accepted once, only when
// it passes the filters; a hidden one or one under a system folder is not.
func TestProjectsIndicators(t *testing.T) {
	home := t.TempDir()
	elsewhere := filepath.Join(t.TempDir(), "proj")
	repo(t, elsewhere)
	hidden := filepath.Join(home, ".dotfiles")
	repo(t, hidden)
	sys := filepath.Join(t.TempDir(), "AppData", "x")
	repo(t, sys)
	res := Search(Options{
		Home: home, SearchHome: true, GOOS: "linux", SystemDirs: []string{filepath.Dir(sys)},
		Indicators: []Indicator{{elsewhere, SourceClaude}, {elsewhere, SourceWorkspace}, {hidden, SourceClaude}, {sys, SourceWorkspace}, {home, SourceClaude}},
	})
	if len(res.Items) != 1 || res.Items[0].Path != elsewhere || res.Items[0].Source != SourceClaude {
		t.Errorf("%+v", res.Items)
	}
}

// BenchmarkProjectsSearch is P13: the tree of CTO-38 §3 (50 repositories at
// 1 to 5 levels, each with folders in .git and src and 30 with node_modules,
// and the noise of hidden, snap, go/pkg, Documents and Datasets folders, a
// loop link and a link to a repository). It never fails: it logs the time and
// the folders read, the Windows figure CTO-38 asks for.
func BenchmarkProjectsSearch(b *testing.B) {
	home := b.TempDir()
	start := time.Now()
	dirs := benchTree(b, home)
	b.Logf("tree: %d folders created in %s", dirs, time.Since(start).Round(time.Millisecond))
	b.ResetTimer()
	var res Result
	for i := 0; i < b.N; i++ {
		res = Search(Options{Home: home, SearchHome: true})
	}
	b.StopTimer()
	b.ReportMetric(float64(res.DirsRead), "dirs_read")
	b.ReportMetric(float64(len(res.Items)), "repos")
	b.Logf("P13 %s/%s: %d repos, %d folders read, status %s, search %s (first run after creating the tree; not a cold cache)",
		runtime.GOOS, runtime.GOARCH, len(res.Items), res.DirsRead, res.Status, res.Duration.Round(time.Millisecond))
}

// benchTree builds the CTO-38 §3 tree under home and returns how many
// folders it made.
func benchTree(b *testing.B, home string) int {
	n := 0
	many := func(dir string, count int) {
		for i := 0; i < count; i++ {
			mkdir(b, filepath.Join(dir, fmt.Sprintf("n%02d", i/100), fmt.Sprintf("m%03d", i%100)))
		}
		n += count + count/100
	}
	bench := func(p string, nodeModules bool) {
		many(filepath.Join(p, ".git"), 296)
		many(filepath.Join(p, "src"), 150)
		if nodeModules {
			many(filepath.Join(p, "node_modules"), 1500)
		}
	}
	r := 0
	place := func(p string) {
		bench(p, r < 30)
		r++
	}
	for i := 0; i < 10; i++ {
		place(filepath.Join(home, fmt.Sprintf("repo%d", i)))
		place(filepath.Join(home, "Trabajo", fmt.Sprintf("solo%d", i)))
		place(filepath.Join(home, "Trabajo", fmt.Sprintf("org%d", i), fmt.Sprintf("r%d", i)))
		place(filepath.Join(home, "Trabajo", fmt.Sprintf("org%d", i), "team", fmt.Sprintf("t%d", i)))
		place(filepath.Join(home, "Trabajo", "org9", "team", "sub", fmt.Sprintf("s%d", i)))
	}
	for dir, count := range map[string]int{
		".cache": 40000, filepath.Join(".local", "share"): 10000, ".npm": 10000, "snap": 8000,
		filepath.Join("go", "pkg", "mod"): 10000, "Documents": 10000, "Downloads": 2000, "Datasets": 40000,
	} {
		many(filepath.Join(home, dir), count)
	}
	_ = os.Symlink(home, filepath.Join(home, "Trabajo", "loop"))
	_ = os.Symlink(filepath.Join(home, "repo0"), filepath.Join(home, "Trabajo", "link"))
	return n
}
