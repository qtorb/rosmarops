package trust_test

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/intel"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/pyenv"
)

// TestNoSecretFromIntelCross is BV-100 (F0a): a known malicious release
// (LiteLLM 1.107.0) is planted three ways — in a user site-packages, in a
// repository's .venv, and as an MCP server invoked with `uvx litellm==1.107.0`
// carrying a fake secret in its arguments. The threat-intel cross must raise
// the three findings, and the fake secret passed to the MCP server must never
// appear — in full or by its middle — in the graph, any report (JSON, Markdown,
// HTML, export for your AI), the findings memory or the log. The MCP server's
// arguments are read only in memory and never written anywhere.
func TestNoSecretFromIntelCross(t *testing.T) {
	// The exact malicious coordinate, from the embedded snapshot's first entry.
	const (
		pkg     = "litellm"
		version = "1.107.0"
	)
	// A fake secret handed to the malicious MCP server on its command line.
	secret := "mcpARG_LEAK" + strings.Repeat("s", 40) + "LEAK"

	// Plant the package two ways on disk: a user site-packages and a repo .venv.
	root := t.TempDir()
	plant := func(site string) string {
		dir := filepath.Join(site, pkg+"-"+version+".dist-info")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "METADATA"), []byte("Name: "+pkg+"\nVersion: "+version+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		// cook.pth: the artifact the snapshot lists for this entry.
		if err := os.WriteFile(filepath.Join(site, "cook.pth"), []byte("import os\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	userSite := filepath.Join(root, "user", "site-packages")
	repoVenv := filepath.Join(root, "repo", ".venv", "Lib", "site-packages")
	userDistInfo := plant(userSite)
	repoDistInfo := plant(repoVenv)

	// The Python collector reads only the directory entries: no file is opened.
	py := pyenv.Scan([]string{userSite, repoVenv})
	if len(py.Packages) != 2 {
		t.Fatalf("pyenv read %d packages, want 2: %+v", len(py.Packages), py.Packages)
	}

	// The malicious MCP server: uvx litellm==1.107.0 with a fake secret arg.
	clineDir := filepath.Join(root, "cline")
	clineConfig := filepath.Join(clineDir, "cline_mcp_settings.json")
	server := platform.MCPServer{
		Client: "cline", Name: "litellm", Command: "uvx",
		Args:       []string{"litellm==" + version, "--api-key", secret},
		ConfigPath: clineConfig, ConfigDir: clineDir, LoadsMCP: true,
		Scope: platform.MCPScopeUser, Transport: "stdio",
	}
	mcpScan := platform.MCPScan{
		Servers: []platform.MCPServer{server},
		Sources: []platform.MCPSource{
			{Platform: "cline", ConfigPath: clineConfig, ConfigDir: clineDir, Status: platform.MCPSourceRead, LoadsMCP: true},
		},
	}

	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	salt := []byte("0123456789abcdef0123456789abcdef")
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	matcher := intel.NewMatcher(mustSnapshot(t))

	// Build the graph, then run the threat-intel cross on it (as the engine does
	// before writing the graph file).
	frag, err := graph.FromMCPScan(mcpScan, salt, now)
	if err != nil {
		t.Fatal(err)
	}
	g := graph.Build(frag, graph.Provenance{
		EngineVersion: "t", RulesVersion: graph.RulesVersion,
		KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt),
	}, now)
	pkgObs := []graph.PackageObservation{
		{Ecosystem: intel.EcosystemPyPI, Name: pkg, Version: version, Path: userDistInfo},
		{Ecosystem: intel.EcosystemPyPI, Name: pkg, Version: version, Path: repoDistInfo},
	}
	graph.CrossIntel(&g, mcpScan.Servers, pkgObs, matcher, now)

	// The three findings are raised: one per planted location.
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("0", 64), Base: base,
		V1: report.Build("0.1.0-dev", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: now,
		Memory: memory.File{}, Salt: salt, Home: root,
		IntelVersion: matcher.Version(), IntelPublishedAt: matcher.PublishedAt(),
	})
	var mal int
	for _, f := range v.Findings {
		if f.Rule == rules.RulePackageKnownMalicious {
			mal++
			if f.Severity != rules.SeverityRed {
				t.Errorf("malicious finding %s is %s, want red", f.ID, f.Severity)
			}
		}
	}
	if mal != 3 {
		t.Fatalf("%d package_known_malicious findings, want 3", mal)
	}
	if v.Status != "red" {
		t.Errorf("report status %q, want red", v.Status)
	}

	// Collect every output and assert the fake secret is nowhere in it.
	enc, err := graph.Encode(g)
	if err != nil {
		t.Fatal(err)
	}
	js, err := report.RenderV2JSON(v)
	if err != nil {
		t.Fatal(err)
	}
	outputs := map[string]string{"graph": string(enc), "report.v2.json": string(js)}
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		outputs["markdown "+lang] = report.RenderV2Markdown(v, cat, time.UTC)
		outputs["html "+lang] = report.RenderHTML(v, cat, root, time.UTC)
		outputs["ai "+lang] = report.RenderAIExport(v, cat, root)
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

	mid := secret[4 : len(secret)-4]
	for name, out := range outputs {
		if strings.Contains(out, secret) {
			t.Errorf("%s contains the full MCP argument secret", name)
		}
		if strings.Contains(out, mid) {
			t.Errorf("%s contains the middle of the MCP argument secret", name)
		}
	}

	// Not vacuous: the English report names the package and version, so the
	// finding really rendered.
	md := outputs["markdown en"]
	if !strings.Contains(md, pkg) || !strings.Contains(md, version) {
		t.Errorf("the Markdown does not name the malicious package and version")
	}
}

// mustSnapshot loads the embedded threat-data snapshot or fails the test.
func mustSnapshot(t *testing.T) *intel.Snapshot {
	t.Helper()
	snap, err := intel.Load()
	if err != nil {
		t.Fatalf("load embedded intel snapshot: %v", err)
	}
	return snap
}
