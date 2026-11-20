package windows

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
)

const subagentBodySecret = "sk-ant-api03-BODYSECRETNEVERREAD0123456789"

func writeSubagent(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadClaudeSubagents(t *testing.T) {
	root := t.TempDir()
	claude := filepath.Join(root, "home", ".claude")
	repo := filepath.Join(root, "repo")
	writeSubagent(t, filepath.Join(claude, "agents"), "all-tools.md", "---\nname: all-tools\ndescription: |\n  a: multi-line description\npermissionMode: bypassPermissions\nhooks:\n  PreToolUse:\n    - matcher: Bash\n      hooks:\n        - type: command\n          command: ./x.sh\n        - type: prompt\n          prompt: check\nmcpServers:\n  - github\n  - playwright:\n      type: stdio\n      command: npx\n      args: [\"-y\", \"@playwright/mcp\"]\n---\nYou are a helper. Use "+subagentBodySecret+"\n---\nnot: header\n")
	writeSubagent(t, filepath.Join(claude, "agents"), "readonly.md", "---\nname: readonly\ntools: Read, Grep\npermissionMode: default\n---\nbody\n")
	writeSubagent(t, filepath.Join(claude, "agents"), "notes.txt", "---\ntools:\n---\n")
	writeSubagent(t, filepath.Join(claude, "agents"), "empty-tools.md", "---\ntools:\npermissionMode: acceptEdits\n---\n")
	writeSubagent(t, filepath.Join(claude, "agents"), "no-header.md", "just text "+subagentBodySecret+"\n")
	writeSubagent(t, filepath.Join(claude, "agents"), "open.md", "---\ntools: Read\n")
	writeSubagent(t, filepath.Join(repo, ".claude", "agents"), "reviewer.md", "---\r\ntools: [Read, Bash]\r\npermissionMode: dontAsk\r\nmcpServers:\r\n  local:\r\n    command: node\r\n---\r\nbody\r\n")

	got := map[string]platform.ClaudeSubagent{}
	for _, s := range ReadClaudeSubagents(claude, []string{repo, repo}) {
		got[filepath.Base(s.Path)] = s
		for _, v := range []string{s.Reason, s.PermissionMode, strings.Join(s.InlineMCPServers, ",")} {
			if strings.Contains(v, "BODYSECRET") {
				t.Fatalf("%s: the body reached the scan: %q", s.Path, v)
			}
		}
	}
	if len(got) != 6 {
		t.Fatalf("subagents %v, want the six .md files (the .txt does not count, the repository once)", got)
	}
	a := got["all-tools.md"]
	if a.Status != platform.MCPSourceRead || a.Name != "all-tools" || a.ToolsDeclared || a.PermissionMode != "bypassPermissions" || a.HookCommands != 1 ||
		strings.Join(a.InlineMCPServers, ",") != "playwright" || a.Scope != platform.SubagentUser || a.Repo != "" {
		t.Errorf("all-tools: %+v", a)
	}
	if r := got["readonly.md"]; !r.ToolsDeclared || r.PermissionMode != "default" || r.HookCommands != 0 || len(r.InlineMCPServers) != 0 {
		t.Errorf("readonly: %+v", r)
	}
	if e := got["empty-tools.md"]; e.ToolsDeclared || e.PermissionMode != "acceptEdits" {
		t.Errorf("an empty tools: inherits every tool: %+v", e)
	}
	if n := got["no-header.md"]; n.Status != platform.MCPSourceFailed {
		t.Errorf("no header: %+v", n)
	}
	if o := got["open.md"]; o.Status != platform.MCPSourceFailed {
		t.Errorf("an unclosed header: %+v", o)
	}
	p := got["reviewer.md"]
	if p.Status != platform.MCPSourceRead || !p.ToolsDeclared || p.PermissionMode != "dontAsk" || strings.Join(p.InlineMCPServers, ",") != "local" ||
		p.Scope != platform.SubagentProject || p.Repo != repo {
		t.Errorf("project reviewer: %+v", p)
	}
	for _, mode := range []string{"acceptEdits", "auto", "dontAsk", "bypassPermissions"} {
		if !platform.PermissiveSubagentMode(mode) {
			t.Errorf("%s is permissive (CTO-76 §5)", mode)
		}
	}
	for _, mode := range []string{"", "default", "plan", "manual"} {
		if platform.PermissiveSubagentMode(mode) {
			t.Errorf("%s is not permissive", mode)
		}
	}
}

// The header reader stops at the closing line: a body larger than anything
// it would buffer is not read.
func TestSubagentHeaderStopsAtTheClosingLine(t *testing.T) {
	r := &countingReader{s: "---\ntools: Read\n---\n" + strings.Repeat("x", 1<<20)}
	if _, err := subagentHeader(r); err != nil {
		t.Fatal(err)
	}
	if r.read > 64*1024 {
		t.Errorf("read %d bytes past a 20-byte header", r.read)
	}
}

type countingReader struct {
	s    string
	read int
}

func (c *countingReader) Read(p []byte) (int, error) {
	if len(p) > 512 {
		p = p[:512]
	}
	n := copy(p, c.s[c.read:])
	c.read += n
	return n, nil
}
