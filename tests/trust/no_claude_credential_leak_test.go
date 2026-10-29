package trust_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeClaudeOAuthToken and fakeClaudeRefreshToken are synthetic Claude Code
// account OAuth tokens, read by exact key from ~/.claude/.credentials.json
// (BV-111, point 6). They carry no real pattern, so they are kept as opaque
// account credentials; no file the run writes may hold them, or their middle.
const (
	fakeClaudeOAuthToken   = "sk-ant-oat01-LEAK" + "oooooooooooooooooooooooooooooooooooooooo" + "LEAK"
	fakeClaudeRefreshToken = "sk-ant-ort01-LEAK" + "rrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrrr" + "LEAK"
)

// TestNoClaudeCredentialSecretLeak is BV-111, point 6 and test (d): the full
// run reads Claude Code's .credentials.json and keeps a credential node for
// it (type oauth_token, service Anthropic), yet no file it writes — the
// graph, the report, the Markdown, the export for your AI or the logs —
// holds either token or its middle.
func TestNoClaudeCredentialSecretLeak(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	s := newSandbox(t)
	s.runAll(t, nil)
	dir := filepath.Join(s.home, ".rosmarops")

	graphRaw, err := os.ReadFile(filepath.Join(dir, "reports", "graph.v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Vacuity: the credential was read and kept as an Anthropic OAuth token.
	if !strings.Contains(string(graphRaw), `"type": "oauth_token"`) || !strings.Contains(string(graphRaw), `"service": "anthropic"`) {
		t.Fatal("no Anthropic oauth_token credential in the graph: the .credentials.json was not read")
	}

	err = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, v := range []string{fakeClaudeOAuthToken, fakeClaudeRefreshToken} {
			if strings.Contains(string(raw), v) || strings.Contains(string(raw), v[4:len(v)-4]) {
				t.Errorf("%s holds a Claude Code account token", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
