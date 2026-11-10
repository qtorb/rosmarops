package trust_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProblemConsentFromSendButtonCallers is CTO-32 C1: the live «Send»-button
// consent (spacenet.ConsentFromSendButton) may be constructed from exactly one
// place in production code — the ans.send branch of offerProblems
// (cmd/rosmarops/problemprompt.go). Any other caller would be a way to send a
// problem report without a real button press, so the test fails on it. The
// definition in internal/spacenet and _test.go files are not callers.
func TestProblemConsentFromSendButtonCallers(t *testing.T) {
	root := repoRoot(t)
	const def = "func ConsentFromSendButton"
	const call = "ConsentFromSendButton("
	allowed := filepath.Join("cmd", "rosmarops", "problemprompt.go")

	var offenders []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		for _, line := range strings.Split(string(b), "\n") {
			if !strings.Contains(line, call) || strings.Contains(line, def) {
				continue
			}
			if rel != allowed {
				offenders = append(offenders, rel+": "+strings.TrimSpace(line))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Errorf("ConsentFromSendButton may be called only from %s; found:\n%s", allowed, strings.Join(offenders, "\n"))
	}
}
