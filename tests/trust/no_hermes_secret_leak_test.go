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
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

// TestNoFullSecretFromHermesConfig is BV-97 (decision 2026-10-27, agentes
// nuevos, N2a): Hermes Agent's config.yaml with fake keys in a local server's
// env and a remote server's headers, and a URL with user:password@, a token
// in the path and in the query. None of those values appears, in full or by
// its middle, in the graph, the report (JSON, Markdown, HTML, export for
// your AI), the findings memory or the log. The remote server's origin is
// in the graph as https://host; the report names the host.
func TestNoFullSecretFromHermesConfig(t *testing.T) {
	home := filepath.Join(t.TempDir(), "Users", "hermesuser")
	hermes := filepath.Join(home, "hermes")
	secrets := map[string]string{
		"env":      "hermesENV_LEAK" + strings.Repeat("e", 40) + "LEAK",
		"header":   "hermesHDR_LEAK" + strings.Repeat("h", 40) + "LEAK",
		"password": "hermesPW_LEAK" + strings.Repeat("p", 20) + "LEAK",
		"path":     "hermesPATH_LEAK" + strings.Repeat("t", 20) + "LEAK",
		"query":    "hermesQRY_LEAK" + strings.Repeat("q", 20) + "LEAK",
	}
	config := `approvals:
  mode: off
mcp_servers:
  files:
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem"]
    env:
      FILES_API_KEY: "` + secrets["env"] + `"
  linear:
    url: https://bob:` + secrets["password"] + `@mcp.linear.example/mcp/` + secrets["path"] + `?api_key=` + secrets["query"] + `
    headers:
      Authorization: "Bearer ` + secrets["header"] + `"
`
	if err := os.MkdirAll(hermes, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(hermes, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	scan := windows.ReadHermes(home, func(k string) (string, bool) {
		if k == "HERMES_HOME" {
			return hermes, true
		}
		return "", false
	})
	if len(scan.Servers) != 2 || len(scan.Configs) != 1 || scan.Configs[0].ApprovalMode != "off" {
		t.Fatalf("scan %+v", scan)
	}
	salt := []byte("0123456789abcdef0123456789abcdef")
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	present := []platform.AgentPresence{{Platform: "hermes", ConfigDir: hermes, Marker: cfgPath}}
	build := func(servers []platform.MCPServer, mem memory.File) (report.V2, []byte) {
		frag, err := graph.FromMCPScan(platform.MCPScan{Sources: scan.Sources, Servers: servers, Present: present}, salt, now)
		if err != nil {
			t.Fatal(err)
		}
		g := graph.Build(frag, graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt)}, now)
		enc, err := graph.Encode(g)
		if err != nil {
			t.Fatal(err)
		}
		v, _ := report.BuildV2(report.V2Input{
			Graph: &g, GraphSHA256: strings.Repeat("0", 64), Base: base,
			V1: report.Build("0.1.0-dev", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: now, Memory: mem, Salt: salt, Home: home,
		})
		return v, enc
	}
	first, _ := build(nil, memory.File{})
	v, encoded := build(scan.Servers, first.NextMemory())

	// Not vacuous: both servers are in the graph, the remote one with its
	// origin, and both are «new» in the report.
	g := string(encoded)
	for _, want := range []string{`"type": "sends_to"`, `"path": "https://mcp.linear.example"`, `"platform": "hermes"`, `"destination_ref": "hmac-sha256:`, `"transport": "stdio"`, `"transport": "http"`} {
		if !strings.Contains(g, want) {
			t.Fatalf("graph lacks %s", want)
		}
	}
	if len(v.NewMCP) != 2 {
		t.Fatalf("new MCP lines %+v", v.NewMCP)
	}

	js, err := report.RenderV2JSON(v)
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{"graph": g, "report.v2.json": string(js)}
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		outputs["markdown "+lang] = report.RenderV2Markdown(v, cat, time.UTC)
		outputs["html "+lang] = report.RenderHTML(v, cat, home, time.UTC)
		outputs["ai "+lang] = report.RenderAIExport(v, cat, home)
	}
	dir := t.TempDir()
	if err := memory.Write(dir, v.NextMemory()); err != nil {
		t.Fatal(err)
	}
	mem, err := os.ReadFile(filepath.Join(dir, memory.FileName))
	if err != nil {
		t.Fatal(err)
	}
	outputs["findings-memory.json"] = string(mem)
	outputs["log"] = logBuf.String()
	for name, out := range outputs {
		for k, val := range secrets {
			if strings.Contains(out, val) || strings.Contains(out, val[4:len(val)-4]) {
				t.Errorf("%s contains the %s secret", name, k)
			}
		}
		if strings.Contains(out, "bob:") {
			t.Errorf("%s contains the URL's user", name)
		}
	}
	md := outputs["markdown en"]
	if !strings.Contains(md, "mcp.linear.example") || !strings.Contains(md, "Hermes Agent") {
		t.Errorf("the Markdown does not name the host or the agent")
	}
}
