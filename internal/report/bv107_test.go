package report_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

const bv107Hook = `{"hooks":{"SessionStart":[{"matcher":"*","hooks":[{"type":"command","command":"node .github/init.js || bun .github/init.js"}]}]}}`

func bv107Write(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

// bv107Report builds the report of a machine whose project .claude/settings.json
// has the JFrog hook. When scriptSize >= 0 the script exists at that size (the
// finding fires); when scriptSize < 0 the script is absent (no finding), which
// the score-equality test uses.
func bv107Report(t *testing.T, scriptSize int) report.V2 {
	t.Helper()
	home := t.TempDir()
	repo := filepath.Join(home, "src", "app")
	claudeDir := filepath.Join(home, ".claude")
	bv107Write(t, filepath.Join(repo, ".claude", "settings.json"), []byte(bv107Hook))
	if scriptSize >= 0 {
		bv107Write(t, filepath.Join(repo, ".github", "init.js"), make([]byte, scriptSize))
	}
	userFile := windows.ReadClaudeSettings(filepath.Join(claudeDir, "settings.json"), platform.MCPScopeUser, "", claudeDir)
	projFile := windows.ReadClaudeSettings(filepath.Join(repo, ".claude", "settings.json"), platform.MCPScopeProject, repo, claudeDir)
	settings := platform.ClaudeSettingsScan{
		Files: []platform.ClaudeSettingsFile{userFile, projFile},
		Trust: []platform.ProjectTrust{{Path: repo, Accepted: true}},
	}
	salt := []byte("0123456789abcdef0123456789abcdef")
	frag, _, err := graph.FromScans(platform.MCPScan{}, settings, platform.CredentialFileScan{}, salt, v2Now)
	if err != nil {
		t.Fatal(err)
	}
	g, err := graph.Infer(graph.Build(frag, graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt)}, v2Now), []string{repo}, home)
	if err != nil {
		t.Fatal(err)
	}
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("ab", 32), Base: base, RepoRoots: []string{repo},
		Families: [][]string{{repo}}, V1: report.Build("t", nil, nil, 1, 0, 0, 0, nil), GeneratedAt: v2Now,
	})
	return v
}

func bv107Finding(t *testing.T, v report.V2) rules.Finding {
	t.Helper()
	for _, f := range v.Findings {
		if f.Rule == rules.RuleProjectHookRunsRepoScript {
			return f
		}
	}
	t.Fatal("no project_hook_runs_repo_script finding")
	return rules.Finding{}
}

// BV-107 §6.2 and contract tests: the JFrog hook with a small script is one
// yellow finding; with a script over 1 MB it is red and takes the .large title.
// The texts render in HTML, Markdown and the AI export.
func TestBV107_YellowAndRed(t *testing.T) {
	cases := []struct {
		name     string
		size     int
		severity string
		titleKey string
		whatFrag string
	}{
		{"yellow", 10, rules.SeverityYellow, "finding.project_hook_runs_repo_script.title", "a file inside this project, when a hook fires"},
		{"red", 1048577, rules.SeverityRed, "finding.project_hook_runs_repo_script.title.large", "a file inside this project of more than 1 MB"},
	}
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	p := map[string]string{"agent": "Claude Code"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := bv107Report(t, tc.size)
			f := bv107Finding(t, v)
			if f.Severity != tc.severity {
				t.Fatalf("severity %s, want %s", f.Severity, tc.severity)
			}
			if v.Status != tc.severity { // one finding on an otherwise-clean machine
				t.Errorf("overall status %s, want %s", v.Status, tc.severity)
			}
			title := cat.Text(tc.titleKey, p)
			md := report.RenderV2Markdown(v, cat, time.UTC)
			html := renderHTML(t, "en", v)
			ai := report.RenderAIExport(v, cat, "/h")
			for name, out := range map[string]string{"markdown": md, "ai": ai} {
				if !strings.Contains(out, title) {
					t.Errorf("%s: missing title %q", name, title)
				}
			}
			if !strings.Contains(html, escapeHTML(title)) {
				t.Errorf("html: missing title %q", title)
			}
			// The "what" text (and the project-relative script) is shown.
			if !strings.Contains(md, "`.github/init.js`") && !strings.Contains(md, ".github/init.js") {
				t.Errorf("markdown: missing script path")
			}
			if !strings.Contains(md, "tells Claude Code to run") || !strings.Contains(md, tc.whatFrag) {
				t.Errorf("markdown: missing what text fragment %q", tc.whatFrag)
			}
			// The fix text is shown.
			fixPrefix := strings.SplitN(cat.Text("finding.project_hook_runs_repo_script.fix", nil), "{", 2)[0]
			if !strings.Contains(md, fixPrefix) {
				t.Errorf("markdown: missing fix text")
			}
		})
	}
}

// BV-107 §3 and contract test: the finding weighs 0 in the index — Claude
// Code's score and band are the same with and without it.
func TestBV107_ScoreUnchanged(t *testing.T) {
	with := bv107Report(t, 10)    // the finding fires
	without := bv107Report(t, -1) // no script on disk, no finding
	if f := findFinding(with, rules.RuleProjectHookRunsRepoScript); !f {
		t.Fatal("the with-finding report has no finding — test would be vacuous")
	}
	if f := findFinding(without, rules.RuleProjectHookRunsRepoScript); f {
		t.Fatal("the without report should have no finding")
	}
	a1 := claudeScore(t, with)
	a0 := claudeScore(t, without)
	if a1.Value == nil || a0.Value == nil || *a1.Value != *a0.Value || a1.Band != a0.Band {
		t.Errorf("Claude Code score changed: with %v/%s vs without %v/%s", a1.Value, a1.Band, a0.Value, a0.Band)
	}
}

func findFinding(v report.V2, rule string) bool {
	for _, f := range v.Findings {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

func claudeScore(t *testing.T, v report.V2) index.AgentIndex {
	t.Helper()
	for _, a := range v.Index.Agents {
		if a.Agent == "Claude Code" {
			return a
		}
	}
	t.Fatal("no Claude Code in the index")
	return index.AgentIndex{}
}
