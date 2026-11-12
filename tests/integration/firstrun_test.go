package integration_test

import (
	"encoding/json"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// firstRunEnv builds the binary and a fake user folder, with the Windows
// folders of U3 inside the sandbox root (decision 2026-10-27, primer uso con
// un solo .exe, U9).
func firstRunEnv(t *testing.T) (bin, root, home string, env []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("black-box run is implemented for Unix-like hosts")
	}
	bin = filepath.Join(t.TempDir(), "rosmarops")
	build := buildCmd(t, bin, "")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	root = t.TempDir()
	home = filepath.Join(root, "home")
	vars := map[string]string{
		"HOME": home, "USERPROFILE": home,
		"APPDATA": filepath.Join(home, "AppData", "Roaming"), "LOCALAPPDATA": filepath.Join(home, "AppData", "Local"),
		"ProgramFiles": filepath.Join(root, "Program Files"), "ProgramFiles(x86)": filepath.Join(root, "Program Files (x86)"),
		"SystemRoot":      filepath.Join(root, "Windows"),
		"XDG_CONFIG_HOME": filepath.Join(root, "xdg"), "XDG_CACHE_HOME": filepath.Join(root, "xdgc"), "TMPDIR": filepath.Join(root, "tmp"),
	}
	env = []string{"PATH=" + os.Getenv("PATH")}
	for k, v := range vars {
		mkdir(t, v)
		env = append(env, k+"="+v)
	}
	mkdir(t, filepath.Join(root, "work"))
	return bin, root, home, env
}

func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, p, content string) {
	t.Helper()
	mkdir(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// repo is a folder with a .git entry.
func repo(t *testing.T, p string) string {
	t.Helper()
	mkdir(t, filepath.Join(p, ".git"))
	return p
}

// doubleClick runs the binary with no arguments from dir, with no console.
func doubleClick(t *testing.T, bin, dir string, env []string) (int, string) {
	t.Helper()
	cmd := exec.Command(bin)
	cmd.Dir, cmd.Env = dir, env
	out, err := cmd.CombinedOutput()
	if exitErr, ok := err.(*exec.ExitError); ok {
		return exitErr.ExitCode(), string(out)
	} else if err != nil {
		t.Fatal(err)
	}
	return 0, string(out)
}

func catalogOf(t *testing.T, lang string) map[string]string {
	t.Helper()
	m := map[string]string{}
	readJSON(t, filepath.Join("..", "..", "internal", "catalog", "messages", lang+".json"), &m)
	return m
}

// U1–U3, U5, U7: a double click on first use finds the projects of each
// source, by U3's rules, initializes with them, runs and writes the five
// pages; the window says what it does; the log keeps every decision.
func TestDoubleClickFirstUse(t *testing.T) {
	bin, root, home, env := firstRunEnv(t)
	appData := filepath.Join(home, "AppData", "Roaming")
	work := filepath.Join(home, "work")
	alpha := repo(t, filepath.Join(work, "alpha"))
	mkdir(t, filepath.Join(work, "beta")) // no .git
	repo(t, home)                         // the user's folder itself has a .git
	inAppData := repo(t, filepath.Join(appData, "tool", "proj"))
	inProgramFiles := repo(t, filepath.Join(root, "Program Files", "app"))
	inWindows := repo(t, filepath.Join(root, "Windows", "src"))
	excluded := repo(t, filepath.Join(work, "node_modules", "pkg"))
	gamma := repo(t, filepath.Join(work, "gamma"))
	delta := repo(t, filepath.Join(work, "délta"))
	one := repo(t, filepath.Join(home, "src", "one"))
	two := repo(t, filepath.Join(home, "Documents", "GitHub", "two"))
	mkdir(t, filepath.Join(home, "Desktop", "notes")) // no .git: ReposUnder skips it
	projects := map[string]any{}
	for _, p := range []string{alpha, filepath.Join(work, "beta"), filepath.Join(home, "missing"), home, inAppData, inProgramFiles, inWindows, excluded} {
		projects[p] = map[string]any{"hasTrustDialogAccepted": true}
	}
	b, _ := json.Marshal(map[string]any{"projects": projects, "history": []string{"not read"}})
	write(t, filepath.Join(home, ".claude.json"), string(b))
	uri := func(p string) string {
		return "file://" + strings.ReplaceAll(strings.ReplaceAll(p, " ", "%20"), "é", "%C3%A9")
	}
	for name, folder := range map[string]string{
		"Cursor/User/workspaceStorage/h1": uri(gamma),
		"Cursor/User/workspaceStorage/h2": "vscode-remote://wsl%2Bubuntu/home/ana/x",
		"Cursor/User/workspaceStorage/h3": "file://server/share/x",
		"Code/User/workspaceStorage/h4":   uri(alpha),
		"Code/User/workspaceStorage/h5":   uri(delta),
	} {
		write(t, filepath.Join(appData, filepath.FromSlash(name), "workspace.json"), `{"folder": "`+folder+`"}`)
	}
	write(t, filepath.Join(appData, "Code", "User", "workspaceStorage", "h6", "workspace.json"), `{"workspace": "file:///x.code-workspace"}`)
	write(t, filepath.Join(appData, "Code", "User", "workspaceStorage", "h6", "state.vscdb"), "SQLite format 3\x00")

	// U2: a configuration planted in the working directory is not read.
	write(t, filepath.Join(root, "work", "rosmarops.config.json"), "{broken")
	code, out := doubleClick(t, bin, filepath.Join(root, "work"), env)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	en := catalogOf(t, "en")
	// On macOS the report opens (buildCmd's fake open): the window says so
	// and does not ask for Enter (BV-102, point 1). Elsewhere here, no
	// browser: the path and «Press Enter».
	opened := runtime.GOOS == "darwin"
	keys := []string{"console.first_run.detecting", "console.checking", "console.press_enter"}
	if opened {
		keys = []string{"console.first_run.detecting", "console.checking", "console.report_opened"}
		if strings.Contains(out, en["console.press_enter"]) {
			t.Errorf("window asks for Enter after opening the report:\n%s", out)
		}
	}
	for _, k := range keys {
		if !strings.Contains(out, en[k]) {
			t.Errorf("window lacks %s:\n%s", k, out)
		}
	}
	// CTO-38: the project search of the review finds the five, walking the
	// user folder and accepting the indicators; the window lists them.
	found := strings.NewReplacer("{count}", "5", "{roots}", "1").Replace(en["console.projects.found.other"])
	if !strings.Contains(out, found) {
		t.Errorf("window: not %q:\n%s", found, out)
	}
	reports := filepath.Join(home, ".rosmarops", "reports")
	if want := strings.ReplaceAll(en["console.report_not_opened"], "{path}", filepath.Join(reports, "report.html")); !opened && !strings.Contains(out, want) {
		t.Errorf("window lacks %q (no browser here):\n%s", want, out)
	}

	// CTO-38 §2.3: the first configuration has no repositories in repo_roots
	// (every review searches again).
	var cfg struct {
		RepoRoots []string `json:"repo_roots"`
	}
	readJSON(t, filepath.Join(home, ".rosmarops", "config.json"), &cfg)
	if len(cfg.RepoRoots) != 0 {
		t.Errorf("repo_roots %q, want empty", cfg.RepoRoots)
	}
	var v2 struct {
		Coverage struct {
			Projects struct {
				Items []struct {
					Path   string `json:"path"`
					Source string `json:"source"`
				} `json:"items"`
			} `json:"projects"`
		} `json:"coverage"`
	}
	readJSON(t, filepath.Join(reports, "data", "report.v2.json"), &v2)
	var gotItems []string
	for _, it := range v2.Coverage.Projects.Items {
		gotItems = append(gotItems, it.Path)
	}
	sort.Strings(gotItems)
	var want []string
	for _, p := range []string{alpha, delta, gamma, one, two} {
		rel, _ := filepath.Rel(home, p)
		want = append(want, "~/"+filepath.ToSlash(rel))
	}
	sort.Strings(want)
	if strings.Join(gotItems, "|") != strings.Join(want, "|") {
		t.Errorf("projects checked\n got %q\nwant %q", gotItems, want)
	}
	log, err := os.ReadFile(filepath.Join(reports, "data", "last-run-log.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), "not read") || strings.Contains(string(log), "state.vscdb") {
		t.Error("the log has what the search must not read")
	}
	for _, p := range []string{inAppData, inProgramFiles, inWindows, excluded} {
		rel, _ := filepath.Rel(home, p)
		for _, it := range gotItems {
			if it == p || it == "~/"+filepath.ToSlash(rel) {
				t.Errorf("%s was checked", p)
			}
		}
	}

	// U7: five pages, report.html in the run's language (en here: no
	// translation outside Windows), each linking to the other language's
	// dated page, which exists.
	checkPages(t, reports, "en")
}

// checkPages checks U7 and §19 on the pages of the reports directory: the
// two dated pages and report.<lang>.html for each language, and report.html
// in runLang; each with a link to the other language's dated page, which
// exists, with its own catalog's text.
func checkPages(t *testing.T, reports, runLang string) {
	t.Helper()
	// BV-183: only the three fixed pages are written, with no dated copies.
	if dated, _ := filepath.Glob(filepath.Join(reports, "report-*.html")); len(dated) != 0 {
		t.Errorf("dated HTML pages still written: %v", dated)
	}
	other := map[string]string{"en": "es", "es": "en"}
	pages := map[string]string{"report.en.html": "en", "report.es.html": "es", "report.html": runLang}
	for name, lang := range pages {
		raw, err := os.ReadFile(filepath.Join(reports, name))
		if err != nil {
			t.Fatal(err)
		}
		page := string(raw)
		if !strings.Contains(page, `<html lang="`+lang+`">`) {
			t.Errorf("%s: not in %s", name, lang)
		}
		o := other[lang]
		link := `<a class="lang" href="report.` + o + `.html" hreflang="` + o + `" lang="` + o + `">` + html.EscapeString(catalogOf(t, lang)["html.lang_switch"]) + `</a>`
		if !strings.Contains(page, link) {
			t.Errorf("%s: no link %s", name, link)
		}
		if _, err := os.Stat(filepath.Join(reports, "report."+o+".html")); err != nil {
			t.Errorf("%s: link to a missing page", name)
		}
	}
	run, _ := os.ReadFile(filepath.Join(reports, "report.html"))
	same, _ := os.ReadFile(filepath.Join(reports, "report."+runLang+".html"))
	if string(run) != string(same) {
		t.Errorf("report.html is not report.%s.html", runLang)
	}
}

// U7: --lang wins: run --lang es makes report.html the Spanish page.
func TestPagesWithLang(t *testing.T) {
	bin, root, home, env := firstRunEnv(t)
	if code, out := doubleClick(t, bin, filepath.Join(root, "work"), env); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	rosmarops(t, bin, filepath.Join(root, "work"), env, "run", "--lang", "es")
	checkPages(t, filepath.Join(home, ".rosmarops", "reports"), "es")
}

// U3: no more than 100 repositories; the rest go to the log with their
// number.
func TestDoubleClickLimit(t *testing.T) {
	bin, root, home, env := firstRunEnv(t)
	for i := 0; i < 105; i++ {
		repo(t, filepath.Join(home, "projects", "r"+strings.Repeat("0", 3-len(itoa(i)))+itoa(i)))
	}
	if code, out := doubleClick(t, bin, filepath.Join(root, "work"), env); code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	// CTO-38 G7: the review checks 100 (the most recently used) and the
	// report lists the other 5 as over the limit.
	var v2 struct {
		Coverage struct {
			Projects struct {
				Scanned     int `json:"scanned"`
				Found       int `json:"found"`
				NotSearched []struct {
					Reason string `json:"reason"`
					Count  int    `json:"count"`
				} `json:"not_searched"`
			} `json:"projects"`
		} `json:"coverage"`
	}
	readJSON(t, filepath.Join(home, ".rosmarops", "reports", "data", "report.v2.json"), &v2)
	p := v2.Coverage.Projects
	over := 0
	for _, n := range p.NotSearched {
		if n.Reason == "over_max" {
			over = n.Count
		}
	}
	if p.Scanned != 100 || p.Found != 105 || over != 5 {
		t.Errorf("scanned %d found %d over_max %d; want 100, 105, 5", p.Scanned, p.Found, over)
	}
}

func itoa(i int) string {
	s := ""
	for {
		s = string(rune('0'+i%10)) + s
		if i /= 10; i == 0 {
			return s
		}
	}
}

// U4: nothing found and no picker here (not on Windows, as if cancelled):
// the init has no repositories and the report has only the agents.
func TestDoubleClickNothingFound(t *testing.T) {
	bin, root, home, env := firstRunEnv(t)
	code, out := doubleClick(t, bin, filepath.Join(root, "work"), env)
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, out)
	}
	en := catalogOf(t, "en")
	for _, k := range []string{"console.first_run.none", "console.first_run.agents_only"} {
		if !strings.Contains(out, en[k]) {
			t.Errorf("window lacks %s:\n%s", k, out)
		}
	}
	var cfg struct {
		RepoRoots []string `json:"repo_roots"`
	}
	readJSON(t, filepath.Join(home, ".rosmarops", "config.json"), &cfg)
	if cfg.RepoRoots == nil || len(cfg.RepoRoots) != 0 {
		t.Errorf("repo_roots %v", cfg.RepoRoots)
	}
	log, _ := os.ReadFile(filepath.Join(home, ".rosmarops", "reports", "data", "last-run-log.txt"))
	// On macOS the picker opens (here, a fake osascript that cancels):
	// only «no folder chosen» (BV-102).
	if (runtime.GOOS != "darwin" && !strings.Contains(string(log), "folder picker not opened")) || !strings.Contains(string(log), "no folder chosen") {
		t.Errorf("the log does not say what happened:\n%s", log)
	}
}

// U1: with a configuration and no state, a double click does not
// initialize again: J1 applies (no console: red), and nothing is fixed.
func TestDoubleClickConfigWithoutState(t *testing.T) {
	bin, root, home, env := firstRunEnv(t)
	cfgPath := filepath.Join(home, ".rosmarops", "config.json")
	cfg := `{"schema_version": 1, "repo_roots": []}`
	write(t, cfgPath, cfg)
	code, out := doubleClick(t, bin, filepath.Join(root, "work"), env)
	if code != 2 {
		t.Fatalf("exit %d, want 2 (J1)\n%s", code, out)
	}
	if raw, _ := os.ReadFile(cfgPath); string(raw) != cfg {
		t.Error("the configuration was rewritten")
	}
	if _, err := os.Stat(filepath.Join(home, ".rosmarops", "state.json")); err == nil {
		t.Error("a state was written")
	}
	if !strings.Contains(out, catalogOf(t, "en")["console.press_enter"]) {
		t.Errorf("window lacks console.press_enter:\n%s", out)
	}
	// The red page in report.html and in each language (U7), with no link.
	reports := filepath.Join(home, ".rosmarops", "reports")
	for _, name := range []string{"report.html", "report.en.html", "report.es.html"} {
		raw, err := os.ReadFile(filepath.Join(reports, name))
		if err != nil || !strings.Contains(string(raw), `<h1 class="hl">`) || strings.Contains(string(raw), `class="lang"`) {
			t.Errorf("%s: not the red page: %v", name, err)
		}
	}
	es, _ := os.ReadFile(filepath.Join(reports, "report.es.html"))
	if !strings.Contains(string(es), html.EscapeString(catalogOf(t, "es")["html.integrity.state_lost"])) {
		t.Error("report.es.html is not in Spanish")
	}
}
