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

// paperclipSecrets are fake values in every Paperclip file that holds a
// secret. RosmarOps identifies those credentials by their path: none of these
// values may reach the graph, the report or the log (BV-264, Albert 20-11).
var paperclipSecrets = map[string]string{
	"master.key":       "pcMASTER_LEAK" + strings.Repeat("m", 40) + "LEAK",
	".env":             "pcJWT_LEAK" + strings.Repeat("j", 40) + "LEAK",
	"llm.apiKey":       "pcLLM_LEAK" + strings.Repeat("k", 40) + "LEAK",
	"connectionString": "pcDBPW_LEAK" + strings.Repeat("p", 20) + "LEAK",
	"run-log":          "pcRUNLOG_LEAK" + strings.Repeat("r", 30) + "LEAK",
}

// paperclipHome is a fake Linux home with Claude Code (its Bash allowed, so it
// reads any file) and a Paperclip instance in authenticated mode, listening on
// the LAN with sign-up open, installed at 2026.415.0 (before the fix).
func paperclipHome(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home", "pc")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	for _, v := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "HERMES_HOME", "XDG_CONFIG_HOME", "GH_CONFIG_DIR", "PAPERCLIP_HOME", "npm_config_cache"} {
		t.Setenv(v, "")
	}
	put := func(rel, content string, mode os.FileMode) {
		p := filepath.Join(home, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
	}
	put(".claude.json", `{"mcpServers": {}}`, 0o600)
	put(".claude/settings.json", `{"permissions": {"allow": ["Bash"]}}`, 0o600)
	inst := ".paperclip/instances/default/"
	put(inst+"config.json", `{"$meta":{"version":1},"server":{"deploymentMode":"authenticated","exposure":"public","bind":"lan","host":"0.0.0.0","port":3100},`+
		`"auth":{"disableSignUp":false},"database":{"mode":"postgres","connectionString":"postgres://paperclip:`+paperclipSecrets["connectionString"]+`@db.local:5432/pc"},`+
		`"llm":{"apiKey":"`+paperclipSecrets["llm.apiKey"]+`"},"telemetry":{"enabled":true}}`, 0o600)
	put(inst+"secrets/master.key", paperclipSecrets["master.key"], 0o600)
	put(inst+".env", "PAPERCLIP_AGENT_JWT_SECRET="+paperclipSecrets[".env"]+"\n", 0o600)
	put(inst+"db/PG_VERSION", "16\n", 0o600)
	put(inst+"data/run-logs/co/agent/run.ndjson", `{"line":"`+paperclipSecrets["run-log"]+`"}`, 0o600)
	put(inst+"claude-runtime/runs/r1/mcp/mcp-config.json", `{"Authorization":"Bearer `+paperclipSecrets["run-log"]+`"}`, 0o600)
	put(".paperclip/cli/install.json", `{"version":"2026.415.0","source":"install.sh","channel":"stable"}`, 0o644)
	return home
}

// paperclipRun reads home with the real Linux readers and builds the graph
// (pipeline.Assemble) and the v2 report.
func paperclipRun(t *testing.T, home string) (graph.Graph, []byte, report.V2, platform.PaperclipScan) {
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
	pc, err := p.ListPaperclip(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	mcp.Sources = append(mcp.Sources, pc.Sources...)
	salt := []byte("0123456789abcdef0123456789abcdef")
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	res, err := pipeline.Assemble(pipeline.Input{
		MCP: mcp, Settings: settings, Paperclip: pc,
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
		Paperclip: pc,
	})
	return res.Graph, res.Encoded, v, pc
}

// TestPaperclipSecretsNeverReachOutputs: the secrets of Paperclip's files are
// credentials identified by their path (identified_by path, no fingerprint),
// cred_readable applies to them, and no value of theirs is in the graph, the
// report (JSON, Markdown, HTML, AI export) or the log.
func TestPaperclipSecretsNeverReachOutputs(t *testing.T) {
	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	home := paperclipHome(t)
	g, encoded, v, _ := paperclipRun(t, home)

	byPath := 0
	for _, n := range g.Nodes {
		if n.Kind != graph.KindCredential || n.IdentifiedBy != graph.IdentifiedByPath {
			continue
		}
		byPath++
		if n.Fingerprint != "" || !strings.HasPrefix(n.PathFingerprint, "hmac-sha256:") || n.FileOwner == "" {
			t.Errorf("path credential %+v", n)
		}
	}
	if byPath != 4 {
		t.Fatalf("%d credentials identified by path, want 4 (master.key, .env, llm.apiKey, connectionString)", byPath)
	}
	rulesOf := map[string]int{}
	for _, f := range v.Findings {
		rulesOf[f.Rule]++
	}
	for _, r := range []string{
		rules.RuleCredReadable, rules.RuleOrchestratorSkipsApprovals, rules.RuleOrchestratorVulnerableVersion,
		rules.RuleOrchestratorNetworkListen, rules.RuleOrchestratorOpenSignup,
	} {
		if rulesOf[r] == 0 && !(r == rules.RuleCredReadable && rulesOf[rules.RuleCredReadAndSend] > 0) {
			t.Errorf("no %s finding; findings by rule %v", r, rulesOf)
		}
	}

	js, err := report.RenderV2JSON(v)
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{"graph": string(encoded), "report.v2.json": string(js)}
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
		for k, val := range paperclipSecrets {
			if strings.Contains(out, val) || strings.Contains(out, val[4:len(val)-4]) {
				t.Errorf("%s holds the %s value", name, k)
			}
		}
	}
	if !strings.Contains(outputs["log"], "paperclip: telemetry") {
		t.Error("the telemetry fact is not in the log")
	}
}

// TestPaperclipPathFingerprintIgnoresContent: a credential identified by its
// path keeps its id and path fingerprint when its file's content changes.
func TestPaperclipPathFingerprintIgnoresContent(t *testing.T) {
	home := paperclipHome(t)
	fps := func() map[string]string {
		g, _, _, _ := paperclipRun(t, home)
		out := map[string]string{}
		for _, n := range g.Nodes {
			if n.IdentifiedBy == graph.IdentifiedByPath {
				out[n.Locations[0]] = n.ID + " " + n.PathFingerprint
			}
		}
		return out
	}
	before := fps()
	inst := filepath.Join(home, ".paperclip", "instances", "default")
	if err := os.WriteFile(filepath.Join(inst, "secrets", "master.key"), []byte("another key entirely"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inst, ".env"), []byte("PAPERCLIP_AGENT_JWT_SECRET=changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, _ := os.ReadFile(filepath.Join(inst, "config.json"))
	cfg = bytes.Replace(cfg, []byte(paperclipSecrets["llm.apiKey"]), []byte("a-new-api-key-value"), 1)
	if err := os.WriteFile(filepath.Join(inst, "config.json"), cfg, 0o600); err != nil {
		t.Fatal(err)
	}
	after := fps()
	if len(before) != 4 {
		t.Fatalf("before: %v", before)
	}
	for loc, id := range before {
		if after[loc] != id {
			t.Errorf("%s: %q became %q when its content changed", loc, id, after[loc])
		}
	}
}

// TestPaperclipProtectedFilesNeverOpened proves the reader sees Paperclip's
// database, data, master key, .env and run logs only through metadata:
// (a) with them unreadable (chmod 000, effective when the tests do not run as
// root) the reader still finds them, and (b) the functions that look at them
// call no function that opens a file.
func TestPaperclipProtectedFilesNeverOpened(t *testing.T) {
	home := paperclipHome(t)
	inst := filepath.Join(home, ".paperclip", "instances", "default")
	locked := []string{filepath.Join(inst, "secrets", "master.key"), filepath.Join(inst, ".env"), filepath.Join(inst, "data", "run-logs", "co", "agent", "run.ndjson")}
	for _, p := range locked {
		if err := os.Chmod(p, 0); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, p := range locked {
			_ = os.Chmod(p, 0o600)
		}
	})
	_, _, _, pc := paperclipRun(t, home)
	if len(pc.Instances) != 1 {
		t.Fatalf("instances %+v", pc.Instances)
	}
	exists := map[string]bool{}
	for _, f := range pc.Instances[0].Files {
		exists[f.Role] = f.Exists
	}
	for _, role := range []string{platform.PaperclipMasterKey, platform.PaperclipEnv, platform.PaperclipDB, platform.PaperclipRunLogs} {
		if !exists[role] {
			t.Errorf("%s not seen from its metadata", role)
		}
	}

	src := filepath.Join("..", "..", "adapters", "windows", "paperclip.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, src, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{"Open": true, "OpenFile": true, "ReadFile": true, "readBounded": true, "ReadAll": true, "ReadDir": true}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || (fn.Name.Name != "lstatPaperclip" && fn.Name.Name != "fileModeOwner") {
			continue
		}
		ast.Inspect(fn, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				name := ""
				switch f := call.Fun.(type) {
				case *ast.SelectorExpr:
					name = f.Sel.Name
				case *ast.Ident:
					name = f.Name
				}
				if forbidden[name] {
					t.Errorf("%s calls %s: a protected file must be seen from its metadata only", fn.Name.Name, name)
				}
			}
			return true
		})
	}
	// Only config.json and install.json are read under the Paperclip home:
	// no path the reader passes to readBounded is a protected one.
	for _, s := range pc.Sources {
		base := filepath.Base(s.ConfigPath)
		if base != "config.json" && base != "install.json" {
			t.Errorf("read %s", s.ConfigPath)
		}
	}
	raw, _ := json.Marshal(pc)
	for k, val := range paperclipSecrets {
		if strings.Contains(string(raw), val) {
			t.Errorf("the scan holds the %s value", k)
		}
	}
}
