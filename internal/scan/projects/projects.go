// Package projects finds the git repositories a review checks (CTO-38, signed
// by Albert 12-11 10:35): in every review, with no cache, it walks the whole
// user folder and each folder of repo_roots, up to MaxDepth levels below each
// root, stopping at each repository it finds. It never enters a hidden folder,
// a system folder, a forced exclusion (invariant 5), a link or reparse point,
// another or a network file system, or a cloud placeholder; it only lists
// folders and never opens a file (invariant 5, G3). Its own limits (MaxTime,
// MaxDirs, MaxRepos) count within the global limit of invariant 6 (G2).
package projects

import (
	"errors"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/scan/repos"
)

// The search's hardcoded limits (invariant 6, G2 and G7).
const (
	MaxTime  = 30 * time.Second // the search's own time
	MaxDirs  = 200_000          // folders read (ReadDir)
	MaxDepth = 6                // levels below each root
	MaxRepos = 100              // repositories found (listed ones are always kept)
)

// Source is where a repository came from (report.v2 coverage.projects.items[].source).
type Source string

// The sources (CTO-38 §4.3).
const (
	SourceHome      Source = "home"      // found in the user folder
	SourceRoot      Source = "root"      // found in a folder of repo_roots
	SourceClaude    Source = "claude"    // a project in ~/.claude.json
	SourceWorkspace Source = "workspace" // a folder open in Cursor or VS Code
	SourcePrevious  Source = "previous"  // from the previous review: the search did not finish
	SourceListed    Source = "listed"    // a repo_roots entry that is itself a repository
)

// Reason is why a place was not searched (coverage.projects.not_searched[].reason).
type Reason string

// The reasons, in the report's order (CTO-38 §4.2).
const (
	ReasonHidden     Reason = "hidden"
	ReasonSystem     Reason = "system"
	ReasonForced     Reason = "forced"
	ReasonLinks      Reason = "links"
	ReasonOtherFS    Reason = "other_fs"
	ReasonNetwork    Reason = "network"
	ReasonCloud      Reason = "cloud"
	ReasonUnreadable Reason = "unreadable"
	ReasonDepth      Reason = "depth"
	ReasonUserSkip   Reason = "user_skip"
	ReasonOverMax    Reason = "over_max"
	ReasonIncomplete Reason = "incomplete"
)

// ReasonOrder is the order of not_searched in the report.
var ReasonOrder = []Reason{
	ReasonHidden, ReasonSystem, ReasonForced, ReasonLinks, ReasonOtherFS, ReasonNetwork, ReasonCloud,
	ReasonUnreadable, ReasonDepth, ReasonUserSkip, ReasonOverMax, ReasonIncomplete,
}

// pathReasons keep the paths of what they count; the fixed categories
// (hidden, system, forced) and depth keep only a count (CTO-38 §4.2).
var pathReasons = map[Reason]bool{
	ReasonLinks: true, ReasonOtherFS: true, ReasonNetwork: true, ReasonCloud: true,
	ReasonUnreadable: true, ReasonUserSkip: true, ReasonOverMax: true, ReasonIncomplete: true,
}

// MaxPaths is the most paths kept per reason (report.v2 maxItems 50).
const MaxPaths = 50

// Status is how the search ended (coverage.projects.search).
type Status string

// The statuses.
const (
	StatusComplete  Status = "complete"
	StatusTimeLimit Status = "time_limit"
	StatusDirLimit  Status = "dir_limit"
	StatusOff       Status = "off" // search_home false and no folder to walk
)

// Indicator is a repository another tool already names (~/.claude.json's
// projects, an editor's workspace.json): accepted if it passes the filters,
// never walked (CTO-38 §1.1, point 3).
type Indicator struct {
	Path   string
	Source Source // SourceClaude or SourceWorkspace
}

// Options are a search's inputs.
type Options struct {
	Home       string   // the user folder
	SearchHome bool     // walk the whole user folder (search_home, true when absent)
	Roots      []string // repo_roots: a repository is kept; a folder is walked
	Skip       []string // skip_folders: absolute paths never searched
	Indicators []Indicator
	Previous   []string // the previous review's repositories (st.Baselines.Repos)
	// SystemDirs are folders a project never lives in (AppData, LocalAppData,
	// Program Files, the Windows folder): a candidate under one is left out.
	SystemDirs []string
	GOOS       string

	FS    FS               // nil: the real file system
	Now   func() time.Time // nil: time.Now
	Limit Limits           // zero: the hardcoded limits
}

// Limits are the search's limits; tests shrink them, production never does.
type Limits struct {
	Time  time.Duration
	Dirs  int
	Depth int
	Repos int
}

// Item is one repository to check.
type Item struct {
	Path   string
	Source Source
}

// NotSearched is one reason and what it left out.
type NotSearched struct {
	Reason Reason
	Count  int
	Paths  []string // absolute; at most MaxPaths
}

// Result is a search's outcome.
type Result struct {
	Status   Status
	Items    []Item   // the repositories to check, in order
	Found    int      // repositories found, before MaxRepos
	Roots    []string // the folders walked
	DirsRead int
	Skipped  []NotSearched // in ReasonOrder, only the reasons with a count
	Started  time.Time     // when the search began: the global limit counts from here (G2)
	Duration time.Duration
}

// Repos are the result's repository paths, in order.
func (r Result) Repos() []string {
	out := make([]string, len(r.Items))
	for i, it := range r.Items {
		out[i] = it.Path
	}
	return out
}

// Complete reports whether the search walked everything it meant to.
func (r Result) Complete() bool {
	return r.Status == StatusComplete || r.Status == StatusOff
}

// NotSearchedTotal is the number of places not searched, for the console.
func (r Result) NotSearchedTotal() int {
	n := 0
	for _, s := range r.Skipped {
		if s.Reason == ReasonIncomplete {
			continue // said by console.projects.incomplete
		}
		n += s.Count
	}
	return n
}

// errStop ends the walk when a limit is reached.
var errStop = errors.New("projects: search limit reached")

// heavyFolders never hold a repository and weigh a lot (CTO-38 §1.3); fixed,
// not configurable, it can only grow.
var heavyFolders = map[string]bool{"bower_components": true, "Pods": true, "DerivedData": true, "site-packages": true}

// macPackages are macOS bundles, skipped at any level.
var macPackages = []string{".app", ".photoslibrary", ".musiclibrary"}

// homeSkips are the first-level folders under the user folder skipped on
// each system (CTO-38 §1.3); a path with a separator is a second level
// (go/pkg).
func homeSkips(goos string) []string {
	switch goos {
	case "windows":
		return []string{"AppData", "scoop", filepath.Join("go", "pkg")}
	case "darwin":
		return []string{"Library", "Applications", filepath.Join("go", "pkg")}
	default:
		return []string{"snap", filepath.Join("go", "pkg")}
	}
}

// searcher is one search's state.
type searcher struct {
	o       Options
	lim     Limits
	fold    bool // compare paths without regard to case (Windows, macOS)
	now     func() time.Time
	start   time.Time
	dirs    int
	status  Status
	skipped map[Reason]*NotSearched
	found   []Item          // walked or indicated, before MaxRepos
	seen    map[string]bool // repository keys
	skip    []string        // skip_folders, cleaned
	home    string
	homeSk  map[string]bool // keys of the user folder's skipped folders
	partial []string        // first-level folders not finished, for previous
}

// Search runs one search. It never fails: what it could not read is counted.
func Search(o Options) Result {
	if o.FS == nil {
		o.FS = OS()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.GOOS == "" {
		o.GOOS = goos
	}
	lim := o.Limit
	if lim.Time == 0 {
		lim.Time = MaxTime
	}
	if lim.Dirs == 0 {
		lim.Dirs = MaxDirs
	}
	if lim.Depth == 0 {
		lim.Depth = MaxDepth
	}
	if lim.Repos == 0 {
		lim.Repos = MaxRepos
	}
	s := &searcher{
		o: o, lim: lim, fold: o.GOOS == "windows" || o.GOOS == "darwin", now: o.Now, status: StatusComplete,
		skipped: map[Reason]*NotSearched{}, seen: map[string]bool{}, homeSk: map[string]bool{},
	}
	s.start = s.now()
	if o.Home != "" {
		s.home = filepath.Clean(o.Home)
		for _, h := range homeSkips(o.GOOS) {
			s.homeSk[s.key(filepath.Join(s.home, h))] = true
		}
	}
	for _, p := range o.Skip {
		if p != "" {
			s.skip = append(s.skip, filepath.Clean(p))
		}
	}

	var listed []Item
	var roots []walkRoot
	if o.SearchHome && s.home != "" {
		roots = append(roots, walkRoot{path: s.home, source: SourceHome, isHome: true})
	}
	for _, r := range o.Roots {
		if r == "" {
			continue
		}
		p := filepath.Clean(r)
		if !s.network(r) {
			if abs, err := filepath.Abs(p); err == nil {
				p = abs
			}
		}
		if s.network(p) || s.network(r) {
			// A listed root on a network drive is not searched below; it is
			// kept as today, the repository the person listed (CTO-38 §1.3).
			s.count(ReasonNetwork, p)
			listed = s.addListed(listed, p)
			continue
		}
		if _, err := o.FS.Lstat(filepath.Join(p, ".git")); err == nil {
			listed = s.addListed(listed, p) // a repository: that repository, as today
			continue
		}
		info, err := o.FS.Lstat(p)
		if err != nil {
			s.count(ReasonUnreadable, p)
			continue
		}
		if isLink(info) {
			// A root that is a link is resolved once (CTO-38 §1.1).
			real, err := o.FS.EvalSymlinks(p)
			if err != nil {
				s.count(ReasonUnreadable, p)
				continue
			}
			p = real
			if _, err := o.FS.Lstat(filepath.Join(p, ".git")); err == nil {
				listed = s.addListed(listed, p)
				continue
			}
		}
		roots = append(roots, walkRoot{path: p, source: SourceRoot})
	}
	roots = s.dedupeRoots(roots)

	for i, r := range roots {
		if s.status != StatusComplete {
			s.partial = append(s.partial, r.path)
			s.count(ReasonIncomplete, r.path)
			continue
		}
		s.walkRoot(r)
		if s.status != StatusComplete {
			for _, rest := range roots[i+1:] {
				s.partial = append(s.partial, rest.path)
				s.count(ReasonIncomplete, rest.path)
			}
			break
		}
	}
	for _, in := range o.Indicators {
		s.indicator(in)
	}

	res := Result{Status: s.status, Started: s.start, DirsRead: s.dirs}
	for _, r := range roots {
		res.Roots = append(res.Roots, r.path)
	}
	if len(roots) == 0 && len(listed) == 0 && len(o.Indicators) == 0 && !o.SearchHome {
		res.Status = StatusOff
	}
	// MaxRepos (G7): the most recently used are kept; listed ones always.
	found := s.found
	res.Found = len(found) + len(listed)
	if len(found) > lim.Repos {
		sort.SliceStable(found, func(i, j int) bool {
			return s.activity(found[i].Path).After(s.activity(found[j].Path))
		})
		for _, it := range found[lim.Repos:] {
			s.count(ReasonOverMax, it.Path)
		}
		found = found[:lim.Repos]
	}
	sort.SliceStable(found, func(i, j int) bool { return s.less(found[i].Path, found[j].Path) })
	res.Items = append(listed, found...)
	// A search that did not finish keeps the previous review's repositories
	// that still exist in the part it did not walk (CTO-38 §2.2).
	if !res.Complete() {
		for _, p := range o.Previous {
			k := s.key(filepath.Clean(p))
			if s.seen[k] || !s.inPartial(p) {
				continue
			}
			if _, err := o.FS.Lstat(filepath.Join(p, ".git")); err != nil {
				continue
			}
			s.seen[k] = true
			res.Items = append(res.Items, Item{Path: filepath.Clean(p), Source: SourcePrevious})
		}
	}
	for _, r := range ReasonOrder {
		if n := s.skipped[r]; n != nil {
			res.Skipped = append(res.Skipped, *n)
		}
	}
	res.Duration = s.now().Sub(s.start)
	return res
}

// walkRoot is a folder walked from the top.
type walkRoot struct {
	path   string
	source Source
	isHome bool
}

// dedupeRoots walks a root once: a root inside another one the outer walk
// reaches is dropped (CTO-38 §1.1, «raíces repetidas»).
func (s *searcher) dedupeRoots(roots []walkRoot) []walkRoot {
	var out []walkRoot
	for i, r := range roots {
		dup := false
		for j, o := range roots {
			if i == j {
				continue
			}
			if s.key(r.path) == s.key(o.path) && j < i {
				dup = true
				break
			}
			if s.key(r.path) != s.key(o.path) && s.reaches(o, r.path) {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, r)
		}
	}
	return out
}

// reaches reports whether walking outer would reach inner: inner is below
// outer, within the depth, and no folder on the way is one the walk skips.
func (s *searcher) reaches(outer walkRoot, inner string) bool {
	rel, err := filepath.Rel(outer.path, inner)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	if len(parts) > s.lim.Depth {
		return false
	}
	p := outer.path
	for _, name := range parts {
		p = filepath.Join(p, name)
		if r := s.nameRule(p, name); r != "" {
			return false
		}
	}
	return true
}

// walkRoot walks one root, one first-level folder at a time, so a search that
// stops knows which first-level folders it did not finish.
func (s *searcher) walkRoot(r walkRoot) {
	rootInfo, err := s.o.FS.Lstat(r.path)
	if err != nil {
		s.count(ReasonUnreadable, r.path)
		return
	}
	if s.network(r.path) {
		s.count(ReasonNetwork, r.path)
		return
	}
	entries, err := s.readDir(r.path)
	if errors.Is(err, errStop) {
		s.partial = append(s.partial, r.path)
		s.count(ReasonIncomplete, r.path)
		return
	}
	if err != nil {
		s.count(ReasonUnreadable, r.path)
		return
	}
	if !r.isHome && hasGit(entries) {
		s.addFound(r.path, r.source)
		return
	}
	subs := s.subdirs(r.path, entries, rootInfo)
	for i, sub := range subs {
		err := s.visit(sub, rootInfo, 1, r)
		if errors.Is(err, errStop) {
			for _, rest := range subs[i:] {
				s.partial = append(s.partial, rest)
				s.count(ReasonIncomplete, rest)
			}
			return
		}
	}
}

// visit is one folder at depth (1 = a child of the root): a repository is
// kept and not entered; at the last level only its .git entry is looked up.
func (s *searcher) visit(dir string, rootInfo fs.FileInfo, depth int, r walkRoot) error {
	if depth >= s.lim.Depth {
		// The last level is not opened: a .git entry makes it a repository,
		// anything else is counted as too deep (CTO-38 P5).
		if _, err := s.o.FS.Lstat(filepath.Join(dir, ".git")); err == nil {
			s.addFound(dir, r.source)
		} else {
			s.count(ReasonDepth, "")
		}
		return nil
	}
	entries, err := s.readDir(dir)
	if errors.Is(err, errStop) {
		return err
	}
	if err != nil {
		s.count(ReasonUnreadable, dir)
		return nil
	}
	if hasGit(entries) {
		s.addFound(dir, r.source)
		return nil // stop at the repository: nothing inside is read
	}
	for _, sub := range s.subdirs(dir, entries, rootInfo) {
		if err := s.visit(sub, rootInfo, depth+1, r); err != nil {
			return err
		}
	}
	return nil
}

// subdirs are the folders of entries the walk enters, in name order; every
// folder it does not enter is counted with its reason.
func (s *searcher) subdirs(dir string, entries []fs.DirEntry, rootInfo fs.FileInfo) []string {
	var out []string
	for _, e := range entries {
		name := e.Name()
		p := filepath.Join(dir, name)
		if e.Type()&fs.ModeSymlink != 0 {
			s.count(ReasonLinks, p)
			continue
		}
		if !e.IsDir() && e.Type()&fs.ModeIrregular == 0 {
			continue // a file: never opened
		}
		if r := s.nameRule(p, name); r != "" {
			s.count(r, p)
			continue
		}
		info, err := s.o.FS.Info(dir, e)
		if err != nil {
			s.count(ReasonUnreadable, p)
			continue
		}
		if r := classify(info, rootInfo); r != "" {
			s.count(r, p)
			continue
		}
		if !info.IsDir() {
			continue
		}
		out = append(out, p)
	}
	return out
}

// nameRule is the rule a folder's path and name break, or "": the forced
// exclusions, hidden folders, heavy folders, macOS packages, the user
// folder's system folders, skip_folders and the system folders.
func (s *searcher) nameRule(p, name string) Reason {
	switch {
	case repos.IsForcedExclusion(name) || heavyFolders[name]:
		return ReasonForced
	case strings.HasPrefix(name, "."):
		return ReasonHidden
	}
	if s.o.GOOS == "darwin" {
		for _, ext := range macPackages {
			if strings.HasSuffix(name, ext) {
				return ReasonSystem
			}
		}
	}
	if s.homeSk[s.key(p)] {
		return ReasonSystem
	}
	for _, d := range s.o.SystemDirs {
		if d != "" && s.under(p, d) {
			return ReasonSystem
		}
	}
	for _, d := range s.skip {
		if s.under(p, d) {
			return ReasonUserSkip
		}
	}
	return ""
}

// readDir lists dir, counting it against MaxDirs and checking MaxTime first.
func (s *searcher) readDir(dir string) ([]fs.DirEntry, error) {
	if s.dirs >= s.lim.Dirs {
		s.status = StatusDirLimit
		return nil, errStop
	}
	if s.now().Sub(s.start) >= s.lim.Time {
		s.status = StatusTimeLimit
		return nil, errStop
	}
	s.dirs++
	return s.o.FS.ReadDir(dir)
}

// indicator accepts a repository another tool names when it passes the
// filters (CTO-38 §1.1, point 3).
func (s *searcher) indicator(in Indicator) {
	p := filepath.Clean(in.Path)
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if s.seen[s.key(p)] {
		return
	}
	if s.home != "" && s.key(p) == s.key(s.home) || filepath.Dir(p) == p {
		return // the user folder or a drive root is never a project
	}
	if s.network(p) {
		return
	}
	for _, part := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if repos.IsForcedExclusion(part) || heavyFolders[part] {
			return
		}
	}
	rel, err := filepath.Rel(s.home, p)
	if s.home != "" && err == nil && !strings.HasPrefix(rel, "..") {
		q := s.home
		for _, name := range strings.Split(rel, string(filepath.Separator)) {
			q = filepath.Join(q, name)
			if r := s.nameRule(q, name); r != "" {
				return
			}
		}
	} else {
		for _, d := range s.o.SystemDirs {
			if d != "" && s.under(p, d) {
				return
			}
		}
	}
	info, err := s.o.FS.Lstat(p)
	if err != nil || !info.IsDir() {
		return
	}
	if _, err := s.o.FS.Lstat(filepath.Join(p, ".git")); err != nil {
		return
	}
	s.addFound(p, in.Source)
}

func (s *searcher) addFound(p string, src Source) {
	k := s.key(p)
	if s.seen[k] {
		return
	}
	s.seen[k] = true
	s.found = append(s.found, Item{Path: p, Source: src})
}

func (s *searcher) addListed(listed []Item, p string) []Item {
	k := s.key(p)
	if s.seen[k] {
		return listed
	}
	s.seen[k] = true
	return append(listed, Item{Path: p, Source: SourceListed})
}

// count records one place not searched.
func (s *searcher) count(r Reason, p string) {
	n := s.skipped[r]
	if n == nil {
		n = &NotSearched{Reason: r}
		s.skipped[r] = n
	}
	n.Count++
	if p != "" && pathReasons[r] && len(n.Paths) < MaxPaths {
		n.Paths = append(n.Paths, p)
	}
}

// activity is a repository's most recent use: the modification time of
// .git/index, else .git/HEAD, else the .git entry (an Lstat; nothing opened).
func (s *searcher) activity(repo string) time.Time {
	for _, p := range []string{filepath.Join(repo, ".git", "index"), filepath.Join(repo, ".git", "HEAD"), filepath.Join(repo, ".git")} {
		if info, err := s.o.FS.Lstat(p); err == nil {
			return info.ModTime()
		}
	}
	return time.Time{}
}

// inPartial reports whether p lies in a folder the search did not finish.
func (s *searcher) inPartial(p string) bool {
	for _, d := range s.partial {
		if s.under(filepath.Clean(p), d) {
			return true
		}
	}
	return false
}

// network reports a path the search must not read below: a UNC path or a
// network drive on Windows, a network file system elsewhere.
func (s *searcher) network(p string) bool {
	if s.o.GOOS == "windows" && (strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//")) {
		return true
	}
	return s.o.FS.Network(p)
}

func (s *searcher) key(p string) string {
	if s.fold {
		return strings.ToLower(p)
	}
	return p
}

func (s *searcher) less(a, b string) bool {
	if ka, kb := strings.ToLower(a), strings.ToLower(b); ka != kb {
		return ka < kb
	}
	return a < b
}

// under reports p inside dir or dir itself.
func (s *searcher) under(p, dir string) bool {
	if dir == "" {
		return false
	}
	kp, kd := s.key(filepath.Clean(p)), s.key(filepath.Clean(dir))
	return kp == kd || strings.HasPrefix(kp, strings.TrimRight(kd, `/\`)+string(filepath.Separator))
}

func hasGit(entries []fs.DirEntry) bool {
	for _, e := range entries {
		if e.Name() == ".git" {
			return true
		}
	}
	return false
}

func isLink(info fs.FileInfo) bool { return info.Mode()&fs.ModeSymlink != 0 }

// Diff is how many repositories of res are new against the previous review's
// and how many of the previous review's are no longer there (CTO-38 §2.1). A
// first review (no previous repositories) has none new.
func Diff(res Result, previous []string) (added, gone int) {
	fold := goos == "windows" || goos == "darwin"
	key := func(p string) string {
		p = filepath.Clean(p)
		if fold {
			return strings.ToLower(p)
		}
		return p
	}
	prev := map[string]bool{}
	for _, p := range previous {
		prev[key(p)] = true
	}
	now := map[string]bool{}
	for _, it := range res.Items {
		now[key(it.Path)] = true
		if len(prev) > 0 && !prev[key(it.Path)] {
			added++
		}
	}
	for p := range prev {
		if !now[p] {
			gone++
		}
	}
	return added, gone
}
