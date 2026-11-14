package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/projects"
	"github.com/qtorb/rosmarops/internal/state"
)

// bv219Repo makes a git repository at p (a .git folder is enough for the
// search; the repository scan treats a folder without git data as unreadable
// git, which is not what these tests look at).
func bv219Repo(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(p, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
}

// bv219Setup writes an accepted configuration and state for a review in home
// and returns the configuration's path and the reports directory.
func bv219Setup(t *testing.T, home string, cfg config.Config) (cfgPath, reports string) {
	t.Helper()
	dir := filepath.Join(home, ".rosmarops")
	reports = filepath.Join(dir, "reports")
	cfgPath = filepath.Join(dir, "config.json")
	if err := os.MkdirAll(reports, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(cfgPath)
	self, err := hash.Self()
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.DefaultState(version)
	if err != nil {
		t.Fatal(err)
	}
	st.Integrity.VigilBinarySHA256 = self
	st.Integrity.ConfigSHA256 = hash.Bytes(raw)
	if err := state.SaveState(filepath.Join(dir, "state.json"), st); err != nil {
		t.Fatal(err)
	}
	if err := writeAcceptedCopy(reports, raw); err != nil {
		t.Fatal(err)
	}
	return cfgPath, reports
}

// bv219Coverage reads coverage.projects of the last review.
func bv219Coverage(t *testing.T, reports string) report.ProjectsCoverage {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(report.DataDir(reports), report.V2FileName))
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Coverage struct {
			Projects *report.ProjectsCoverage `json:"projects"`
		} `json:"coverage"`
	}
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	if v.Coverage.Projects == nil {
		t.Fatal("report.v2.json has no coverage.projects")
	}
	return *v.Coverage.Projects
}

// P1 and P10 (CTO-38): with v0.11.2's configuration (four repositories in
// repo_roots, search_home absent) a review checks the 50 repositories under
// ~/Trabajo, at 2, 3 and 4 levels, with coverage.projects listing them; the
// configuration is never rewritten, so its hash does not change. With
// search_home false the list is exactly repo_roots'.
func TestProjectsRealCaseReview(t *testing.T) {
	home := homeEnv(t)
	var all []string
	for i := 0; i < 50; i++ {
		var p string
		switch {
		case i < 20:
			p = filepath.Join(home, "Trabajo", fmt.Sprintf("solo%02d", i))
		case i < 40:
			p = filepath.Join(home, "Trabajo", fmt.Sprintf("org%d", i%5), fmt.Sprintf("r%02d", i))
		default:
			p = filepath.Join(home, "Trabajo", "org9", "team", fmt.Sprintf("t%02d", i))
		}
		bv219Repo(t, p)
		all = append(all, p)
	}
	bv219Repo(t, filepath.Join(home, ".cache", "x"))
	bv219Repo(t, filepath.Join(home, "Trabajo", "solo00", "node_modules", "y"))

	cfg := config.DefaultConfig()
	cfg.RepoRoots = all[:4]
	cfgPath, reports := bv219Setup(t, home, cfg)
	cfg.ReportDirectory = ""
	before, _ := os.ReadFile(cfgPath)
	if _, err := runWith("en", runMode{unattended: true}); err != nil {
		t.Fatalf("run: %v", err)
	}
	cov := bv219Coverage(t, reports)
	if cov.Search != "complete" || cov.Scanned != 50 || cov.Found != 50 || len(cov.Items) != 50 {
		t.Fatalf("coverage.projects %+v", cov)
	}
	for _, it := range cov.Items {
		if !strings.HasPrefix(it.Path, "~/Trabajo/") {
			t.Errorf("item path %q is not ~/…", it.Path)
		}
		if it.Source != "home" && it.Source != "listed" {
			t.Errorf("item %s: source %s", it.Path, it.Source)
		}
	}
	if after, _ := os.ReadFile(cfgPath); !bytes.Equal(before, after) {
		t.Error("the review rewrote config.json (its hash would change)")
	}

	// search_home: false → exactly repo_roots' repositories, as v0.11.2.
	off := false
	cfg.SearchHome = &off
	res := searchProjects(cfg, state.State{})
	if got := res.Repos(); strings.Join(got, "|") != strings.Join(all[:4], "|") {
		t.Errorf("search_home false: %v", got)
	}
	// A configuration of v0.11.2 (no search_home, no skip_folders) still
	// validates and reads as search_home on.
	if loaded, err := config.LoadConfig(cfgPath); err != nil || !loaded.SearchHomeOn() {
		t.Errorf("v0.11.2 config: %v, search_home on %v", err, loaded.SearchHomeOn())
	}
}

// P14: the console lists folder names and never a path, the user folder or
// the user name; two repositories with the same name show their parent.
func TestConsoleProjectNamesOnly(t *testing.T) {
	home := filepath.Join(t.TempDir(), "ana-fake")
	res := projects.Result{Status: projects.StatusComplete, Roots: []string{home}, Items: []projects.Item{
		{Path: filepath.Join(home, "Trabajo", "acme", "api"), Source: projects.SourceHome},
		{Path: filepath.Join(home, "Trabajo", "beta", "api"), Source: projects.SourceHome},
		{Path: filepath.Join(home, "code", "web"), Source: projects.SourceHome},
	}, Skipped: []projects.NotSearched{{Reason: projects.ReasonHidden, Count: 3}}}
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		printProjects(&buf, cat, res, 80)
		out := buf.String()
		for _, bad := range []string{home, "ana-fake", string(filepath.Separator) + "Trabajo", "~"} {
			if strings.Contains(out, bad) {
				t.Errorf("%s: the console shows %q:\n%s", lang, bad, out)
			}
		}
		for _, want := range []string{"acme/api", "beta/api", "web", cat.Text("console.projects.found.other.one_folder", map[string]string{"count": "3", "roots": "1"})} {
			if !strings.Contains(out, want) {
				t.Errorf("%s: missing %q:\n%s", lang, want, out)
			}
		}
	}
	// More than 20 names: 20 shown and «and N more».
	var many projects.Result
	for i := 0; i < 25; i++ {
		many.Items = append(many.Items, projects.Item{Path: filepath.Join(home, fmt.Sprintf("p%02d", i))})
	}
	many.Status = projects.StatusComplete
	var buf bytes.Buffer
	printProjects(&buf, enCat(t), many, 1000)
	if !strings.Contains(buf.String(), enCat(t).Text("console.projects.more", map[string]string{"count": "5"})) {
		t.Errorf("no «and 5 more»:\n%s", buf.String())
	}
	// None found.
	buf.Reset()
	printProjects(&buf, enCat(t), projects.Result{Status: projects.StatusComplete}, 80)
	if strings.TrimSpace(buf.String()) != enCat(t).Text("console.projects.none", nil) {
		t.Errorf("none: %q", buf.String())
	}
}

// P7 (review part) and P15: a search that reached its limit gives the yellow
// project_search_incomplete finding (never green) and the incomplete line;
// a repository of the previous review that is gone counts in gone and its
// baseline is retired, a new one counts in new.
func TestProjectsIncompleteAndGone(t *testing.T) {
	home := homeEnv(t)
	kept := filepath.Join(home, "code", "kept")
	added := filepath.Join(home, "code", "added")
	bv219Repo(t, kept)
	bv219Repo(t, added)
	_, reports := bv219Setup(t, home, config.DefaultConfig())
	stPath := filepath.Join(home, ".rosmarops", "state.json")
	st, err := state.LoadState(stPath)
	if err != nil {
		t.Fatal(err)
	}
	gone := filepath.Join(home, "code", "deleted")
	st.Baselines.Repos = []state.RepoBaseline{{Path: kept}, {Path: gone}}
	if err := state.SaveState(stPath, st); err != nil {
		t.Fatal(err)
	}
	if _, err := runWith("en", runMode{unattended: true}); err != nil {
		t.Fatalf("run: %v", err)
	}
	cov := bv219Coverage(t, reports)
	if cov.New != 1 || cov.Gone != 1 {
		t.Errorf("new %d gone %d, want 1 and 1", cov.New, cov.Gone)
	}
	after, _ := state.LoadState(stPath)
	for _, b := range after.Baselines.Repos {
		if b.Path == gone {
			t.Error("the baseline of a repository that is gone was not retired")
		}
	}

	// A search over its time: yellow finding, report not green.
	prev := searchProjects
	t.Cleanup(func() { searchProjects = prev })
	searchProjects = func(cfg config.Config, st state.State) projects.Result {
		r := prev(cfg, st)
		r.Status = projects.StatusTimeLimit
		r.Started = time.Now()
		return r
	}
	if _, err := runWith("en", runMode{unattended: true}); err != nil {
		t.Fatalf("run: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(report.DataDir(reports), "report.json"))
	var v1 struct {
		Status   string `json:"status"`
		Findings []struct {
			Category string `json:"category"`
			Severity string `json:"severity"`
		} `json:"findings"`
	}
	if err := json.Unmarshal(b, &v1); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range v1.Findings {
		if f.Category == "project_search_incomplete" && f.Severity == "yellow" {
			found = true
		}
	}
	if !found || v1.Status == "green" {
		t.Errorf("status %s, findings %+v: want a yellow project_search_incomplete and not green", v1.Status, v1.Findings)
	}
	var buf bytes.Buffer
	printProjects(&buf, enCat(t), searchProjects(config.DefaultConfig(), state.State{}), 80)
	if !strings.Contains(buf.String(), enCat(t).Text("console.projects.incomplete", nil)) {
		t.Errorf("no incomplete line:\n%s", buf.String())
	}
}
