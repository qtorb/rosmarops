package trust_test

import (
	"bytes"
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/adapters/linux"
	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/credfiles"
)

// orchSecrets are fake values in every file of Paseo, Claude Squad, Orca and
// a Claude Code subagent that may hold a secret (BV-268). None may reach the
// graph, the report or the log.
var orchSecrets = map[string]string{
	"paseo apiKey":        "sk-proj-ORCHLEAK" + strings.Repeat("a", 40) + "LEAK",
	"paseo launchEnv":     "ghp_ORCHLEAK" + strings.Repeat("b", 30) + "LEAK",
	"paseo keypair":       "ORCHLEAKKEYPAIR" + strings.Repeat("c", 30) + "LEAK",
	"paseo credential":    "ORCHLEAKLOCALCRED" + strings.Repeat("d", 30) + "LEAK",
	"paseo push":          "ORCHLEAKPUSH" + strings.Repeat("e", 30) + "LEAK",
	"squad diff":          "AKIAORCHLEAK" + strings.Repeat("F", 16) + "LEAK",
	"orca db":             "ORCHLEAKORCADB" + strings.Repeat("g", 30) + "LEAK",
	"orca key file":       "ORCHLEAKORCAKEY" + strings.Repeat("h", 30) + "LEAK",
	"subagent body":       "ORCHLEAKSUBAGENTBODY" + strings.Repeat("i", 30) + "LEAK",
	"subagent body token": "sk-ant-api03-ORCHLEAK" + strings.Repeat("j", 40) + "LEAK",
}

// orchLocked are the files RosmarOps never opens (CTO-76 §3), relative to
// the home folder.
var orchLocked = []string{
	".paseo/daemon-keypair.json", ".paseo/local-credential", ".paseo/push-tokens.json",
	".config/Orca/profiles/p1/profile-state.db", ".orca/credentials/openai.key",
}

func orchHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home", "oc")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, v := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "HERMES_HOME", "GH_CONFIG_DIR", "PAPERCLIP_HOME", "PASEO_HOME", "npm_config_cache"} {
		t.Setenv(v, "")
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	put := func(rel, content string) {
		p := filepath.Join(home, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	put(".claude.json", `{"mcpServers": {}}`)
	put(".claude/settings.json", `{"permissions": {"allow": ["Bash"]},"hooks":{"Stop":[{"hooks":[{"type":"command","command":"\"${HOME-}/.orca/agent-hooks/claude-hook.sh\""}]}]}}`)
	put(".claude/agents/helper.md", "---\nname: helper\npermissionMode: bypassPermissions\n---\nUse "+orchSecrets["subagent body"]+" and "+orchSecrets["subagent body token"]+"\n")
	put(".paseo/config.json", `{"version":1,"daemon":{"listen":"127.0.0.1:6767"},"features":{"dictation":{"stt":{"provider":"openai","apiKey":"`+orchSecrets["paseo apiKey"]+`"}}}}`)
	put(".paseo/agents/home-oc-repo/a1.json", `{"id":"a1","provider":"claude","cwd":"/home/oc/repo","lastModeId":"bypassPermissions","launchEnv":{"GITHUB_TOKEN":"`+orchSecrets["paseo launchEnv"]+`"}}`)
	put(".paseo/schedules/s1.json", `{"id":"s1","prompt":"x","cadence":{"type":"cron","expression":"0 9 * * *"},"target":{"type":"new-agent","config":{"provider":"claude","modeId":"bypassPermissions"}},"status":"active"}`)
	put(".paseo/daemon-keypair.json", `{"secretKey":"`+orchSecrets["paseo keypair"]+`"}`)
	put(".paseo/local-credential", orchSecrets["paseo credential"])
	put(".paseo/push-tokens.json", `["`+orchSecrets["paseo push"]+`"]`)
	put(".claude-squad/config.json", `{"default_program":"claude","auto_yes":true}`)
	put(".claude-squad/state.json", `{"instances":[{"program":"claude","auto_yes":true,"diff_stats":{"content":"+AWS_KEY=`+orchSecrets["squad diff"]+`"}}]}`)
	put(".config/Orca/profiles/p1/profile-state.db", orchSecrets["orca db"])
	put(".orca/credentials/openai.key", orchSecrets["orca key file"])
	return home
}

func orchRun(t *testing.T, home string) (graph.Graph, []byte, report.V2, platform.OrchestratorScan) {
	t.Helper()
	ctx := context.Background()
	p := linux.NewWith(filepath.Join(t.TempDir(), "managed"), "")
	mcp, err := p.ListMCPServers(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	settings, err := p.ListClaudeSettings(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	orch, err := p.ListOrchestrators(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	mcp.Sources = append(mcp.Sources, orch.Sources...)
	salt := []byte("0123456789abcdef0123456789abcdef")
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	res, err := pipeline.Assemble(pipeline.Input{
		MCP: mcp, Settings: settings, Orchestrators: orch,
		CredFiles:  credfiles.CollectOn(agentdirs.LinuxLayout, home, nil, nil),
		Provenance: graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt)},
		Salt:       salt, Home: home, Now: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := report.BuildV2(report.V2Input{
		Graph: &res.Graph, GraphSHA256: strings.Repeat("0", 64), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: now, ClaudeSettings: &settings, Home: home, Salt: salt,
	})
	return res.Graph, res.Encoded, v, orch
}

// TestOrchestratorSecretsNeverReachOutputs: Paseo's OpenAI key is a
// credential identified by its path, the approvals findings are there, and
// no secret value nor a subagent's body is in the scan, the graph, the
// report (JSON, Markdown, HTML, AI export, en and es) or the log.
func TestOrchestratorSecretsNeverReachOutputs(t *testing.T) {
	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	home := orchHome(t)
	g, encoded, v, scan := orchRun(t, home)

	byPath := 0
	for _, n := range g.Nodes {
		if n.Kind == graph.KindCredential && n.IdentifiedBy == graph.IdentifiedByPath {
			byPath++
		}
	}
	if byPath != 1 {
		t.Errorf("%d credentials identified by path, want Paseo's OpenAI key", byPath)
	}
	approvals := map[string]bool{}
	for _, f := range v.Findings {
		if f.Rule == rules.RuleOrchestratorSkipsApprovals {
			approvals[f.Params.Agent] = true
		}
	}
	for _, o := range []string{"Paseo", "Claude Squad", "Orca"} {
		if !approvals[o] {
			t.Errorf("no orchestrator_skips_approvals for %s; got %v", o, approvals)
		}
	}

	js, err := report.RenderV2JSON(v)
	if err != nil {
		t.Fatal(err)
	}
	rawScan, _ := json.Marshal(scan)
	outputs := map[string]string{"scan": string(rawScan), "graph": string(encoded), "report.v2.json": string(js)}
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		outputs["markdown "+lang] = report.RenderV2Markdown(v, cat, time.UTC)
		outputs["html "+lang] = report.RenderHTML(v, cat, home, time.UTC)
		outputs["ai "+lang] = report.RenderAIExport(v, cat, home)
	}
	outputs["log"] = logBuf.String()
	for name, out := range outputs {
		for k, val := range orchSecrets {
			if strings.Contains(out, val) || strings.Contains(out, val[4:len(val)-4]) {
				t.Errorf("%s holds the %s value", name, k)
			}
		}
		if strings.Contains(out, "ORCHLEAK") {
			t.Errorf("%s holds part of a secret value", name)
		}
	}
}

// TestOrchestratorForbiddenFilesNeverOpened: the files CTO-76 §3 puts out of
// reach are locked (chmod 000, which only binds when not run as root) and
// the run still reads everything else; no reader names them; and no file
// under Paseo's, Claude Squad's or Orca's folders other than the read ones
// is a source.
func TestOrchestratorForbiddenFilesNeverOpened(t *testing.T) {
	home := orchHome(t)
	for _, rel := range orchLocked {
		p := filepath.Join(home, filepath.FromSlash(rel))
		if err := os.Chmod(p, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(p, 0o600) })
	}
	_, _, _, scan := orchRun(t, home)
	if scan.Paseo == nil || scan.Paseo.ConfigStatus != platform.MCPSourceRead || scan.ClaudeSquad == nil || scan.Orca == nil {
		t.Fatalf("scan %+v", scan)
	}
	for _, s := range scan.Sources {
		switch filepath.Base(s.ConfigPath) {
		case "config.json", "helper.md":
		default:
			t.Errorf("source %s", s.ConfigPath)
		}
	}

	// No reader names a forbidden file: the names below appear in no string
	// literal of the BV-268 readers.
	names := []string{"daemon-keypair", "local-credential", "push-tokens", "profile-state", ".db", "credentials", "bot-tool-credentials", "google"}
	for _, src := range []string{"orchestrators.go", "subagents.go"} {
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, filepath.Join("..", "..", "adapters", "windows", src), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			v, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			for _, name := range names {
				if strings.Contains(v, name) {
					t.Errorf("%s:%d names %q", src, fset.Position(lit.Pos()).Line, v)
				}
			}
			return true
		})
	}
}
