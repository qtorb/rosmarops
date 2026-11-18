package integration_test

import (
	"html"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/profiles"
	"github.com/qtorb/rosmarops/internal/report"
)

// BV-254 (CTO-68 §4): with blockReadsOutsideWorkingDirectories in force,
// Claude Code leaves out the repository's allowRead and does not re-open a
// working folder under a denyRead. The old P2 project block (denyRead ~/,
// allowRead .) then keeps the terminal out of the project, and the report
// says so in that project; the new P2 (user text only) does not; without the
// blocking rule the project's allowRead counts as before.
func TestBV254SandboxProjectReadable(t *testing.T) {
	p2, err := profiles.Permissions(profiles.P2)
	if err != nil {
		t.Fatal(err)
	}
	oldBlock := platform.ClaudePermissions{Sandbox: platform.ClaudeSandbox{DenyRead: []string{"~/"}, AllowRead: []string{"."}}}
	noBlock := p2
	noBlock.BlockReadsOutsideWorkingDirectories = false
	cases := []struct {
		name      string
		files     []platform.ClaudeSettingsFile
		os        string
		terminal  bool // the terminal reads the project's .env
		notice    bool
		localFile bool // the old block in .claude/settings.local.json
	}{
		{name: "old block with blockReads", files: []platform.ClaudeSettingsFile{b232User(p2), b232Project(oldBlock)}, os: "linux", terminal: false, notice: true},
		{name: "old block in settings.local.json", files: []platform.ClaudeSettingsFile{b232User(p2)}, os: "linux", terminal: false, notice: true, localFile: true},
		{name: "new template", files: []platform.ClaudeSettingsFile{b232User(p2)}, os: "linux", terminal: true, notice: false},
		{name: "no blockReads: the project's allowRead counts", files: []platform.ClaudeSettingsFile{b232User(noBlock), b232Project(oldBlock)}, os: "linux", terminal: true, notice: false},
		{name: "old block on Windows: no sandbox there", files: []platform.ClaudeSettingsFile{b232User(p2), b232Project(oldBlock)}, os: "windows", notice: false, terminal: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			files := c.files
			if c.localFile {
				l := b232Project(oldBlock)
				l.Scope, l.ConfigPath = platform.MCPScopeLocal, "/home/u/src/app/.claude/settings.local.json"
				files = append(files, l)
			}
			r := b232Machine{files: files}.run(t, c.os)
			if got := anyContains(r.reachVias("claude-code", b232Env), "shell_readonly_bash_commands"); got != c.terminal {
				t.Errorf("the terminal reads the project's .env: %v, want %v (%v)", got, c.terminal, r.reachVias("claude-code", b232Env))
			}
			want := []string(nil)
			if c.notice {
				want = []string{"~/src/app"}
			}
			if strings.Join(r.v2.SandboxBlockedProjects, ",") != strings.Join(want, ",") {
				t.Errorf("projects with the notice: %v, want %v", r.v2.SandboxBlockedProjects, want)
			}
			// The notice shows in «Projects checked», in both languages.
			r.v2.Coverage.Projects = &report.ProjectsCoverage{
				Search: "complete", Found: 1, Scanned: 1, Roots: []string{"~/src"},
				Items: []report.ProjectItem{{Path: "~/src/app", Source: "home"}}, NotSearched: []report.ProjectNotSearched{},
			}
			for _, lang := range []string{"en", "es"} {
				cat, err := catalog.Load(lang)
				if err != nil {
					t.Fatal(err)
				}
				page := report.RenderHTML(r.v2, cat, b232Home, time.UTC)
				if got := strings.Contains(page, html.EscapeString(cat.Text("cc.P2.old", nil))); got != c.notice {
					t.Errorf("%s: cc.P2.old in the page: %v, want %v", lang, got, c.notice)
				}
			}
			// report.v2 does not carry it.
			js, err := report.RenderV2JSON(r.v2)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(js), "SandboxBlocked") {
				t.Error("the notice leaked into report.v2")
			}
		})
	}
}
