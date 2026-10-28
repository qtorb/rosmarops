package trust_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/credfiles"
)

// fakeAgentKeyFiles are the fake secrets of Codex's .credentials.json and
// Hermes Agent's auth.json (BV-96, N1b), by JSON Pointer.
var fakeAgentKeyFiles = map[string]string{
	"codex /k/access_token":                       "mcpAT_LEAK" + strings.Repeat("a", 40) + "LEAK",
	"codex /k/refresh_token":                      "mcpRT_LEAK" + strings.Repeat("r", 40) + "LEAK",
	"hermes /providers/nous/access_token":         "nousAT_LEAK" + strings.Repeat("n", 40) + "LEAK",
	"hermes /providers/nous/refresh_token":        "nousRT_LEAK" + strings.Repeat("m", 40) + "LEAK",
	"hermes /providers/nous/agent_key":            "nousAK_LEAK" + strings.Repeat("k", 40) + "LEAK",
	"hermes /providers/oc/tokens/access_token":    "ocAT_LEAK" + strings.Repeat("o", 40) + "LEAK",
	"hermes /providers/oc/tokens/refresh_token":   "ocRT_LEAK" + strings.Repeat("p", 40) + "LEAK",
	"hermes /credential_pool/x/0/access_token":    "poolAT_LEAK" + strings.Repeat("q", 40) + "LEAK",
	"hermes /credential_pool/x/0/refresh_token":   "poolRT_LEAK" + strings.Repeat("s", 40) + "LEAK",
	"hermes /credential_pool/x/0/agent_key":       "poolAK_LEAK" + strings.Repeat("t", 40) + "LEAK",
	"hermes /credential_pool/x/0/api_key":         "poolAPI_LEAK" + strings.Repeat("u", 40) + "LEAK",
	"hermes /credential_pool/anthropic/0/api_key": "sk-ant-api03-" + strings.Repeat("Q", 93) + "-" + strings.Repeat("W", 10) + "AA",
}

// TestNoFullSecretFromAgentKeyFiles is BV-96 (decision 2026-10-27, agentes
// nuevos, N1b): the secrets of Codex's .credentials.json (CODEX_HOME) and
// Hermes Agent's auth.json (HERMES_HOME, with a BOM) become credentials whose
// value never appears, in full or by its middle, in the graph, the report
// (JSON, Markdown, HTML, export for your AI), the findings memory or the log.
// Each pointer is the location of a finding, so the test is not vacuous;
// server_name and server_url are never read.
func TestNoFullSecretFromAgentKeyFiles(t *testing.T) {
	home := filepath.Join(t.TempDir(), "Users", "agentkeyuser")
	codexHome, hermesHome := filepath.Join(home, ".codex"), filepath.Join(home, "hermes")
	v := func(k string) string { return fakeAgentKeyFiles[k] }
	codex := `{"k": {"server_name": "srvNAME_LEAK", "server_url": "https://srvurl-leak.example/mcp", "client_id": "cid",
	  "access_token": "` + v("codex /k/access_token") + `", "refresh_token": "` + v("codex /k/refresh_token") + `"}}`
	hermes := "\ufeff" + `{"providers": {
	  "nous": {"access_token": "` + v("hermes /providers/nous/access_token") + `", "refresh_token": "` + v("hermes /providers/nous/refresh_token") +
		`", "agent_key": "` + v("hermes /providers/nous/agent_key") + `"},
	  "oc": {"tokens": {"access_token": "` + v("hermes /providers/oc/tokens/access_token") + `", "refresh_token": "` + v("hermes /providers/oc/tokens/refresh_token") + `"}}},
	  "credential_pool": {
	    "x": [{"access_token": "` + v("hermes /credential_pool/x/0/access_token") + `", "refresh_token": "` + v("hermes /credential_pool/x/0/refresh_token") +
		`", "agent_key": "` + v("hermes /credential_pool/x/0/agent_key") + `", "api_key": "` + v("hermes /credential_pool/x/0/api_key") + `"}],
	    "anthropic": [{"api_key": "` + v("hermes /credential_pool/anthropic/0/api_key") + `"}]}}`
	for p, c := range map[string]string{filepath.Join(codexHome, ".credentials.json"): codex, filepath.Join(hermesHome, "auth.json"): hermes} {
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	env := func(k string) (string, bool) {
		switch k {
		case "CODEX_HOME":
			return codexHome, true
		case "HERMES_HOME":
			return hermesHome, true
		}
		return "", false
	}
	credFiles := credfiles.Collect(home, nil, env)
	salt := []byte("0123456789abcdef0123456789abcdef")
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	claudeDir := filepath.Join(home, ".claude")
	settings := platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{{
		Scope: platform.MCPScopeUser, ConfigPath: filepath.Join(claudeDir, "settings.json"), ConfigDir: claudeDir,
		Status: platform.MCPSourceRead, Permissions: platform.ClaudePermissions{Allow: []string{"Bash(npm test)"}},
	}}}
	frag, _, err := graph.FromScans(platform.MCPScan{}, settings, credFiles, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	g, err := graph.Infer(graph.Build(frag, graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt)}, now), nil, home)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := graph.Encode(g)
	if err != nil {
		t.Fatal(err)
	}
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	vr, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("0", 64), Base: base,
		V1: report.Build("0.1.0-dev", nil, nil, 1, 0, 0, 0, nil), GeneratedAt: now, Salt: salt, Home: home,
	})

	// Not vacuous: every pointer is a location of a finding.
	located := map[string]string{}
	for _, f := range vr.Findings {
		for _, l := range f.Evidence.Locations {
			located[l] = f.Rule + "/" + f.Severity
		}
	}
	var seen []string
	for k := range fakeAgentKeyFiles {
		file, pointer, _ := strings.Cut(k, " ")
		path := filepath.Join(codexHome, ".credentials.json")
		if file == "hermes" {
			path = filepath.Join(hermesHome, "auth.json")
		}
		got, ok := located[path+"#"+pointer]
		if !ok {
			t.Fatalf("no finding at %s#%s — test would be vacuous; located %v", file, pointer, located)
		}
		seen = append(seen, file+" "+pointer+" → "+got)
	}
	sort.Strings(seen)
	t.Logf("findings at the key files:\n  %s", strings.Join(seen, "\n  "))

	js, err := report.RenderV2JSON(vr)
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{"graph": string(encoded), "report.v2.json": string(js)}
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		outputs["markdown "+lang] = report.RenderV2Markdown(vr, cat, time.UTC)
		outputs["html "+lang] = report.RenderHTML(vr, cat, home, time.UTC)
		outputs["ai "+lang] = report.RenderAIExport(vr, cat, home)
	}
	dir := t.TempDir()
	if err := memory.Write(dir, vr.NextMemory()); err != nil {
		t.Fatal(err)
	}
	mem, err := os.ReadFile(filepath.Join(dir, memory.FileName))
	if err != nil {
		t.Fatal(err)
	}
	outputs["findings-memory.json"] = string(mem)
	outputs["log"] = logBuf.String()
	// The entries themselves never print their value.
	raw, _ := json.Marshal(credFiles.Entries)
	outputs["entries as JSON"] = string(raw)

	for name, out := range outputs {
		for k, val := range fakeAgentKeyFiles {
			if strings.Contains(out, val) || strings.Contains(out, val[4:len(val)-4]) {
				t.Errorf("%s contains the secret of %s", name, k)
			}
		}
		for _, s := range []string{"srvNAME_LEAK", "srvurl-leak.example"} {
			if strings.Contains(out, s) {
				t.Errorf("%s contains Codex's server_name or server_url (%s)", name, s)
			}
		}
	}
}
