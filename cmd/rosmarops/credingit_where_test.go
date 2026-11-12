package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
)

// Fake credentials for BV-219: shaped like an AWS access key id and a
// GitHub classic token, never valid anywhere.
const (
	bv219AWSKey = "AKIAFAKEFAKEFAKE1234"
	bv219Token  = "ghp_FAKEfakeFAKEfakeFAKEfakeFAKEfake0000"
)

// bv219Run makes a git repository "proj" under a temp home, lets setup add
// its commits, runs a real scheduled review over it in Spanish and returns
// the Spanish page and report.v2.json.
func bv219Run(t *testing.T, setup func(git func(...string) string, write func(name, content string))) (page, v2 string) {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git absent")
	}
	home := homeEnv(t)
	repo := filepath.Join(home, "proj")
	if err := os.MkdirAll(repo, 0o700); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(git, append([]string{"-c", "user.email=t@example.invalid", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	setup(run, write)

	dir := filepath.Join(home, ".rosmarops")
	reports := filepath.Join(dir, "reports")
	if err := os.MkdirAll(reports, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath, stPath := filepath.Join(dir, "config.json"), filepath.Join(dir, "state.json")
	cfg := config.DefaultConfig()
	cfg.ReportDirectory = reports
	cfg.RepoRoots = []string{repo}
	if err := config.WriteConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
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
	if err := state.SaveState(stPath, st); err != nil {
		t.Fatal(err)
	}
	if err := writeAcceptedCopy(reports, raw); err != nil {
		t.Fatal(err)
	}
	if _, err := runWith("es", runMode{unattended: true}); err != nil {
		t.Fatalf("run: %v", err)
	}
	read := func(p string) string {
		t.Helper()
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	page = read(filepath.Join(reports, report.LangFileName("es")))
	v2 = read(filepath.Join(report.DataDir(reports), "report.v2.json"))
	for _, secret := range []string{bv219AWSKey, bv219Token} {
		if strings.Contains(page, secret) || strings.Contains(v2, secret) {
			t.Fatal("a fake secret's full value is in the report")
		}
	}
	return page, v2
}

// bv219Findings counts the cred_in_git findings of report.v2.json.
func bv219Findings(v2 string) int {
	return len(regexp.MustCompile(`"rule":\s*"cred_in_git"`).FindAllString(v2, -1))
}

// bv219Where is how many times the page shows a «where» line: step 3's row
// of the second key (the first is step 2's).
const bv219Where = 1

// bv219Env is the .env both keys are in.
const bv219Env = "AWS_ACCESS_KEY_ID=" + bv219AWSKey + "\nGITHUB_TOKEN=" + bv219Token + "\n"

// BV-219, case 1: a .env that .gitignore ignores and that was never
// committed gives no cred_in_git.
func TestCredInGitWhere_IgnoredNeverCommitted(t *testing.T) {
	_, v2 := bv219Run(t, func(git func(...string) string, write func(string, string)) {
		write(".gitignore", ".env\n")
		write(".env", bv219Env)
		git("add", ".gitignore")
		git("commit", "-q", "-m", "ignore env")
	})
	if n := bv219Findings(v2); n != 0 {
		t.Errorf("an ignored, never committed .env gave %d cred_in_git findings", n)
	}
}

// BV-219, case 2: a committed .env that git tracks: the title says it is
// saved in git, the «where» line names the tracked file and the project.
func TestCredInGitWhere_Tracked(t *testing.T) {
	page, v2 := bv219Run(t, func(git func(...string) string, write func(string, string)) {
		write(".env", bv219Env)
		git("add", ".env")
		git("commit", "-q", "-m", "env")
	})
	if n := bv219Findings(v2); n != 2 {
		t.Fatalf("%d cred_in_git findings for a committed .env, want 2 (one per key)", n)
	}
	if !strings.Contains(page, "está guardada en git") || strings.Contains(page, "está en tu historial de git") {
		t.Error("a tracked key's title does not say it is saved in git")
	}
	want := `<p class="where">En <code>.env</code>, que git sigue, en el proyecto ~` + string(filepath.Separator) + `proj.`
	if n := strings.Count(page, want); n != bv219Where {
		t.Errorf("the «where» line of a tracked key shows %d times, want %d: %q", n, bv219Where, want)
	}
	if strings.Contains(page, "la añadió en") {
		t.Error("a tracked key shows the history «where» line")
	}
}

// BV-219, case 3: a .env committed, then untracked and ignored: the title
// says it is in the history, the «where» line names the 7-character commit
// that added it and the file.
func TestCredInGitWhere_HistoryOnly(t *testing.T) {
	var commit string
	page, v2 := bv219Run(t, func(git func(...string) string, write func(string, string)) {
		write(".env", bv219Env)
		git("add", ".env")
		git("commit", "-q", "-m", "env")
		commit = git("rev-parse", "--short=7", "HEAD")
		git("rm", "-q", "--cached", ".env")
		write(".gitignore", ".env\n")
		git("add", ".gitignore")
		git("commit", "-q", "-m", "untrack env")
	})
	if n := bv219Findings(v2); n != 2 {
		t.Fatalf("%d cred_in_git findings for a key only in the history, want 2 (one per key)", n)
	}
	if !strings.Contains(page, "está en tu historial de git") || strings.Contains(page, "está guardada en git") {
		t.Error("a history-only key's title does not say it is in the history")
	}
	if len(commit) != 7 {
		t.Fatalf("short commit %q", commit)
	}
	want := `<p class="where">En el proyecto ~` + string(filepath.Separator) + `proj: el commit <code>` + commit +
		`</code> la añadió en <code>.env</code>. Hoy ese fichero puede estar ignorado o ya no existir, pero la clave sigue en el historial.`
	if n := strings.Count(page, want); n != bv219Where {
		t.Errorf("the «where» line of a history-only key shows %d times, want %d: %q", n, bv219Where, want)
	}
	if strings.Contains(page, "que git sigue") {
		t.Error("a history-only key shows the tracked «where» line")
	}
}
