package report_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

var v2Now = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

// v2Graph is Claude Code with no defaultMode (built-in auto), Cursor, a
// CLAUDE.md, a Supabase service_role key and a template database URL.
func v2Graph(t *testing.T) graph.Graph {
	t.Helper()
	return v2GraphWith(t, []platform.CredentialEntry{
		{Path: "/src/a/dashboard/.env.local", Line: 4, Key: "SUPABASE_SERVICE_ROLE_KEY", Value: secrets.NewSecretValue(serviceRoleJWT)},
		{Path: "/src/b/.env.example", Line: 10, Key: "DATABASE_URL", Value: secrets.NewSecretValue("postgres://app:apppass@db.example.com:5432/app")},
	}, []string{"/src/a/CLAUDE.md"})
}

func seg(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }

var serviceRoleJWT = seg(`{"alg":"HS256","typ":"JWT"}`) + "." + seg(`{"iss":"supabase","ref":"abcdefghijklmnopqrst","role":"service_role"}`) + ".ZmFrZS1zaWduYXR1cmUtZmlsbGVy"

// v2GraphWith is v2Graph with the given credential entries and CLAUDE.md
// files.
func v2GraphWith(t *testing.T, entries []platform.CredentialEntry, instructions []string) graph.Graph {
	t.Helper()
	return claudeCodeOnly(v2GraphFull(t, entries, instructions))
}

// v2GraphFull is v2GraphWith with Cursor's inferred edges (BV-48).
func v2GraphFull(t *testing.T, entries []platform.CredentialEntry, instructions []string) graph.Graph {
	t.Helper()
	salt := []byte("0123456789abcdef0123456789abcdef")
	mcp := platform.MCPScan{Sources: []platform.MCPSource{{Platform: "cursor", ConfigPath: "/h/.cursor/mcp.json", ConfigDir: "/h/.cursor", Status: platform.MCPSourceRead, LoadsMCP: true}}}
	settings := platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{{
		Scope: platform.MCPScopeUser, ConfigPath: "/h/.claude/settings.json", ConfigDir: "/h/.claude", Status: platform.MCPSourceRead,
		Permissions: platform.ClaudePermissions{Allow: []string{"Bash(npx tsx *)"}},
	}}}
	cf := platform.CredentialFileScan{Entries: entries}
	f, _, err := graph.FromScans(mcp, settings, cf, salt, v2Now)
	if err != nil {
		t.Fatal(err)
	}
	var files []platform.InstructionFile
	for _, p := range instructions {
		files = append(files, platform.InstructionFile{Product: "claude-code", ConfigDir: "/h/.claude", Path: p})
	}
	f = graph.AddInstructions(f, platform.InstructionScan{Files: files}, v2Now)
	g, err := graph.Infer(graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Version, HostID: graph.HostID(salt)}, v2Now), []string{"/src/a", "/src/b"}, "/h")
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// claudeCodeOnly drops the edges Cursor's inference added (BV-48): these
// fixtures pin the report of Claude Code's findings. Cursor stays as a
// target; Cursor's report has its tests.
func claudeCodeOnly(g graph.Graph) graph.Graph {
	cursor := map[string]bool{}
	for _, n := range g.Nodes {
		cursor[n.ID] = n.Kind == graph.KindAgent && n.Platform == knowledge.CursorProduct
	}
	edges := g.Edges[:0:0]
	for _, e := range g.Edges {
		if !cursor[e.From] || e.Type != graph.EdgeReaches && e.Type != graph.EdgeWritesConfigOf {
			edges = append(edges, e)
		}
	}
	g.Edges = edges
	return g
}

func buildV2(t *testing.T, g *graph.Graph, v1 report.Report) report.V2 {
	t.Helper()
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	v, _ := report.BuildV2(report.V2Input{
		Graph: g, GraphSHA256: strings.Repeat("ab", 32), Base: base, RepoRoots: []string{"/src/a", "/src/b"},
		Families: [][]string{{"/src/a"}, {"/src/b"}}, V1: v1, GeneratedAt: v2Now,
	})
	return v
}

// reservedKeys are signed texts the binary cannot emit: the launcher's
// message (a .bat cannot read the catalog) and the HTML's legacy section
// (gone from the HTML in informe HTML v2 §3).
var reservedKeys = []string{
	"html.console.open",
	// agents_overview carries session_env_not_readable as a code; the HTML
	// says it with html.ov.data.session_env (v3 §11.4, line 9)
	"unknown.session_env_not_readable",
	// the notification says where the report is only if its click cannot
	// open it (decision 2026-10-26, integridad y revisión semanal, J7):
	// pending the check on Windows
	"notify.where",
	// gone from the HTML in informe HTML v3 §17 (the bottom strip's
	// titles and notes, and the score's series); the .ai.md keeps its texts
	"html.memory.title", "html.memory.evolution", "html.memory.evolution.agent", "html.unknown.title",
	"html.ai.title", "html.ai.note", "html.card.title", "html.card.note",
	"html.legacy.title", "html.legacy.body", "html.legacy.none",
	// gone from the HTML in informe HTML v3 (traffic light, counters, today
	// tag and chip)
	"html.status.sub", "html.count.red", "html.count.yellow", "html.counts.label",
	"html.today.tag", "html.today.time", "html.today.chip",
	// gone from the footer: the agents not evaluated are in step 1 (v3 §13.1)
	"html.index.not_evaluated",
	// BV-93, B2: own_task_changed is a legacy scheduled-task finding whose text
	// the scanner bakes in English; localizing it would need internal/report/
	// changes, out of B2's scope, so its catalog texts are signed but not yet
	// emitted through the catalog.
	"finding.own_task_changed.title", "finding.own_task_changed.why", "finding.own_task_changed.action",
}

// allEmitted is every code the Markdown, the console and the HTML can emit,
// and the reserved ones, sorted.
func allEmitted() []string {
	seen := map[string]bool{}
	var out []string
	for _, ks := range [][]string{report.CatalogKeys(), report.HTMLKeys(), cli.HelpKeys, reservedKeys} {
		for _, k := range ks {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}

// E11: every code the Markdown can emit is in messages/en.json, and the
// catalog has no code the Markdown never emits.
func TestCatalogCoverage(t *testing.T) {
	keys, err := catalog.Keys()
	if err != nil {
		t.Fatal(err)
	}
	if want := allEmitted(); !reflect.DeepEqual(keys, want) {
		have := map[string]bool{}
		for _, k := range keys {
			have[k] = true
		}
		for _, k := range want {
			if !have[k] {
				t.Errorf("messages/en.json lacks %q", k)
			}
			delete(have, k)
		}
		for k := range have {
			t.Errorf("messages/en.json has %q, which nothing emits", k)
		}
	}
}

// stripCode removes code blocks, code spans and <code> elements: data (paths,
// rules, JSON fragments), not catalog text.
func stripCode(md string) string {
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile("(?s)```.*?```"), regexp.MustCompile("`[^`]*`"), regexp.MustCompile(`(?s)<code>.*?</code>`),
	} {
		md = re.ReplaceAllString(md, "")
	}
	return md
}

// BV-38: with the signed texts, no rendered text keeps a placeholder ("{")
// or falls back to its key ("[code]"); the order of §6 holds.
func TestRenderV2Markdown_Texts(t *testing.T) {
	g := v2Graph(t)
	v1 := report.Build("t", []finding.Finding{
		{ID: "secret_tracked:1", Severity: finding.SeverityRed, Category: "secret_tracked", GroupDetail: "github_pat_classic", Evidence: finding.Evidence{RepoPath: "/src/a", File: "dashboard/.env.local"}},
	}, nil, 0, 0, 0, 0, nil)
	v := buildV2(t, &g, v1)
	cat, err := catalog.Load("")
	if err != nil {
		t.Fatal(err)
	}
	md := report.RenderV2Markdown(v, cat, time.UTC)
	assertRenderedTexts(t, md)
	last := -1
	for _, k := range []string{"report.status.red", "section.today", "section.findings", "section.posture", "section.coverage", "section.legacy"} {
		i := strings.Index(md, cat.Text(k, nil))
		if i <= last {
			t.Errorf("%s out of order", k)
		}
		last = i
	}
	for _, k := range []string{"finding.part.what", "finding.part.why", "finding.part.todo", "finding.part.unknown", "finding.part.evidence"} {
		if n := strings.Count(md, "#### "+cat.Text(k, nil)+"\n"); n != len(v.Findings) {
			t.Errorf("%s: %d, want one per finding (%d)", k, n, len(v.Findings))
		}
	}
	if !strings.Contains(md, `"blockReadsOutsideWorkingDirectories": true`) || !strings.Contains(md, "`/h/.claude/settings.json`") {
		t.Errorf("today's change not rendered:\n%s", md)
	}
	// M5: secret_tracked passes to v2 (cred_in_git): not in the technical
	// report.
	if strings.Contains(md[strings.Index(md, cat.Text("section.legacy", nil)):], "secret_tracked") {
		t.Errorf("secret_tracked still in the technical report")
	}
}

// assertRenderedTexts fails on a placeholder left in, or a key rendered for
// a missing text, outside code.
func assertRenderedTexts(t *testing.T, md string) {
	t.Helper()
	text := stripCode(md)
	if i := strings.Index(text, "{"); i >= 0 {
		t.Errorf("placeholder left: %q", text[max(0, i-60):min(len(text), i+40)])
	}
	if m := regexp.MustCompile(`\[[a-z][a-z0-9_]*(\.[a-z0-9_]+)+\]`).FindString(text); m != "" {
		t.Errorf("catalog key rendered: %s", m)
	}
}

// BV-38: {cred_label} in its three forms, and {service_name}.
func TestRenderV2Markdown_CredentialLabels(t *testing.T) {
	plain := seg(`{"alg":"HS256","typ":"JWT"}`) + "." + seg(`{"sub":"user-1","name":"x"}`) + ".ZmFrZS1zaWduYXR1cmUtZmlsbGVy"
	g := v2GraphWith(t, []platform.CredentialEntry{
		{Path: "/src/a/.env", Line: 1, Key: "SUPABASE_SERVICE_ROLE_KEY", Value: secrets.NewSecretValue(serviceRoleJWT)},
		{Path: "/src/a/.env", Line: 2, Key: "DATABASE_URL", Value: secrets.NewSecretValue("postgres://app:apppass@db.example.com:5432/app")},
		{Path: "/src/a/.env", Line: 3, Key: "OPENROUTER_API_KEY", Value: secrets.NewSecretValue("sk-or-v1-" + strings.Repeat("a", 64))},
		{Path: "/src/b/.env", Line: 1, Key: "SESSION_TOKEN", Value: secrets.NewSecretValue(plain)}, // another file: not merged with the db credential
	}, nil)
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	cat, _ := catalog.Load("")
	md := report.RenderV2Markdown(v, cat, time.UTC)
	assertRenderedTexts(t, md)
	for _, want := range []string{
		"can read your Supabase service_role key and send it out",
		"can read your database credential and send it out",
		"can read your OpenRouter key and send it out",
		"can read your secret and send it out",
		"whatever Supabase lets this key do",
		"whatever the service it belongs to lets this key do",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// BV-38: a path with a pattern character gets no ask rule; the other paths
// and the other actions stay.
func TestBuildV2_NoRuleForPatternPaths(t *testing.T) {
	g := v2GraphWith(t, []platform.CredentialEntry{
		{Path: "/src/a/app[1]/.env", Line: 1, Key: "SUPABASE_SERVICE_ROLE_KEY", Value: secrets.NewSecretValue(serviceRoleJWT)},
	}, []string{"/src/a/CLAUDE.md", "/src/a/docs*/CLAUDE.md"})
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	for _, f := range v.Findings {
		var codes []string
		for _, a := range f.Actions {
			codes = append(codes, a.Code)
			for _, r := range a.Rules {
				if strings.ContainsAny(strings.TrimSuffix(strings.SplitN(r, "(", 2)[1], ")"), "*?[]") {
					t.Errorf("%s: rule %s", f.Rule, r)
				}
			}
		}
		switch f.Rule {
		case rules.RuleCredReadAndSend:
			if strings.Contains(strings.Join(codes, ","), rules.ActionAskReadRule) || !strings.Contains(strings.Join(codes, ","), rules.ActionRemoveFromDisk) {
				t.Errorf("credential actions %v", codes)
			}
		case rules.RuleOwnInstructionsWritable:
			for _, a := range f.Actions {
				if a.Code == rules.ActionAskEditRule && (!reflect.DeepEqual(a.Rules, []string{"Edit(//src/a/CLAUDE.md)"}) || a.Completeness == rules.CompletenessFull) {
					t.Errorf("edit rule %+v", a)
				}
			}
		}
	}
}

// report.v2.json validates against its schema, with and without a graph.
func TestRenderV2JSON_ValidatesSchema(t *testing.T) {
	g := v2Graph(t)
	for name, v := range map[string]report.V2{
		"graph":    buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil)),
		"no graph": buildV2(t, nil, report.Build("t", nil, []finding.ScanError{{Scope: "graph", Message: "x"}}, 0, 0, 0, 0, nil)),
	} {
		out, err := report.RenderV2JSON(v)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		var back map[string]any
		if err := json.Unmarshal(out, &back); err != nil || back["schema_version"] != float64(2) {
			t.Errorf("%s: %v %v", name, err, back["schema_version"])
		}
	}
	bad := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	bad.Status = "blue"
	if _, err := report.RenderV2JSON(bad); err == nil {
		t.Error("invalid report validated")
	}
}

// Status is the highest of v1 and v2; no graph is at least yellow
// (fail-secure). Green needs both green.
func TestBuildV2_Status(t *testing.T) {
	g := v2Graph(t)
	green := report.Build("t", nil, nil, 0, 0, 0, 0, nil)
	if v := buildV2(t, &g, green); v.Status != "red" || v.Legacy.Status != "green" {
		t.Errorf("v1 green, v2 red: %s (legacy %s)", v.Status, v.Legacy.Status)
	}
	if v := buildV2(t, nil, green); v.Status != "yellow" || v.Coverage.GraphBuilt || v.Today.Code != rules.TodayNothingUrgent {
		t.Errorf("no graph: %s %+v", v.Status, v.Today)
	}
	empty := graph.Build(graph.Fragment{}, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: "k", HostID: "h"}, v2Now)
	if v := buildV2(t, &empty, green); v.Status != "green" {
		t.Errorf("both green: %s", v.Status)
	}
	red := report.Build("t", []finding.Finding{{ID: "x:1", Severity: finding.SeverityRed, Category: "x"}}, nil, 0, 0, 0, 0, nil)
	if v := buildV2(t, &empty, red); v.Status != "red" {
		t.Errorf("v1 red: %s", v.Status)
	}
}

// §3: v1 findings group by (category, pattern or detail, family); the
// group takes the highest severity; findings outside the repository checks
// stay one per group.
func TestBuildV2_LegacyGroups(t *testing.T) {
	f := func(id, cat, detail, repo string, sev finding.Severity) finding.Finding {
		return finding.Finding{ID: id, Category: cat, GroupDetail: detail, Severity: sev, Evidence: finding.Evidence{RepoPath: repo}}
	}
	v1 := report.Build("t", []finding.Finding{
		f("wf:1", "workflow_dangerous_pattern", "pull_request_target", "/src/a", finding.SeverityRed),
		f("wf:2", "workflow_dangerous_pattern", "pull_request_target", "/src/a2", finding.SeverityRed),
		f("wf:3", "workflow_dangerous_pattern", "pull_request_target", "/src/b", finding.SeverityRed),
		f("wf:4", "workflow_dangerous_pattern", "curl_pipe_shell", "/src/a", finding.SeverityRed),
		f("hn:1", "git_hook_with_network", "pre-commit", "/src/a", finding.SeverityYellow),
		f("tk:1", "scheduled_task_new", "", "", finding.SeverityYellow),
		f("tk:2", "scheduled_task_new", "", "", finding.SeverityYellow),
		// M5: passed to v2, or retired: not in the technical report.
		f("st:1", "secret_tracked", "github_pat_classic", "/src/a", finding.SeverityRed),
		f("ht:1", "git_hook_new_or_modified", "pre-commit", "/src/a", finding.SeverityYellow),
		f("mcp:1", "mcp_server_new", "", "", finding.SeverityYellow),
	}, nil, 0, 0, 0, 0, nil)
	base, _ := knowledge.ClaudeCode()
	v, _ := report.BuildV2(report.V2Input{Base: base, V1: v1, GeneratedAt: v2Now, RepoRoots: []string{"/src/a", "/src/a2", "/src/b"}, Families: [][]string{{"/src/a", "/src/a2"}, {"/src/b"}}})
	var got []string
	for _, g := range v.Legacy.Groups {
		got = append(got, strings.Join([]string{g.Severity, g.Category, g.Detail, g.Family, strings.Join(g.Repos, "+")}, " "))
	}
	want := []string{
		"red workflow_dangerous_pattern curl_pipe_shell /src/a /src/a",
		"red workflow_dangerous_pattern pull_request_target /src/a /src/a+/src/a2",
		"red workflow_dangerous_pattern pull_request_target /src/b /src/b",
		"yellow git_hook_with_network pre-commit /src/a /src/a",
		"yellow scheduled_task_new tk:1  ",
		"yellow scheduled_task_new tk:2  ",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("groups:\n%s", strings.Join(got, "\n"))
	}
	if v.Legacy.Counts.Red != 4 || v.Legacy.Counts.Yellow != 3 {
		t.Errorf("counts %+v", v.Legacy.Counts)
	}
}

// The Markdown file name keeps its format, with the run's status and the
// counts of v1 and v2; the files go to the reports directory only.
func TestWriteV2(t *testing.T) {
	g := v2Graph(t)
	v1 := report.Build("t", nil, nil, 0, 0, 0, 0, nil)
	v := buildV2(t, &g, v1)
	dir := t.TempDir()
	md, js, v2, err := report.WriteV2(v1, v, time.UTC, dir)
	if err != nil {
		t.Fatal(err)
	}
	// 3 red since BV-64: a launcher writes, own_config_writable (L1).
	if want := "report-" + v1.GeneratedAt[:10] + "__RED__3R-1Y"; filepath.Base(md) != want+".md" || filepath.Base(js) != want+".json" || filepath.Base(v2) != report.V2FileName {
		t.Errorf("files %s %s %s", md, js, v2)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Errorf("%d files in the reports directory", len(entries))
	}
	raw, _ := os.ReadFile(js)
	var back report.Report
	if err := json.Unmarshal(raw, &back); err != nil || back.SchemaVersion != 1 || back.Header.Status != "green" {
		t.Errorf("v1 sidecar changed: %v %+v", err, back.Header)
	}
}

// BV-38: a legacy group whose file holds a credential v2 reports is
// also_in_v2; one in another file is not.
func TestBuildV2_LegacyDedupByFile(t *testing.T) {
	g := v2Graph(t)
	// Technical-report checks (M5) in a file v2 reports a credential of,
	// in another, and in a template.
	v1 := report.Build("t", []finding.Finding{
		{ID: "x:1", Severity: finding.SeverityRed, Category: "unknown_check", GroupDetail: "a", Evidence: finding.Evidence{RepoPath: "/src/a", File: "dashboard/.env.local"}},
		{ID: "x:2", Severity: finding.SeverityRed, Category: "unknown_check", GroupDetail: "b", Evidence: finding.Evidence{RepoPath: "/src/a", File: "config.py"}},
		{ID: "x:3", Severity: finding.SeverityRed, Category: "unknown_check", GroupDetail: "c", Evidence: finding.Evidence{RepoPath: "/src/b", File: ".env.example"}},
	}, nil, 0, 0, 0, 0, nil)
	v := buildV2(t, &g, v1)
	got := map[string]bool{}
	for _, lg := range v.Legacy.Groups {
		got[lg.FindingIDs[0]] = lg.AlsoInV2
	}
	// .env.example holds only a template credential: posture, not a finding.
	if want := map[string]bool{"x:1": true, "x:2": false, "x:3": false}; !reflect.DeepEqual(got, want) {
		t.Errorf("also_in_v2 %v, want %v", got, want)
	}
	if _, err := report.RenderV2JSON(v); err != nil {
		t.Error(err)
	}
}

// BV-39: the four common assumptions are shown once, in section.assumptions
// before the findings; each finding lists only the others. With two agents,
// one list per agent under its name.
func TestRenderV2Markdown_SharedAssumptions(t *testing.T) {
	cat, _ := catalog.Load("")
	common := []string{graph.AssumeBashToolAvailable, graph.AssumeNoPreToolUseHookBlocks, graph.AssumeProcessRunsAsUser, graph.AssumeVersionAsDocumented}
	mk := func(id, agent string, extra ...string) rules.Finding {
		return rules.Finding{
			ID: id, Rule: rules.RuleOwnInstructionsWritable, Severity: rules.SeverityYellow, Confidence: rules.ConfidenceMedium,
			Params:   rules.Params{Agent: agent, Count: 1},
			Evidence: rules.Evidence{Nodes: []string{"a"}, Assumptions: append(append([]string{}, common...), extra...)},
		}
	}
	section := cat.Text("section.assumptions", nil)
	line := func(a, agent string) string {
		return "- " + cat.Text("unknown."+a, map[string]string{"agent": agent}) + "\n"
	}

	v := report.V2{Status: "yellow", GeneratedAt: "2026-10-24T12:00:00Z", Findings: []rules.Finding{
		mk("f_0000000000000001", "Claude Code", graph.AssumeAutoClassifierPermits),
		mk("f_0000000000000002", "Claude Code"),
	}}
	md := report.RenderV2Markdown(v, cat, time.UTC)
	s, f := strings.Index(md, "## "+section), strings.Index(md, "## "+cat.Text("section.findings", nil))
	if s < 0 || s > f {
		t.Fatalf("no shared section before the findings:\n%s", md)
	}
	for _, a := range common {
		if n := strings.Count(md, line(a, "Claude Code")); n != 1 {
			t.Errorf("%s shown %d times", a, n)
		}
	}
	if !strings.Contains(md[f:], line(graph.AssumeAutoClassifierPermits, "Claude Code")) {
		t.Error("finding-specific assumption missing")
	}
	assertRenderedTexts(t, md)

	v.Findings = append(v.Findings, mk("f_0000000000000003", "Cursor"))
	md = report.RenderV2Markdown(v, cat, time.UTC)
	shared := md[strings.Index(md, "## "+section):strings.Index(md, "## "+cat.Text("section.findings", nil))]
	if !strings.Contains(shared, "### Claude Code\n") || !strings.Contains(shared, "### Cursor\n") ||
		!strings.Contains(shared, line(graph.AssumeProcessRunsAsUser, "Cursor")) {
		t.Errorf("per-agent lists:\n%s", shared)
	}
}

// BV-39: legacy rows list what identifies each finding; a hashed id detail
// becomes the readable identifier; never a preview.
func TestBuildV2_LegacyMembers(t *testing.T) {
	ev := func(e finding.Evidence) finding.Evidence { return e }
	v1 := report.Build("t", []finding.Finding{
		{ID: "st:1", Severity: finding.SeverityRed, Category: "secret_tracked", GroupDetail: "github_pat_classic", Evidence: ev(finding.Evidence{RepoPath: "/src/a", File: "config.py", RedactedPreview: "ghp_ ... ZZZZ"})},
		{ID: "sh:1", Severity: finding.SeverityRed, Category: "secret_in_history", GroupDetail: "github_pat_classic", Evidence: ev(finding.Evidence{RepoPath: "/src/a", CommitSHA: "0123456789abcdef0123", RedactedPreview: "ghp_ ... ZZZZ"})},
		{ID: "vscode_extension_new:196450aa", Severity: finding.SeverityYellow, Category: "vscode_extension_new", Evidence: ev(finding.Evidence{ExtensionID: "ms-python.python", PackageVersion: "1.0"})},
		{ID: "mcp_server_new:77aa", Severity: finding.SeverityYellow, Category: "mcp_server_new", Evidence: ev(finding.Evidence{MCPServer: "fs", MCPClient: "cursor", Command: "npx"})},
		{ID: "scheduled_task_new:88bb", Severity: finding.SeverityYellow, Category: "scheduled_task_new", Evidence: ev(finding.Evidence{Extra: map[string]string{"task_name": "Updater", "task_path": `\x\`}})},
		{ID: "global_package_new:99cc", Severity: finding.SeverityYellow, Category: "global_package_new", Evidence: ev(finding.Evidence{Extra: map[string]string{"package_manager": "npm", "package_name": "tsx", "package_version": "4.0.0"}})},
		{ID: "npm_registry_non_standard:0", Severity: finding.SeverityRed, Category: "npm_registry_non_standard", Evidence: ev(finding.Evidence{Extra: map[string]string{"registry_url": "https://u:p@r.example", "npmrc_path": "/h/.npmrc"}})},
	}, nil, 0, 0, 0, 0, nil)
	base, _ := knowledge.ClaudeCode()
	v, _ := report.BuildV2(report.V2Input{Base: base, V1: v1, GeneratedAt: v2Now, RepoRoots: []string{"/src/a"}})
	details := map[string]string{}
	for _, g := range v.Legacy.Groups {
		details[g.Category] = g.Detail
	}
	for cat, want := range map[string]string{
		"vscode_extension_new": "ms-python.python", "scheduled_task_new": "Updater",
		"global_package_new": "npm tsx@4.0.0", "npm_registry_non_standard": "/h/.npmrc",
	} {
		if details[cat] != want {
			t.Errorf("%s detail %q, want %q", cat, details[cat], want)
		}
	}
	// M5: the repository secrets and a new MCP server pass to v2.
	for _, gone := range []string{"secret_tracked", "secret_in_history", "mcp_server_new"} {
		if _, ok := details[gone]; ok {
			t.Errorf("%s still in the technical report", gone)
		}
	}
	// BV-59, A1: a v1 mcp_server_new is not a new MCP server; the memory
	// says what is new.
	if len(v.NewAgents) != 0 || len(v.NewMCP) != 0 {
		t.Errorf("new agents %v, new MCP servers %v", v.NewAgents, v.NewMCP)
	}
	cat, _ := catalog.Load("")
	md := report.RenderV2Markdown(v, cat, time.UTC)
	if strings.Contains(md, cat.Text("memory.new_mcp", map[string]string{"name": "fs"})) {
		t.Error("a v1 mcp_server_new is a new MCP server")
	}
	for _, want := range []string{"- `ms-python.python`", "- `Updater`"} {
		if !strings.Contains(md, want) {
			t.Errorf("legacy member %s missing", want)
		}
	}
	for _, never := range []string{"ghp_ ...", "u:p@", "196450aa"} {
		if strings.Contains(md, never) {
			t.Errorf("markdown contains %q", never)
		}
	}
}

// BV-39: the file name counts v2 only, with the global status; the console
// shows the catalog's two lines.
func TestFilenameAndConsole(t *testing.T) {
	g := v2Graph(t)
	v1 := report.Build("t", []finding.Finding{{ID: "x:1", Severity: finding.SeverityRed, Category: "x"}, {ID: "y:1", Severity: finding.SeverityYellow, Category: "y"}}, nil, 0, 0, 0, 0, nil)
	v := buildV2(t, &g, v1)
	if got, want := report.FilenameV2(v1, v), "report-"+v1.GeneratedAt[:10]+"__RED__3R-1Y"; got != want {
		t.Errorf("file name %s, want %s", got, want)
	}
	// BV-46: «Label: n», the status followed by a space, never ".:".
	for lang, want := range map[string]string{
		"en": "Red: 3 · Yellow: 1. Legacy checks — red: 1 · yellow: 1.\nReport: /r/report.md\n",
		"es": "Urgentes: 3 · Para revisar: 1. Comprobaciones antiguas — urgentes: 1 · para revisar: 1.\nInforme: /r/report.md\n",
	} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		got := report.ConsoleSummary(v, cat, "/r/report.md")
		if want = cat.Text("report.status.red", nil) + " " + want; got != want {
			t.Errorf("%s console:\n%q\nwant\n%q", lang, got, want)
		}
		for _, status := range []string{"green", "yellow", "red"} {
			w := v
			w.Status = status
			line := report.ConsoleSummary(w, cat, "/r/report.md")
			if strings.Contains(line, ".:") || !strings.HasPrefix(line, cat.Text("report.status."+status, nil)+" ") {
				t.Errorf("%s %s console: %q", lang, status, line)
			}
		}
	}
}

// BV-40: en.json is the two signed English copies joined; es.json is the
// signed Spanish copy, byte for byte.
func TestCatalogFilesAreTheSignedCopy(t *testing.T) {
	read := func(p string) []byte {
		b, err := os.ReadFile(filepath.Join("..", "..", p))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if string(read("internal/catalog/messages/es.json")) != string(read("docs/copy/report-html-es.json")) {
		t.Error("es.json differs from docs/copy/report-html-es.json")
	}
	want := map[string]string{}
	for _, p := range []string{"docs/copy/report-v2-en.json", "docs/copy/report-html-en.json"} {
		var m map[string]string
		if err := json.Unmarshal(read(p), &m); err != nil {
			t.Fatal(err)
		}
		for k, v := range m {
			if old, dup := want[k]; dup && old != v {
				t.Errorf("%s: two texts", k)
			}
			want[k] = v
		}
	}
	var got map[string]string
	if err := json.Unmarshal(read("internal/catalog/messages/en.json"), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("en.json is not the two English copies joined (%d keys, want %d)", len(got), len(want))
	}
}

// §9: every html.*, band.* and console.* key the HTML or the console can
// emit, and every cli.* help text (BV-49), exists in es; only the legacy
// rows may fall back to English (BV-41: the unknown.* texts are translated).
func TestSpanishCoverage(t *testing.T) {
	es, err := catalog.Load("es")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range append(append(report.HTMLKeys(), cli.HelpKeys...), "console.summary", "console.report", "report.status.red", "report.status.yellow", "report.status.green") {
		if es.HasOwn(k) || report.LegacyHTMLKeys[k] {
			continue
		}
		t.Errorf("es lacks %s", k)
	}
}

// A graph with agents and no finding gives a valid report (findings: []).
func TestBuildV2_NoFindingIsValid(t *testing.T) {
	g := v2GraphWith(t, nil, nil)
	g.Edges = nil // no inferred edge: no finding
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	if v.Findings == nil {
		t.Fatal("findings is nil")
	}
	if _, err := report.RenderV2JSON(v); err != nil {
		t.Error(err)
	}
}
