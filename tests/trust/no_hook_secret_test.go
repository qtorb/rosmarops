package trust_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

// BV-107 (contract §2, §6.3): a project hook command can hold a secret in its
// arguments. RosmarOps resolves the repo script it runs without keeping the
// command text, so the secret never reaches the graph, the report, the
// Markdown, the export for your AI or the run log. The project_hook finding is
// still produced (vacuity guard).
func TestNoHookCommandSecretInReports(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "src", "app")
	claudeDir := filepath.Join(home, ".claude")
	// A hook command whose arguments carry a real-shaped secret, and a real
	// repo script it runs.
	cmd := "node .github/init.js --token " + fakeAnthropicKey
	body := `{"hooks":{"SessionStart":[{"matcher":"*","hooks":[{"type":"command","command":"` + cmd + `"}]}]}}`
	for p, c := range map[string][]byte{
		filepath.Join(repo, ".claude", "settings.json"): []byte(body),
		filepath.Join(repo, ".github", "init.js"):       make([]byte, 10),
	} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, c, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	proj := windows.ReadClaudeSettings(filepath.Join(repo, ".claude", "settings.json"), platform.MCPScopeProject, repo, claudeDir)
	user := windows.ReadClaudeSettings(filepath.Join(claudeDir, "settings.json"), platform.MCPScopeUser, "", claudeDir)
	settings := platform.ClaudeSettingsScan{
		Files: []platform.ClaudeSettingsFile{user, proj},
		Trust: []platform.ProjectTrust{{Path: repo, Accepted: true}},
	}
	salt := []byte("0123456789abcdef0123456789abcdef")
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	frag, _, err := graph.FromScans(platform.MCPScan{}, settings, platform.CredentialFileScan{}, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	g, err := graph.Infer(graph.Build(frag, graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt)}, now), []string{repo}, home)
	if err != nil {
		t.Fatal(err)
	}
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("0", 64), Base: base, RepoRoots: []string{repo},
		V1: report.Build("0.1.0-dev", nil, nil, 1, 0, 0, 0, nil), GeneratedAt: now,
	})

	// Vacuity: the finding must exist, so the outputs really do describe the hook.
	found := false
	for _, f := range v.Findings {
		if f.Rule == "project_hook_runs_repo_script" {
			found = true
		}
	}
	if !found {
		t.Fatal("no project_hook_runs_repo_script finding — test would be vacuous")
	}

	graphRaw, err := graph.Encode(g)
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{"graph.v2.json": string(graphRaw), "run log": buf.String()}
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		outputs["html "+lang] = report.RenderHTML(v, cat, home, time.UTC)
		outputs["markdown "+lang] = report.RenderV2Markdown(v, cat, time.UTC)
		outputs["ai "+lang] = report.RenderAIExport(v, cat, home)
	}
	mid := fakeAnthropicKey[4 : len(fakeAnthropicKey)-4]
	for name, out := range outputs {
		if strings.Contains(out, fakeAnthropicKey) || strings.Contains(out, mid) {
			t.Errorf("%s carries the hook-command secret", name)
		}
		if strings.Contains(out, "--token") {
			t.Errorf("%s carries the hook command text", name)
		}
	}
}
