package trust_test

// Trust test (BV-226 §2, CTO-44 §4b; CTO-37 T3 applied to statusline.txt):
// Claude Code's status line is on screen all the time, in recordings and in
// shared screens. What a real run leaves in data/statusline.txt, and what
// `rosmarops statusline` prints, carry only counts, the score and the date:
// never a path, a file name, a key, an MCP server, a skill, a project or
// the user's name.

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/agenthooks"
	"github.com/qtorb/rosmarops/internal/report"
)

func TestNoIdentityInStatusline(t *testing.T) {
	s := newSandbox(t)
	s.runAll(t, nil)
	raw, err := os.ReadFile(filepath.Join(s.home, ".rosmarops", "reports", report.DataSubdir, agenthooks.StatuslineFile))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(s.binary, "statusline")
	cmd.Dir, cmd.Env = s.work, s.env
	shown, err := cmd.Output()
	if err != nil {
		t.Fatalf("statusline: %v", err)
	}
	if len(strings.TrimSpace(string(shown))) == 0 {
		t.Fatal("statusline printed nothing: test would be vacuous")
	}
	forbidden := []string{s.home, s.repo, s.root, "demo-repo", "demo", "server.js", ".json", ".env", "claude", "cursor", "codex", "hermes", "src", "home"}
	if u, err := user.Current(); err == nil && len(u.Username) > 2 {
		forbidden = append(forbidden, u.Username)
	}
	for name, text := range map[string]string{"statusline.txt": string(raw), "statusline output": string(shown)} {
		low := strings.ToLower(text)
		for _, f := range forbidden {
			if strings.Contains(low, strings.ToLower(f)) {
				t.Errorf("%s contains %q: %q", name, f, text)
			}
		}
		// No path separator but the score's «/100» (and the RFC 3339 date in
		// the file, which has none).
		if strings.ContainsAny(strings.ReplaceAll(text, "/100", ""), `/\`) {
			t.Errorf("%s has a path separator: %q", name, text)
		}
	}
	// Each line of the file: language, date, then only the line's own words.
	line := regexp.MustCompile(`^(en|es)\t\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ\tRosmarOps · [^\t]+$`)
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if !line.MatchString(l) {
			t.Errorf("unexpected line in statusline.txt: %q", l)
		}
	}
}
