package trust_test

// Usage statistics stay off on every test computer (BV-271): a journey, the
// realistic end-to-end run or a CI job that turned them on would send its
// runs to app.rosmarops.com as if a person had made them. At run time,
// journey.py and scripts/e2e/run.ps1 check each test computer's state.json
// and that no usage package was built; this test checks what they are told
// to do: every journey answers «no» to the usage question, and no workflow
// or script turns usage statistics on or marks an installation as the team's.
//
// Go tests are not scanned here. A test that turned usage statistics on by
// mistake did it with a bare Enter (the question's default is yes), which no
// text scan can tell from any other answer. Two checks look at behaviour
// instead: a build that is not a published version never asks or sends
// (cmd_usage.go, usageBuildMaySend), and CI runs every test behind a proxy
// that records where they go and fails on app.rosmarops.com
// (.github/scripts/go-test-recorded.sh). See ADR 0032.

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// usageOn matches a command that turns usage statistics on or sets the team
// mark: `usage on`, `usage team …`, or the same as separate arguments.
var usageOn = regexp.MustCompile(`\busage['",\s]+(on|team)\b`)

func TestUsageOffInJourneys(t *testing.T) {
	root := repoRoot(t)
	steps, err := filepath.Glob(filepath.Join(root, "tests", "journeys", "steps", "*.steps"))
	if err != nil || len(steps) == 0 {
		t.Fatalf("no steps files: %v", err)
	}
	asked := 0
	for _, p := range steps {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 || f[1] != "usage.ask.prompt" {
				continue
			}
			asked++
			if len(f) != 4 || f[0] != "if-asked" || f[2] != "send" || !strings.HasPrefix(strings.ToLower(f[3]), "n") {
				t.Errorf("%s:%d: the usage question must be answered no: %q", filepath.Base(p), i+1, line)
			}
		}
	}
	if asked == 0 {
		t.Error("no steps file answers the usage question: the check would pass for nothing")
	}
}

func TestUsageOffInWorkflowsAndScripts(t *testing.T) {
	root := repoRoot(t)
	var files []string
	for _, dir := range []string{".github", "scripts", filepath.Join("tests", "journeys"), filepath.Join("tests", "e2e")} {
		_ = filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			switch filepath.Ext(p) {
			case ".yml", ".yaml", ".ps1", ".sh", ".py", ".steps":
				files = append(files, p)
			}
			return nil
		})
	}
	if len(files) == 0 {
		t.Fatal("no workflow or script found")
	}
	for _, p := range files {
		raw, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(raw), "\n") {
			if usageOn.MatchString(line) {
				rel, _ := filepath.Rel(root, p)
				t.Errorf("%s:%d turns usage statistics on or marks the team: %q", rel, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// The pattern itself: what it must and must not catch.
func TestUsageOnPattern(t *testing.T) {
	for _, s := range []string{`rosmarops usage on`, `& $exe usage team on`, `["usage", "on"]`, `@('usage','team','off')`} {
		if !usageOn.MatchString(s) {
			t.Errorf("not caught: %s", s)
		}
	}
	for _, s := range []string{`rosmarops usage off`, `("usage", ["usage"], "ask")`, `echo "usage: conpty -steps FILE"`, `if-asked usage.ask.prompt send n\r`} {
		if usageOn.MatchString(s) {
			t.Errorf("caught by mistake: %s", s)
		}
	}
}
