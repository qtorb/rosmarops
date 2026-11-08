package trust_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// BV-198 point 8 (CTO-23 §1, §5): the reference computer «R2» as a repository
// fixture, and a test that fixes the nine factory scores. R2 is a Windows-shaped
// machine with ONE scored agent installed, a git project (src/app) whose
// AGENTS.md is present and whose .env (OPENAI key, a medium credential) is
// ignored by git and so unversioned, and a user-level AWS key (high) in
// ~/.aws/credentials. The project folder is trusted, as a product asks on first
// open. Each agent is scored alone with the real engine (graph → rules → index →
// BuildV2); a score that moves without a code or knowledge-base change fails the
// test (CTO-23 §5, reproducibility).
//
// PROVISIONAL FOOTPRINT: until the «measure-clean-installs» workflow (point 7)
// runs on a real Windows machine, each agent's at-rest files are taken from the
// reference-computer spec (§1) and RosmarOps's own knowledge of each product's
// configuration folder, not from a measured clean install. What a product writes
// only on first use is "sin verificar" and is NOT assumed here. When point 7's
// artifacts land, this fixture and the pinned scores are reconciled with them.

const (
	r2AWS  = fcHome + "/.aws/credentials" // user-level AWS key (high)
	r2Env  = fcRepo + "/.env"             // project .env, git-ignored (OPENAI, medium)
	r2Toml = fcRepo + "/AGENTS.md"
)

// r2Creds is the reference computer's two credentials: a high AWS key outside
// the project and a medium OpenAI key in the project's unversioned .env.
func r2Creds() []platform.CredentialEntry {
	return []platform.CredentialEntry{
		{Path: r2AWS, Line: 1, Key: "AWS_ACCESS_KEY_ID", Value: secrets.NewSecretValue("AKIAABCDEFGHIJ123456")},
		{Path: r2Env, Line: 1, Key: "OPENAI_API_KEY", Value: secrets.NewSecretValue("sk-proj-" + strings.Repeat("O", 48))},
	}
}

func r2Settings(files ...platform.ClaudeSettingsFile) platform.ClaudeSettingsScan {
	return platform.ClaudeSettingsScan{Files: files, Trust: []platform.ProjectTrust{{Path: fcRepo, Accepted: true}}}
}

func r2BuildV2(t *testing.T, g *graph.Graph, settings *platform.ClaudeSettingsScan, extra func(*report.V2Input)) report.V2 {
	t.Helper()
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	in := report.V2Input{
		Graph: g, GraphSHA256: strings0(64), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: []string{fcRepo}, V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: fcNow,
		ClaudeSettings: settings, Home: fcHome,
	}
	if extra != nil {
		extra(&in)
	}
	v, _ := report.BuildV2(in)
	return v
}

func r2Instr(product, configDir string) []platform.InstructionFile {
	return []platform.InstructionFile{{Product: product, ConfigDir: configDir, Path: r2Toml, Shared: true}}
}

// --- per-agent R2 reports ---

func r2ClaudeCode(t *testing.T) report.V2 {
	// Claude Code installed alone, default (auto) mode, Bash available. It loads
	// the repo's AGENTS.md (no CLAUDE.md present).
	settings := r2Settings(platform.ClaudeSettingsFile{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead})
	frag, _, err := graph.FromScans(platform.MCPScan{}, settings, platform.CredentialFileScan{Entries: r2Creds()}, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.AddInstructions(frag, platform.InstructionScan{Files: r2Instr("claude-code", fcClaudeDir)}, fcNow)
	g, err := graph.Infer(graph.Build(frag, r2Prov(), fcNow), []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, nil)
}

func r2Cursor(t *testing.T) report.V2 {
	settings := r2Settings()
	cursor := platform.CursorScan{ConfigDir: fcHome + "/.cursor", Exists: true}
	frag, _, err := graph.FromScansWithCursor(platform.MCPScan{}, settings, cursor, platform.CredentialFileScan{Entries: r2Creds()}, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.AddInstructions(frag, platform.InstructionScan{Files: r2Instr("cursor", fcHome+"/.cursor")}, fcNow)
	g, err := graph.Infer(graph.Build(frag, r2Prov(), fcNow), []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, nil)
}

func r2Codex(t *testing.T) report.V2 {
	scan := platform.CodexScan{
		ConfigDir: fcCodexDir, Exists: true,
		Layers: []platform.CodexLayer{
			{Kind: platform.CodexLayerUser, Path: fcCodexCfg, Status: platform.MCPSourceRead},
			func() platform.CodexLayer {
				l := platform.CodexLayer{Kind: platform.CodexLayerProject, Path: fcCodexProj, Root: fcRepo, Status: platform.MCPSourceRead}
				l.Trusted = true
				return l
			}(),
		},
	}
	settings := r2Settings()
	frag, _, err := graph.FromScansWithCodex(platform.MCPScan{Servers: graph.CodexMCPServers(scan)}, settings, platform.CursorScan{}, platform.CredentialFileScan{Entries: r2Creds()}, graph.Repos{}, platform.HermesScan{}, scan, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.AddInstructions(frag, platform.InstructionScan{Files: r2Instr("codex", fcCodexDir)}, fcNow)
	g := graph.Build(frag, r2Prov(), fcNow)
	graph.MarkCodexUntrusted(&g, scan)
	g, err = graph.Infer(g, []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, func(in *report.V2Input) { in.Codex = scan })
}

func r2Copilot(t *testing.T) report.V2 {
	scan := platform.CopilotScan{ConfigDir: fcCopilotDir, Present: true, Layers: []platform.CopilotLayer{{Kind: platform.CopilotLayerUser, Path: fcCopilotDir + "/settings.json", Status: platform.MCPSourceRead}}}
	settings := r2Settings()
	frag, _, err := graph.FromScans(platform.MCPScan{Servers: graph.CopilotMCPServers(scan)}, settings, platform.CredentialFileScan{Entries: r2Creds()}, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.Merge(frag, graph.FromCopilot(scan, true, fcNow))
	frag = graph.AddInstructions(frag, platform.InstructionScan{Files: r2Instr("github-copilot", fcCopilotDir)}, fcNow)
	g, err := graph.Infer(graph.Build(frag, r2Prov(), fcNow), []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, nil)
}

func r2Gemini(t *testing.T) report.V2 {
	// Gemini loads GEMINI.md, not AGENTS.md: the reference repo has AGENTS.md
	// only, so Gemini loads no project instruction here.
	scan := platform.GeminiScan{ConfigDir: fcGemini, Present: true, FolderTrustEnabled: true, Layers: []platform.GeminiLayer{{Kind: platform.GeminiLayerUser, Path: fcGemini + "/settings.json", Status: platform.MCPSourceRead}}}
	settings := r2Settings()
	frag, _, err := graph.FromScans(platform.MCPScan{Servers: graph.GeminiMCPServers(scan)}, settings, platform.CredentialFileScan{Entries: r2Creds()}, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.Merge(frag, graph.FromGeminiCLI(scan, fcNow))
	g := graph.Build(frag, r2Prov(), fcNow)
	graph.MarkGeminiUntrusted(&g, scan, []string{fcRepo})
	g, err = graph.Infer(g, []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, nil)
}

func r2Cline(t *testing.T) report.V2 {
	scan := platform.ClineScan{ConfigDir: fcHome + "/.cline/data", Present: true, GlobalStateStatus: platform.MCPSourceRead, HooksEnabled: true, MigrationVersion: 3}
	settings := r2Settings()
	frag, _, err := graph.FromScans(platform.MCPScan{}, settings, platform.CredentialFileScan{Entries: r2Creds()}, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.Merge(frag, graph.FromCline(scan, true, fcNow))
	frag = graph.AddInstructions(frag, platform.InstructionScan{Files: r2Instr("cline", fcHome+"/.cline/data")}, fcNow)
	g, err := graph.Infer(graph.Build(frag, r2Prov(), fcNow), []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, func(in *report.V2Input) { in.Cline = scan })
}

func r2Grok(t *testing.T) report.V2 {
	scan := platform.GrokScan{ConfigDir: fcGrokDir, Exists: true, Layers: []platform.GrokLayer{{Kind: platform.GrokLayerUser, Path: fcGrokCfg, Status: platform.MCPSourceRead}}}
	settings := r2Settings()
	frag, _, err := graph.FromScansWithGrok(platform.MCPScan{Servers: graph.GrokMCPServers(scan)}, settings, platform.CursorScan{}, platform.CredentialFileScan{Entries: r2Creds()}, graph.Repos{}, platform.HermesScan{}, platform.CodexScan{}, scan, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.AddInstructions(frag, platform.InstructionScan{Files: r2Instr("grok-build", fcGrokDir)}, fcNow)
	g, err := graph.Infer(graph.Build(frag, r2Prov(), fcNow), []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, func(in *report.V2Input) { in.Grok = scan })
}

func r2Hermes(t *testing.T) report.V2 {
	hd := fcHome + "/.hermes"
	cfg := platform.HermesConfig{Path: hd + "/config.yaml", ConfigDir: hd, Status: platform.MCPSourceRead, ApprovalMode: "smart", TerminalBackend: "local"}
	source := platform.MCPSource{Platform: "hermes", ConfigPath: cfg.Path, ConfigDir: cfg.ConfigDir, Status: platform.MCPSourceRead, LoadsMCP: true}
	hermes := platform.HermesScan{Configs: []platform.HermesConfig{cfg}, Sources: []platform.MCPSource{source}}
	settings := r2Settings()
	frag, _, err := graph.FromScansWithHermes(platform.MCPScan{Sources: hermes.Sources}, settings, platform.CursorScan{}, platform.CredentialFileScan{Entries: r2Creds()}, graph.Repos{}, hermes, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.AddInstructions(frag, platform.InstructionScan{Files: r2Instr("hermes", hd)}, fcNow)
	g, err := graph.Infer(graph.Build(frag, r2Prov(), fcNow), []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, func(in *report.V2Input) { in.Hermes = hermes })
}

func r2Antigravity(t *testing.T) report.V2 {
	scan := platform.AntigravityScan{ConfigDir: fcGemini, Present: true, Settings: platform.AntigravitySettings{Path: fcGemini + "/antigravity-cli/settings.json", Status: platform.MCPSourceRead}}
	settings := r2Settings()
	frag, _, err := graph.FromScans(platform.MCPScan{}, settings, platform.CredentialFileScan{Entries: r2Creds()}, fcSalt, fcNow)
	if err != nil {
		t.Fatal(err)
	}
	frag = graph.Merge(frag, graph.FromAntigravityCLI(scan, fcNow))
	frag = graph.AddInstructions(frag, platform.InstructionScan{Files: r2Instr("antigravity-cli", fcGemini)}, fcNow)
	g, err := graph.Infer(graph.Build(frag, r2Prov(), fcNow), []string{fcRepo}, fcHome)
	if err != nil {
		t.Fatal(err)
	}
	return r2BuildV2(t, &g, &settings, nil)
}

func r2Prov() graph.Provenance {
	return graph.Provenance{EngineVersion: "t", RulesVersion: graph.RulesVersion, KnowledgeVersion: knowledge.Composite, HostID: graph.HostID(fcSalt)}
}

// r2Case is one agent's build and its pinned factory score.
type r2Case struct {
	product string
	build   func(*testing.T) report.V2
	score   int
	band    string
}

// r2Cases are the nine scored agents of the reference computer, in the order
// factory-scores.json lists them.
func r2Cases() []r2Case {
	return []r2Case{
		{"claude-code", r2ClaudeCode, 31, "exposed"},
		{"cursor", r2Cursor, 31, "exposed"},
		{"codex", r2Codex, 73, "open_gaps"},
		{"github-copilot", r2Copilot, 76, "open_gaps"},
		{"gemini-cli", r2Gemini, 76, "open_gaps"},
		{"cline", r2Cline, 27, "exposed"},
		{"grok-build", r2Grok, 32, "exposed"},
		{"hermes", r2Hermes, 32, "exposed"},
		{"antigravity-cli", r2Antigravity, 76, "open_gaps"},
	}
}

// Factory-scores file (BV-200, CTO-23 §5): only ids, enums, stable codes,
// numbers, versions and dates — no user-facing prose. Field order here IS the
// file's key order.
type fsFinding struct {
	Code   string  `json:"code"`
	Weight float64 `json:"weight"`
}

type fsAgent struct {
	Product     string      `json:"product"`
	Score       int         `json:"score"`
	Band        string      `json:"band"`
	Findings    []fsFinding `json:"findings"`
	Assumptions []string    `json:"assumptions"`
}

type factoryScores struct {
	Format            string    `json:"format"`
	RosmaropsVersion  string    `json:"rosmarops_version"`
	Commit            string    `json:"commit"`
	GeneratedAt       string    `json:"generated_at"`
	ReferenceComputer string    `json:"reference_computer"`
	IndexVersion      int       `json:"index_version"`
	RulesVersion      string    `json:"rules_version"`
	KnowledgeVersion  string    `json:"knowledge_version"`
	Agents            []fsAgent `json:"agents"`
}

// r2AgentIndexID finds the evaluated agent of the given product and returns its
// score, band and ai_client id (R2 installs exactly one scored agent).
func r2AgentIndexID(t *testing.T, v report.V2, product string) (score int, band, id string) {
	t.Helper()
	for _, a := range v.Index.Agents {
		if a.AgentID == "" {
			continue
		}
		for _, o := range v.AgentsOverview {
			if o.AgentID == a.AgentID && o.Product == product {
				if a.Value == nil {
					t.Fatalf("%s has no score: %+v", product, a)
				}
				return *a.Value, a.Band, a.AgentID
			}
		}
	}
	t.Fatalf("no evaluated %s in R2 report", product)
	return 0, "", ""
}

// r2Detail returns the agent's findings as {code, weight} sorted by weight
// descending then code, and its assumption codes sorted unique.
func r2Detail(v report.V2, agentID string) ([]fsFinding, []string) {
	rule := map[string]string{}
	assume := map[string]bool{}
	for _, f := range v.Findings {
		if len(f.Evidence.Nodes) == 0 || f.Evidence.Nodes[0] != agentID {
			continue
		}
		rule[f.ID] = f.Rule
		for _, a := range f.Evidence.Assumptions {
			assume[a] = true
		}
	}
	var findings []fsFinding
	for _, a := range v.Index.Agents {
		if a.AgentID != agentID {
			continue
		}
		for _, w := range a.Weights {
			findings = append(findings, fsFinding{Code: rule[w.FindingID], Weight: w.Weight})
		}
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Weight != findings[j].Weight {
			return findings[i].Weight > findings[j].Weight
		}
		return findings[i].Code < findings[j].Code
	})
	assumptions := make([]string, 0, len(assume))
	for a := range assume {
		assumptions = append(assumptions, a)
	}
	sort.Strings(assumptions)
	if findings == nil {
		findings = []fsFinding{}
	}
	return findings, assumptions
}

// r2Collect builds the nine R2 reports, checks each against its pinned score,
// and returns the factory-scores document, the index version, and whether all
// nine matched. A mismatch is reported with t.Errorf (CTO-23 §5).
func r2Collect(t *testing.T) (doc factoryScores, allMatch bool) {
	t.Helper()
	allMatch = true
	idxVer := 0
	for _, c := range r2Cases() {
		v := c.build(t)
		got, band, id := r2AgentIndexID(t, v, c.product)
		idxVer = v.Index.IndexVersion
		t.Logf("R2 %s = %d (%s)", c.product, got, band)
		if got != c.score || band != c.band {
			allMatch = false
			t.Errorf("R2 %s = %d (%s), want %d (%s) — a score moved; if intended, update the pin", c.product, got, band, c.score, c.band)
		}
		findings, assumptions := r2Detail(v, id)
		doc.Agents = append(doc.Agents, fsAgent{Product: c.product, Score: got, Band: band, Findings: findings, Assumptions: assumptions})
	}
	doc.Format = "rosmarops.factory-scores/1"
	doc.RosmaropsVersion = envOr("ROSMAROPS_VERSION", "dev")
	doc.Commit = os.Getenv("GITHUB_SHA")
	doc.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	doc.ReferenceComputer = "R2"
	doc.IndexVersion = idxVer
	doc.RulesVersion = graph.RulesVersion
	doc.KnowledgeVersion = knowledge.Composite
	return doc, allMatch
}

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

// TestR2FactoryScores fixes the nine factory scores of the reference computer
// (CTO-23 §5). A change here without a code or knowledge-base change is a
// regression; a deliberate change updates the pinned values in the same commit.
// With ROSMAROPS_FACTORY_SCORES_OUT set, and only when all nine match, it also
// writes factory-scores.json to that path (BV-200): the binary is untouched,
// this lives only in the test, and with no variable nothing is written.
func TestR2FactoryScores(t *testing.T) {
	doc, allMatch := r2Collect(t)
	out := os.Getenv("ROSMAROPS_FACTORY_SCORES_OUT")
	if out == "" {
		return
	}
	// `go test` runs in the package directory (tests/trust), so a relative path
	// does not point at the repo root (BV-200 fix): demand an absolute path and
	// do not resolve it here.
	if !filepath.IsAbs(out) {
		t.Fatalf("ROSMAROPS_FACTORY_SCORES_OUT must be an absolute path, got %q: go test runs in the package directory, not the repo root", out)
	}
	if !allMatch {
		return
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", out, err)
	}
	t.Logf("wrote factory-scores to %s (%d bytes)", out, len(data))
}

// TestFactoryScoresJSON validates the generated file: the top-level and
// per-agent keys in the order BV-200 fixes, and that no value carries catalog
// prose (every value is an id, enum, code, number, version or date — none has
// a space, which any catalog sentence would).
func TestFactoryScoresJSON(t *testing.T) {
	doc, allMatch := r2Collect(t)
	if !allMatch {
		t.Fatal("factory scores did not match the pins; cannot validate the file")
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)

	// Top-level keys, in order.
	topKeys := []string{"format", "rosmarops_version", "commit", "generated_at", "reference_computer", "index_version", "rules_version", "knowledge_version", "agents"}
	assertKeyOrder(t, s, topKeys, "top-level")
	// Per-agent keys, in order (check the first agent block).
	assertKeyOrder(t, s[strings.Index(s, "\"agents\""):], []string{"product", "score", "band", "findings", "assumptions"}, "agent")

	// Re-parse and walk every string value: none may contain a space, so no
	// catalog sentence can have slipped in.
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		t.Fatal(err)
	}
	walkStrings(generic, func(v string) {
		if strings.ContainsRune(v, ' ') {
			t.Errorf("factory-scores value contains a space (possible catalog text): %q", v)
		}
	})

	// Shape sanity: nine agents, the format tag, R2.
	if len(doc.Agents) != 9 {
		t.Errorf("want 9 agents, got %d", len(doc.Agents))
	}
	if doc.Format != "rosmarops.factory-scores/1" || doc.ReferenceComputer != "R2" {
		t.Errorf("format/reference_computer: %q / %q", doc.Format, doc.ReferenceComputer)
	}
}

// assertKeyOrder checks that the given JSON keys appear in s in the given order.
func assertKeyOrder(t *testing.T, s string, keys []string, where string) {
	t.Helper()
	prev := -1
	for _, k := range keys {
		i := strings.Index(s, "\""+k+"\"")
		if i < 0 {
			t.Errorf("%s: key %q missing", where, k)
			continue
		}
		if i <= prev {
			t.Errorf("%s: key %q out of order", where, k)
		}
		prev = i
	}
}

// walkStrings calls fn on every string value in a decoded JSON value, skipping
// object keys.
func walkStrings(v any, fn func(string)) {
	switch x := v.(type) {
	case string:
		fn(x)
	case []any:
		for _, e := range x {
			walkStrings(e, fn)
		}
	case map[string]any:
		for _, e := range x {
			walkStrings(e, fn)
		}
	}
}
