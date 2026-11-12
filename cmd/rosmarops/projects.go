package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/firstrun"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/projects"
	"github.com/qtorb/rosmarops/internal/state"
)

// searchProjects is the review's project search (CTO-38): in every review,
// with no cache, over the user folder (search_home), the folders of
// repo_roots and the indicators, with the previous review's repositories for
// a search that does not finish. Tests replace it.
var searchProjects = func(cfg config.Config, st state.State) projects.Result {
	env := firstrun.EnvFromOS()
	o := env.Options(cfg.RepoRoots)
	o.SearchHome = cfg.SearchHomeOn()
	o.Skip = cfg.SkipFolders
	o.Previous = previousRepos(st)
	return projects.Search(o)
}

// previousRepos are the previous review's repositories (st.Baselines.Repos).
func previousRepos(st state.State) []string {
	out := make([]string, 0, len(st.Baselines.Repos))
	for _, b := range st.Baselines.Repos {
		out = append(out, b.Path)
	}
	return out
}

// maxConsoleNames is the most project names the console lists (CTO-38 §4.1).
const maxConsoleNames = 20

// printProjects writes the search's console lines (CTO-38 §4.1): how many
// projects in how many folders, their folder names (never a path), how many
// places it did not look in, and whether it reached its limit.
func printProjects(w io.Writer, cat catalog.Catalog, res projects.Result, width int) {
	n := len(res.Items)
	if n == 0 {
		fmt.Fprintln(w, cat.Text("console.projects.none", nil))
	} else {
		if n == 1 {
			fmt.Fprintln(w, cat.Text("console.projects.found.one", nil))
		} else {
			fmt.Fprintln(w, cat.Text("console.projects.found.other", map[string]string{
				"count": strconv.Itoa(n), "roots": strconv.Itoa(projectRoots(res)),
			}))
		}
		names := consoleNames(res.Repos())
		line, shown := namesLine(names, width)
		fmt.Fprintln(w, cat.Text("console.projects.list", map[string]string{"names": line}))
		if rest := len(names) - shown; rest > 0 {
			fmt.Fprintln(w, cat.Text("console.projects.more", map[string]string{"count": strconv.Itoa(rest)}))
		}
	}
	if t := res.NotSearchedTotal(); t > 0 {
		fmt.Fprintln(w, cat.Text("console.projects.not_searched", map[string]string{"count": strconv.Itoa(t)}))
	}
	if !res.Complete() {
		fmt.Fprintln(w, cat.Text("console.projects.incomplete", nil))
	}
}

// projectRoots is the {roots} of the summary: the folders walked, at least
// one when anything was found.
func projectRoots(res projects.Result) int {
	if len(res.Roots) == 0 && len(res.Items) > 0 {
		return 1
	}
	return len(res.Roots)
}

// consoleNames are the repositories' folder names; two that are the same
// show their parent too (padre/nombre), and so on up while they still repeat
// (CTO-38 §4.1, P14).
func consoleNames(paths []string) []string {
	parts := make([][]string, len(paths))
	depth := make([]int, len(paths))
	for i, p := range paths {
		parts[i] = strings.FieldsFunc(filepath.ToSlash(filepath.Clean(p)), func(r rune) bool { return r == '/' })
		depth[i] = 1
	}
	name := func(i int) string {
		ps := parts[i]
		d := depth[i]
		if d > len(ps) {
			d = len(ps)
		}
		return strings.Join(ps[len(ps)-d:], "/")
	}
	for changed := true; changed; {
		changed = false
		groups := map[string][]int{}
		for i := range paths {
			groups[strings.ToLower(name(i))] = append(groups[strings.ToLower(name(i))], i)
		}
		for _, g := range groups {
			if len(g) < 2 {
				continue
			}
			for _, i := range g {
				if depth[i] < len(parts[i]) {
					depth[i]++
					changed = true
				}
			}
		}
	}
	out := make([]string, len(paths))
	for i := range paths {
		out[i] = name(i)
	}
	return out
}

// namesLine joins as many names as fit in width (at most maxConsoleNames) and
// returns the line and how many it holds.
func namesLine(names []string, width int) (string, int) {
	if width <= 0 {
		width = 80
	}
	var b strings.Builder
	shown := 0
	for _, n := range names {
		if shown == maxConsoleNames {
			break
		}
		add := n
		if shown > 0 {
			add = ", " + n
		}
		if shown > 0 && len([]rune(b.String()+add)) > width {
			break
		}
		b.WriteString(add)
		shown++
	}
	return b.String(), shown
}

// consoleWidth is the terminal's width when the shell says it (COLUMNS),
// else 80.
func consoleWidth() int {
	if n, err := strconv.Atoi(os.Getenv("COLUMNS")); err == nil && n > 20 {
		return n
	}
	return 80
}

// projectSearchFinding is the yellow project_search_incomplete finding of a
// search that reached its limit (CTO-38 §2.2, invariant 4: it prevents green).
func projectSearchFinding(cat catalog.Catalog, res projects.Result) finding.Finding {
	f := finding.NewFinding("project_search_incomplete:"+string(res.Status), finding.SeverityYellow,
		"project_search_incomplete", cat.Text("finding.project_search_incomplete.title", nil))
	f.Evidence = finding.Evidence{Extra: map[string]string{"search": string(res.Status), "dirs_read": strconv.Itoa(res.DirsRead)}}
	return f
}

// projectsCoverage is the search for report.v2's coverage.projects (G4):
// paths as ~/… under the user folder, new and gone against the previous
// review's repositories.
func projectsCoverage(res projects.Result, previous []string, home string) *report.ProjectsCoverage {
	tilde := func(p string) string { return report.TildePath(p, home) }
	pc := &report.ProjectsCoverage{
		Search: string(res.Status), Found: res.Found, Scanned: len(res.Items),
		Roots: []string{}, Items: []report.ProjectItem{}, NotSearched: []report.ProjectNotSearched{},
	}
	for _, r := range res.Roots {
		pc.Roots = append(pc.Roots, tilde(r))
	}
	for _, it := range res.Items {
		pc.Items = append(pc.Items, report.ProjectItem{Path: tilde(it.Path), Source: string(it.Source)})
	}
	for _, n := range res.Skipped {
		ns := report.ProjectNotSearched{Reason: string(n.Reason), Count: n.Count}
		for _, p := range n.Paths {
			ns.Paths = append(ns.Paths, tilde(p))
		}
		pc.NotSearched = append(pc.NotSearched, ns)
	}
	pc.New, pc.Gone = projects.Diff(res, previous)
	return pc
}
