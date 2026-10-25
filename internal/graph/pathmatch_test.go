package graph

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/knowledge"
)

// One case per documented pattern form
// (https://code.claude.com/docs/en/permissions, "Read and Edit").
func TestMatchRulePath_DocumentedForms(t *testing.T) {
	project := ruleScope{appliesIn: "project:/src/app", configDir: "/home/alice/.claude", home: "/home/alice", roots: []string{"/src/app", "/src/other"}}
	user := ruleScope{appliesIn: "user", configDir: "/home/alice/.claude", home: "/home/alice", roots: []string{"/src/app", "/src/other"}}
	managed := ruleScope{appliesIn: "managed", configDir: "/home/alice/.claude", home: "/home/alice", roots: []string{"/src/app"}}
	cases := []struct {
		name, pattern, approval string
		scope                   ruleScope
		file                    string
		want                    bool
	}{
		// //path: absolute from the filesystem root.
		{"// absolute", "//Users/alice/secrets/**", "deny", project, "/Users/alice/secrets/key", true},
		{"// absolute elsewhere", "//Users/alice/secrets/**", "deny", project, "/Users/bob/secrets/key", false},
		{"// file", "//tmp/scratch.txt", "allow", project, "/tmp/scratch.txt", true},
		// ~/path: from the home folder.
		{"~/ glob", "~/Documents/*.pdf", "deny", project, "/home/alice/Documents/a.pdf", true},
		{"~/ glob one segment", "~/Documents/*.pdf", "deny", project, "/home/alice/Documents/x/a.pdf", false},
		{"~/ file", "~/.zshrc", "deny", user, "/home/alice/.zshrc", true},
		// /path: relative to the settings source.
		{"/ project", "/src/**/*.ts", "allow", project, "/src/app/src/a/b.ts", true},
		{"/ project not root", "/docs/**", "allow", project, "/docs/x.md", false},
		{"/ project not .claude", "/docs/**", "allow", project, "/src/app/.claude/docs/x.md", false},
		{"/ user anchors at config folder", "/secrets/**", "deny", user, "/home/alice/.claude/secrets/k", true},
		{"/ user not the project", "/secrets/**", "deny", user, "/src/app/secrets/k", false},
		{"/ managed undocumented", "/secrets/**", "deny", managed, "/src/app/secrets/k", false},
		// path or ./path: relative to the current directory.
		{"bare filename any depth", ".env", "deny", project, "/src/app/a/b/.env", true},
		{"**/ filename", "**/.env", "deny", project, "/src/app/a/.env", true},
		{"bare filename not parent", ".env", "deny", project, "/src/.env", false},
		{"./ anchored", "./.env", "deny", project, "/src/app/.env", true},
		{"./ anchored not nested", "./.env", "deny", project, "/src/app/a/.env", false},
		{"*.env any depth", "*.env", "deny", project, "/src/app/x/prod.env", true},
		{"user relative: any root", "src/**", "allow", user, "/src/other/src/a.ts", true},
		// Single directory segment: depth depends on the rule type.
		{"allow src/** top", "src/**", "allow", project, "/src/app/src/app.ts", true},
		{"allow src/** nested", "src/**", "allow", project, "/src/app/vendor/pkg/src/lib.js", false},
		{"deny src/** nested", "src/**", "deny", project, "/src/app/vendor/pkg/src/lib.js", true},
		{"**/src/** nested", "**/src/**", "allow", project, "/src/app/vendor/pkg/src/lib.js", true},
		{"src/components/** anchored", "src/components/**", "deny", project, "/src/app/x/src/components/a", false},
		// A pattern matches a folder's contents.
		{"folder name", "secrets", "deny", project, "/src/app/secrets/k", true},
		{"folder only", "secrets/", "deny", project, "/src/app/secrets", false},
		// Windows: POSIX form before matching.
		{"windows drive", "//c/**/.env", "deny", project, `C:\Users\alice\app\.env`, true},
		{"windows all drives", "//**/.env", "deny", project, `D:\x\.env`, true},
		{"windows case", "//c/Users/Alice/**", "allow", project, `C:\USERS\alice\f.txt`, true},
		// Negation and unusable patterns.
		{"allow ! approves nothing", "!x", "allow", project, "/src/app/x", false},
		{"allow unusable", "[", "allow", project, "/src/app/[", false},
		{"deny unusable guards exact path", "[", "deny", project, "/src/app/[", true},
		{"parentheses literal", "./Finance (2024)/**", "allow", project, "/src/app/Finance (2024)/q1.xlsx", true},
		{"all", "*", "allow", project, "/anything", true},
	}
	for _, c := range cases {
		if got := matchRulePath(c.pattern, c.approval, c.scope, c.file); got != c.want {
			t.Errorf("%s: matchRulePath(%q, %s, %s) = %v, want %v", c.name, c.pattern, c.approval, c.file, got, c.want)
		}
	}
}

func TestProtectedPaths(t *testing.T) {
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	e, _ := base.Entry(kbProtectedPaths)
	p := protectedPaths{entry: e, configDir: "/home/alice/.claude", home: "/home/alice"}
	custom := protectedPaths{entry: e, configDir: "/cfg/claude", home: "/home/alice"}
	cases := []struct {
		p    protectedPaths
		file string
		want bool
	}{
		{p, "/src/app/.git/config", true},
		{p, "/src/app/.config/git/config", true},
		{p, "/src/app/.claude/settings.json", true},
		{p, "/home/alice/.claude/settings.json", true},
		{p, "/home/alice/.claude.json", true},
		{p, "/src/app/.mcp.json", true},
		{p, "/home/alice/.bashrc", true},
		{p, "/src/app/sub/.npmrc", true},
		{p, "/src/app/.vscode/settings.json", true},
		{p, "/home/alice/.claude/dev-mods/m.js", true},
		{p, "/src/app/CLAUDE.md", false},
		{p, "/src/app/AGENTS.md", false},
		{p, "/src/app/.cursor/rules/x.mdc", false},
		{p, "/src/app/.cursorrules", false},
		{p, "/home/alice/.cursor/mcp.json", false},
		// Exceptions to .claude.
		{p, "/src/app/.claude/worktrees/w/a.go", false},
		{p, "/home/alice/.claude/plans/p.md", false},
		{p, "/home/alice/.claude/jobs/42/tmp/x", false},
		{p, "/home/alice/.claude/jobs/42/out", true},
		{p, "/home/alice/.claude/projects/app/memory/m.md", false},
		{p, "/home/alice/.claude/projects/app/memory/m.json", true},
		{p, "/src/app/.claude/agent-memory/notes.md", false},
		// An exception does not lift another entry's protection.
		{p, "/src/app/.claude/worktrees/w/.npmrc", true},
		// CLAUDE_CONFIG_DIR: the configuration folder counts as ~/.claude.
		{custom, "/cfg/claude/settings.json", true},
		{custom, "/cfg/claude/plans/p.md", false},
		{custom, "/cfg/claude/dev-mods/m.js", true},
		// Windows paths compare case-insensitively.
		{p, `C:\Users\alice\app\.GIT\config`, true},
		{p, `C:\Users\alice\app\.Claude\settings.json`, true},
	}
	for _, c := range cases {
		if got := c.p.protected(c.file); got != c.want {
			t.Errorf("protected(%s) = %v, want %v", c.file, got, c.want)
		}
	}
}
