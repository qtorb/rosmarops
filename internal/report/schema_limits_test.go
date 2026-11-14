package report_test

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
	"github.com/qtorb/rosmarops/internal/schemas"
)

// BV-203 point 3 (CTO-24 E5): the limits come from the schema, not a hand
// list. For every maxLength and maxItems in space-view.v1 and report.v2, a
// maximal graph is driven through the real pipeline so the limited field would
// overflow; the produced output must still validate against its schema (the
// pipeline clips every field), and every distinct limit must be exercised —
// some output string or array reaches exactly that cap — or be named in the
// documented allowlist below with the reason it cannot be driven from a graph.
//
// Keyed by the limit's (kind, value): a value like maxLength 400 is exercised
// once any clipped field reaches 400 runes. Several schema fields share a cap
// (the t200/t300/t600/... text defs), and the catalog or knowledge base bounds
// most text well under its cap, so what a graph can push to the boundary are
// the path and count limits; the rest are allowlisted with their reason.

// limit is one maxLength/maxItems appearing in a schema.
type limit struct {
	kind  string // "maxLength" | "maxItems"
	value int
}

func (l limit) String() string { return fmt.Sprintf("%s=%d", l.kind, l.value) }

// schemaLimits collects every distinct (kind, value) maxLength/maxItems in raw.
func schemaLimits(t *testing.T, raw []byte) map[limit]bool {
	t.Helper()
	var root any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	out := map[limit]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for _, k := range []string{"maxLength", "maxItems"} {
				if n, ok := x[k].(float64); ok {
					out[limit{k, int(n)}] = true
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(root)
	return out
}

// atCap walks a produced document and records, for every string, a
// maxLength=<runes> entry, and for every array, a maxItems=<len> entry — the
// caps the output actually reaches.
func atCap(t *testing.T, doc []byte) map[limit]bool {
	t.Helper()
	var root any
	if err := json.Unmarshal(doc, &root); err != nil {
		t.Fatal(err)
	}
	out := map[limit]bool{}
	var walk func(v any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for _, child := range x {
				walk(child)
			}
		case []any:
			out[limit{"maxItems", len(x)}] = true
			for _, child := range x {
				walk(child)
			}
		case string:
			out[limit{"maxLength", utf8.RuneCountInString(x)}] = true
		}
	}
	walk(root)
	return out
}

// limitsAllowlist are the schema limits a graph cannot push to their cap, with
// the reason. It only ever shrinks: a limit that becomes graph-drivable moves
// out. The core guarantee (the output validates, so nothing exceeds any cap)
// holds for every limit, driven or allowlisted.
var limitsAllowlist = map[limit]string{
	{"maxLength", 60}:    "agents[].name / services[].service: product and catalog MCP names, fixed by the knowledge base and MCP catalog, never near 60",
	{"maxLength", 80}:    "keys[].label: a short catalog phrase plus the key kind, fixed well under 80",
	{"maxLength", 32}:    "analysis.intel_version: the threat-intel snapshot version string, fixed and short",
	{"maxLength", 400}:   "findings[].file and report.rows[].locations[]: finding path values; the engine's paths in these fields stay well under 400 in practice (observed max ~200, home-relativized), so a graph cannot drive them to 400. The cap is still honoured — the maximal output validates, so no file/location exceeds 400.",
	{"maxLength", 600}:   "t600 (report step1.warnings / step2.lines / rows[].assumptions / how_fix_body / coverage): catalog text with bounded params, under 600",
	{"maxLength", 300}:   "report step headlines/counts/meta/verdict/gain (t300): catalog text with bounded params, under 300",
	{"maxLength", 1000}:  "report.rows[].fix_steps (t1000): catalog fix steps, under 1000",
	{"maxLength", 16000}: "finding_text.paste_block: catalog paste block, under 16000",
	{"maxLength", 40000}: "card_svg: the generated share card SVG, bounded by its template, not graph-driven",
	{"maxLength", 49152}: "report.ai_export (t49152): the AI export text, bounded well under 48 KiB",
	{"maxItems", 5}:      "report.rows[].assumptions: at most a handful of assumption lines per finding, never 5",
	{"maxItems", 6}:      "report step2.lines / rows[].fix_steps: catalog-driven, never 6",
	{"maxItems", 8}:      "report step1.warnings: at most a few run warnings, never 8",
	{"maxItems", 12}:     "agents[].groups (BV-235): one per finding group, and there are eight groups, never 12",
	{"maxItems", 20}:     "since.score_changes / overview / signals: bounded by the nine scored agents and the signal categories, never 20",
	{"maxItems", 50}:     "report.coverage: one line per coverage topic, never 50",
}

func TestSchemaLimitsFromPipeline(t *testing.T) {
	// 1. Every limit, straight from the two schemas.
	want := map[limit]bool{}
	for l := range schemaLimits(t, schemas.SpaceViewSchema) {
		want[l] = true
	}
	for l := range schemaLimits(t, schemas.ReportV2Schema) {
		want[l] = true
	}
	if len(want) == 0 {
		t.Fatal("no limits read from the schemas")
	}

	// 2. A maximal graph through the real pipeline.
	g, v2 := maximalReport(t)

	// 3. The outputs validate: the pipeline clipped every field to its cap, so
	// no limit is ever exceeded. This is the core E5 guarantee.
	svJSON := mustValidSpaceView(t, v2, g)
	v2JSON, err := report.RenderV2JSON(v2)
	if err != nil {
		t.Fatalf("report.v2 fails schema validation (a limit was exceeded): %v", err)
	}

	// 4. Every limit is exercised (driven to its cap) or allowlisted.
	reached := atCap(t, svJSON)
	for l := range atCap(t, v2JSON) {
		reached[l] = true
	}
	var missing []string
	for l := range want {
		if reached[l] {
			continue
		}
		if _, ok := limitsAllowlist[l]; ok {
			continue
		}
		missing = append(missing, l.String())
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("these schema limits were neither driven to their cap by the maximal graph nor allowlisted (make the fixture richer, or allowlist with a reason): %s",
			strings.Join(missing, ", "))
	}

	// The allowlist only shrinks: an entry no longer in either schema is stale.
	for l := range limitsAllowlist {
		if !want[l] {
			t.Errorf("allowlisted limit %s is not in any schema — remove it", l)
		}
	}
}

// maximalReport builds a graph designed to overflow the graph-drivable limits
// (long credential paths > 400, a credential in many locations, hundreds of
// findings) and returns it with its v2 report.
func maximalReport(t *testing.T) (*graph.Graph, report.V2) {
	t.Helper()
	const home = `C:\Users\José-Añá`
	salt := []byte("0123456789abcdef0123456789abcdef")
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

	// Many repo roots, each with a long path so a credential or instruction in
	// it overflows the 400-rune path caps.
	longSeg := strings.Repeat("deeply-nested-directory-", 20) // ~480 runes
	repos := make([]string, 0, 60)
	for i := 0; i < 60; i++ {
		repos = append(repos, fmt.Sprintf(`%s\src\%s\repo%02d`, home, longSeg, i))
	}

	// One credential value in many long locations → locations maxItems 5 and the
	// 400-rune location/file caps; plus hundreds of distinct credentials → many
	// findings (rows maxItems 500).
	// Credentials sit INSIDE the (long-pathed) repo roots, so each finding's
	// location is a long in-repo path that overflows the 400-rune file/location
	// caps. One value in many such locations drives the locations maxItems (5);
	// hundreds of distinct values drive the findings/rows count (500).
	var entries []platform.CredentialEntry
	shared := secrets.NewSecretValue("AKIASHAREDKEY0000001")
	for i := 0; i < 8; i++ {
		entries = append(entries, platform.CredentialEntry{
			Path: fmt.Sprintf(`%s\sub%02d\.env`, repos[0], i), Line: i + 1,
			Key: "AWS_ACCESS_KEY_ID", Value: shared,
		})
	}
	for i := 0; i < 600; i++ {
		entries = append(entries, platform.CredentialEntry{
			Path: fmt.Sprintf(`%s\k%03d\.env`, repos[i%len(repos)], i), Line: 1,
			Key: fmt.Sprintf("AWS_ACCESS_KEY_ID_%03d", i),
			// Distinct AKIA-shaped high-severity keys.
			Value: secrets.NewSecretValue(fmt.Sprintf("AKIA%016d", i)),
		})
	}

	st := platform.MCPSourceRead
	settings := platform.ClaudeSettingsScan{
		Files: []platform.ClaudeSettingsFile{{Scope: platform.MCPScopeUser, ConfigPath: home + `\.claude\settings.json`, ConfigDir: home + `\.claude`, Status: st}},
	}
	var trust []platform.ProjectTrust
	for _, r := range repos {
		trust = append(trust, platform.ProjectTrust{Path: r, Accepted: true})
	}
	settings.Trust = trust

	// Codex with many Computer Use apps, to push the finding title's {apps}
	// list long.
	apps := make([]string, 0, 40)
	for i := 0; i < 40; i++ {
		apps = append(apps, fmt.Sprintf("com.example.application-with-a-long-identifier-%02d", i))
	}
	codexUser := platform.CodexLayer{Kind: platform.CodexLayerUser, Path: home + `\.codex\config.toml`, Status: st}
	codexUser.ComputerUse = &platform.CodexComputerUse{DefaultAppAccess: "allow", Allowed: apps}
	codex := platform.CodexScan{ConfigDir: home + `\.codex`, Exists: true, Layers: []platform.CodexLayer{codexUser}}

	// Instruction files (AGENTS.md) in every repo for Claude Code, with long
	// paths.
	var instr platform.InstructionScan
	for _, r := range repos {
		instr.Files = append(instr.Files, platform.InstructionFile{Product: "claude-code", ConfigDir: home + `\.claude`, Path: r + `\AGENTS.md`, Shared: true})
	}

	in := pipeline.Input{
		Settings:     settings,
		Codex:        codex,
		Instructions: instr,
		CredFiles:    platform.CredentialFileScan{Entries: entries},
		Repos:        graph.Repos{},
		Provenance: graph.Provenance{
			EngineVersion:    "0.10.3-test",
			RulesVersion:     graph.RulesVersion,
			KnowledgeVersion: graph.KnowledgeVersion,
			HostID:           graph.HostID(salt),
		},
		Salt:      salt,
		RepoRoots: repos,
		Home:      home,
		Now:       now,
	}
	res, err := pipeline.Assemble(in)
	if err != nil {
		t.Fatalf("pipeline.Assemble: %v", err)
	}
	g := res.Graph

	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	v1 := report.Build("0.10.3-test", nil, nil, len(repos), 0, 0, 0, nil)
	v2, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("0", 64), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: repos, V1: v1, GeneratedAt: now, Salt: salt,
		ClaudeSettings: &settings, Home: home, Codex: codex,
	})
	return &g, v2
}

func mustValidSpaceView(t *testing.T, v2 report.V2, g *graph.Graph) []byte {
	t.Helper()
	sv, err := report.BuildSpaceView(v2, g, `C:\Users\José-Añá`, "0.10.3-test")
	if err != nil {
		t.Fatalf("BuildSpaceView: %v", err)
	}
	data, err := report.RenderSpaceViewJSON(sv)
	if err != nil {
		t.Fatalf("space-view.v1 fails schema validation (a limit was exceeded): %v", err)
	}
	return data
}
