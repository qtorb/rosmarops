package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/results"
	"github.com/qtorb/rosmarops/internal/schemas"
	"github.com/qtorb/rosmarops/internal/schemaval"
)

// TestFullMachinePipeline drives the full-machine fixture (every scored agent,
// 48 repos, Windows paths, non-ASCII user) through the real single assembly and
// the real report/space/package pipeline, and validates every output against
// its schema (CTO-24 E8). It is the mandatory CI gate on all three systems
// (ci.yml). It fails if a scored agent has no note, if the results package does
// not build, or if an agent that is present has a sources_failed entry.
func TestFullMachinePipeline(t *testing.T) {
	for _, configured := range []bool{true, false} {
		name := "detected_no_config"
		if configured {
			name = "with_config"
		}
		t.Run(name, func(t *testing.T) {
			runFullMachine(t, configured, codexAsIs)
		})
	}
	// BV-229 §2: Codex in danger-full-access with approval never, so its two
	// actions with nothing to paste reach the report's fix box: with the
	// trusted project, «untrusted project»; without it, «workspace sandbox».
	t.Run("with_config_codex_danger", func(t *testing.T) {
		runFullMachine(t, true, codexDangerTrusted)
	})
	t.Run("with_config_codex_danger_untrusted", func(t *testing.T) {
		runFullMachine(t, true, codexDangerUntrusted)
	})
}

// The Codex variants of the full machine (BV-229 §2).
const (
	codexAsIs = iota
	codexDangerTrusted
	codexDangerUntrusted
)

func runFullMachine(t *testing.T, configured bool, codexVariant int) {
	in, scans := fullMachineInput(configured)
	wantHow := ""
	if codexVariant != codexAsIs {
		// in.Codex and scans.Codex share their layers.
		user := &in.Codex.Layers[0]
		user.SandboxMode = "danger-full-access"
		user.ApprovalPolicy, user.HasApprovalPolicy = "never", true
		wantHow = "action.codex_untrusted_project.how"
		if codexVariant == codexDangerUntrusted {
			in.Codex.Layers[1].Trusted = false
			wantHow = "action.codex_workspace_sandbox.how"
		}
	}

	// 1. The graph, through the one assembly `rosmarops run` uses. Assemble
	// encodes and validates it against graph.v2; a bad graph is an error.
	res, err := pipeline.Assemble(in)
	if err != nil {
		t.Fatalf("pipeline.Assemble: %v", err)
	}
	g := res.Graph
	if len(res.Encoded) == 0 {
		t.Fatal("assembled graph encoded to nothing")
	}
	if len(g.Nodes) < 20 {
		t.Fatalf("only %d nodes — the full-machine fixture is not exercising the engine", len(g.Nodes))
	}
	// Every present agent's sources must be read, never left as a failure of a
	// present agent (E8).
	if n := len(g.Coverage.SourcesFailed); n > 0 {
		t.Errorf("full machine has %d sources_failed for present agents: %+v", n, g.Coverage.SourcesFailed)
	}
	// Hermes is detected only through its config file, so the detected-only
	// variant has eight scored agents (every one but Hermes); the configured
	// variant has all nine.
	want := fmScoredProducts
	if !configured {
		want = nil
		for _, p := range fmScoredProducts {
			if p != "hermes" {
				want = append(want, p)
			}
		}
	}
	var clients int
	for _, n := range g.Nodes {
		if n.Type == graph.AgentAIClient {
			clients++
		}
	}
	if clients < len(want) {
		t.Errorf("only %d ai_client nodes for an all-agents machine (want >= %d)", clients, len(want))
	}

	// 2. The v2 report. BuildV2 is pure; RenderV2JSON validates against
	// report.v2 before anything is written.
	bases, err := knowledge.All()
	if err != nil {
		t.Fatal(err)
	}
	v1 := report.Build("0.10.3-test", nil, nil, len(in.RepoRoots), 0, 0, 0, nil)
	// BV-235 R1: the factory pass, as `rosmarops run` builds it.
	fres, err := pipeline.Assemble(pipeline.Factory(in))
	if err != nil {
		t.Fatalf("pipeline.Assemble(Factory): %v", err)
	}
	v2, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: graphSHA(res.Encoded), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: in.RepoRoots, V1: v1, GeneratedAt: fmNow, Salt: fmSalt,
		ClaudeSettings: &scans.Settings, Home: fmHome,
		Hermes: scans.Hermes, Codex: scans.Codex, Grok: scans.Grok, Cline: scans.Cline,
		FactoryGraph: &fres.Graph,
	})

	// Every scored agent present on the machine has a note (an evaluated
	// overview row and a score). Missing one is the E8 failure.
	overview := map[string]bool{}
	prodByID := map[string]string{} // ai_client id -> product code
	for _, o := range v2.AgentsOverview {
		prodByID[o.AgentID] = o.Product
		if o.Evaluated {
			overview[o.Product] = true
		}
	}
	// AgentIndex.Agent is the display name, so the score is keyed to a product
	// through the overview's agent id.
	scored := map[string]bool{}
	for _, a := range v2.Index.Agents {
		if a.Value != nil {
			scored[prodByID[a.AgentID]] = true
		}
	}
	for _, p := range want {
		if !overview[p] {
			t.Errorf("full machine: %s has no evaluated overview row", p)
		}
		if !scored[p] {
			t.Errorf("full machine: %s has no score (note)", p)
		}
	}

	// report.v2 validates.
	v2JSON, err := report.RenderV2JSON(v2)
	if err != nil {
		t.Fatalf("report.v2 fails schema validation: %v", err)
	}
	if len(v2JSON) == 0 {
		t.Fatal("empty report.v2")
	}

	// 3. The v1 report validates against report.v1.
	v1JSON, err := report.RenderJSON(v1)
	if err != nil {
		t.Fatalf("render report.v1: %v", err)
	}
	validate(t, "report.v1", schemas.ReportSchema, v1JSON)

	// 4. The space view validates against space-view.v1 (it carries EN and ES).
	sv, err := report.BuildSpaceView(v2, &g, fmHome, "0.10.3-test")
	if err != nil {
		t.Fatalf("BuildSpaceView: %v", err)
	}
	svJSON, err := report.RenderSpaceViewJSON(sv)
	if err != nil {
		t.Fatalf("space-view.v1 fails schema validation: %v", err)
	}
	// BV-235: the sample report.v2.json and space package of the full test
	// computer, written only when asked (go generate-style, by hand).
	if dir := os.Getenv("ROSMAROPS_BV235_SAMPLE_DIR"); dir != "" && configured && codexVariant == codexAsIs {
		if err := os.WriteFile(filepath.Join(dir, "report.v2.json"), v2JSON, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "space-view.v1.json"), svJSON, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if len(svJSON) == 0 {
		t.Fatal("empty space view")
	}

	// BV-203 point 9 / CTO-30 A1,A3 (§4 point 4): the space's today-action
	// figures must equal the report's, compared against the report's own
	// function (report.TodayFigures), not a copy of the resolution in this test.
	// Today's finding on this fixture is cred_in_git — a machine finding in
	// every agent's score — so both must resolve to the lowest-scored agent; if
	// svAgentOfToday reverts to the first-in-index agent the two diverge and this
	// fails. The check is driven off the report: when the report resolves a
	// figure the space must show the same one and must not be absent, and vice
	// versa.
	from, to, ok := report.TodayFigures(v2)
	switch {
	case ok && sv.TopAction == nil:
		t.Errorf("the report resolves today's figures %d->%d but the space shows no top action", from, to)
	case !ok && sv.TopAction != nil:
		t.Errorf("the space shows a top action %d->%d but the report resolves no figures for today",
			sv.TopAction.ScoreFrom, sv.TopAction.ScoreTo)
	case ok && (sv.TopAction.ScoreFrom != from || sv.TopAction.ScoreTo != to):
		t.Errorf("space top action %d->%d differs from the report's %d->%d (svAgentOfToday resolved the wrong agent)",
			sv.TopAction.ScoreFrom, sv.TopAction.ScoreTo, from, to)
	}

	// 5. The results package builds and its sealed form validates against
	// results.v1.
	dir := t.TempDir()
	sec, err := kit.NewSecret(fmSalt)
	if err != nil {
		t.Fatal(err)
	}
	kitID, err := kit.NewKitID()
	if err != nil {
		t.Fatal(err)
	}
	pkgPath, err := report.WriteResultsPackage(v2, &g, fmHome, "0.10.3-test", sec.Key, kitID, kit.KeyID(sec.Key), fmNow, dir)
	if err != nil {
		t.Fatalf("WriteResultsPackage: %v", err)
	}
	if pkgPath == "" {
		t.Fatal("no results package was written for the full machine")
	}
	pkgRaw, err := os.ReadFile(pkgPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := results.Parse(pkgRaw); err != nil {
		t.Fatalf("sealed package does not parse: %v", err)
	}
	validate(t, "results.v1", schemas.ResultsSchema, pkgRaw)

	// 6. The HTML pages render in each language (EN and ES), as each of the
	// three systems shows them; BV-229 §4: none carries a key without its
	// text, and no «by hand» block or copy text is empty.
	for _, goos := range []string{"windows", "darwin", "linux"} {
		v2.OS = goos
		for _, lang := range []string{"en", "es"} {
			htmlDir := filepath.Join(dir, goos, lang)
			if err := os.MkdirAll(htmlDir, 0o755); err != nil {
				t.Fatal(err)
			}
			p, err := report.WriteHTMLPages(v2, lang, fmHome, nil, htmlDir)
			if err != nil {
				t.Fatalf("WriteHTMLPages(%s, %s): %v", goos, lang, err)
			}
			if st, err := os.Stat(p); err != nil || st.Size() == 0 {
				t.Errorf("WriteHTMLPages(%s, %s) produced no page at %s", goos, lang, p)
			}
			checkPagesShown(t, goos+"/"+lang, htmlDir)
			if wantHow != "" {
				checkCodexHow(t, goos+"/"+lang, lang, wantHow, htmlDir)
			}
		}
	}
}

// checkCodexHow: with Codex in danger-full-access, the report tells its
// change with the action's .how text, key (BV-229 §2).
func checkCodexHow(t *testing.T, name, lang, key, dir string) {
	t.Helper()
	cat, err := catalog.Load(lang)
	if err != nil {
		t.Fatal(err)
	}
	how := cat.Text(key, map[string]string{"file": "\x00", "agent": "\x00"})
	lead := html.EscapeString(how[:strings.Index(how, "\x00")])
	var pages strings.Builder
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		raw, err := os.ReadFile(path)
		pages.Write(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(pages.String(), lead) {
		t.Errorf("%s: the report does not tell Codex's change with %s (%q)", name, key, lead)
	}
}

// untranslatedKey is a catalog key shown in place of its text: the catalog
// renders a key it has no text for as "[key]".
var untranslatedKey = regexp.MustCompile(`\[(?:html|report|console|[a-z][a-z0-9_]*)\.[a-zA-Z0-9_.-]+\]`)

// emptyBlock is a «by hand» block whose content to paste is empty, or a copy
// text with nothing after its instruction (BV-229 §2).
var emptyBlock = regexp.MustCompile(`<pre>\s*</pre>|<pre hidden>[^<]*?\n\n\s*</pre>|<pre>[^<]*?\n\n\s*</pre>`)

// numberedPlural matches «1 » followed by a plural word that the catalog puts
// after a number marker ({count}, {repos}, {roots}, {found}, {n}, {left},
// {projects}, {groups}) in English or Spanish (BV-232 §7, extending BV-229 §4).
var numberedPlural = func() *regexp.Regexp {
	marker := regexp.MustCompile(`\{(?:count|repos|roots|found|n|left|projects|groups)\}\s+(\p{L}+)`)
	words := map[string]bool{}
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			panic(err)
		}
		keys, err := catalog.Keys()
		if err != nil {
			panic(err)
		}
		for _, k := range keys {
			for _, m := range marker.FindAllStringSubmatch(cat.Text(k, nil), -1) {
				w := m[1]
				// A plural noun or adjective: ends in s, and is not a word that
				// does with one too.
				if strings.HasSuffix(w, "s") && !map[string]bool{"más": true, "has": true, "was": true, "is": true}[w] {
					words[w] = true
				}
			}
		}
	}
	alts := make([]string, 0, len(words))
	for w := range words {
		alts = append(alts, regexp.QuoteMeta(w))
	}
	sort.Strings(alts)
	return regexp.MustCompile(`(?:^|[\s>(])1 (` + strings.Join(alts, "|") + `)\b`)
}()

// checkPagesShown is BV-229 §4 on every page written in dir.
func checkPagesShown(t *testing.T, name, dir string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, k := range untranslatedKey.FindAllString(string(raw), -1) {
			t.Errorf("%s: %s shows %s without its text", name, filepath.Base(path), k)
		}
		if i := strings.Index(string(raw), "&amp;#"); i >= 0 {
			t.Errorf("%s: %s escapes a text twice: %q", name, filepath.Base(path), raw[max(0, i-40):i+10])
		}
		if m := numberedPlural.FindString(string(raw)); m != "" {
			t.Errorf("%s: %s says %q: a count of one in a plural form", name, filepath.Base(path), strings.TrimSpace(m))
		}
		if m := emptyBlock.FindString(string(raw)); m != "" {
			t.Errorf("%s: %s has an empty block to paste: %q", name, filepath.Base(path), m)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// graphSHA is the lowercase-hex SHA-256 of the encoded graph, as the report
// records the hash of the graph file it evaluated.
func graphSHA(encoded []byte) string {
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// validate compiles schema and checks data against it, failing the test with
// the field paths (no values) on any violation.
func validate(t *testing.T, name string, schema, data []byte) {
	t.Helper()
	s, err := schemaval.Compile(schema)
	if err != nil {
		t.Fatalf("compile %s schema: %v", name, err)
	}
	r, err := s.Validate(data)
	if err != nil {
		t.Fatalf("validate %s: %v", name, err)
	}
	if !r.Valid() {
		var fields []string
		for _, e := range r.Errors() {
			fields = append(fields, e.String())
		}
		t.Fatalf("%s fails schema validation: %v", name, fields)
	}
}
