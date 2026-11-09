package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/results"
	"github.com/qtorb/rosmarops/internal/rules"
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
			runFullMachine(t, configured)
		})
	}
}

func runFullMachine(t *testing.T, configured bool) {
	in, scans := fullMachineInput(configured)

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
	v2, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: graphSHA(res.Encoded), Base: bases[knowledge.ClaudeCodeProduct], Bases: bases,
		RepoRoots: in.RepoRoots, V1: v1, GeneratedAt: fmNow, Salt: fmSalt,
		ClaudeSettings: &scans.Settings, Home: fmHome,
		Hermes: scans.Hermes, Codex: scans.Codex, Grok: scans.Grok, Cline: scans.Cline,
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
	if len(svJSON) == 0 {
		t.Fatal("empty space view")
	}

	// BV-203 point 9: the space's today-action figures must equal the report's.
	// The report resolves today's agent from the finding (Evidence.Nodes[0]);
	// the space must now do the same, so the two before/after figures agree.
	if sv.TopAction != nil {
		from, to, ok := reportTodayFigures(v2)
		if !ok {
			t.Errorf("space shows a top action but the report resolves no agent for today's finding")
		} else if sv.TopAction.ScoreFrom != from || sv.TopAction.ScoreTo != to {
			t.Errorf("space top action %d->%d differs from the report's %d->%d (svTopAction resolved the wrong agent)",
				sv.TopAction.ScoreFrom, sv.TopAction.ScoreTo, from, to)
		}
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

	// 6. The HTML pages render in each language (EN and ES).
	for _, lang := range []string{"en", "es"} {
		htmlDir := filepath.Join(dir, lang)
		if err := os.MkdirAll(htmlDir, 0o755); err != nil {
			t.Fatal(err)
		}
		p, err := report.WriteHTMLPages(v2, lang, fmHome, nil, htmlDir)
		if err != nil {
			t.Fatalf("WriteHTMLPages(%s): %v", lang, err)
		}
		if st, err := os.Stat(p); err != nil || st.Size() == 0 {
			t.Errorf("WriteHTMLPages(%s) produced no page at %s", lang, p)
		}
	}
}

// reportTodayFigures resolves today's agent the way the report does — a
// machine finding shows the first scored agent's score, otherwise the finding's
// first evidence node is its agent — and returns its before/after projection.
func reportTodayFigures(v report.V2) (from, to int, ok bool) {
	if v.Today.FindingID == "" {
		return 0, 0, false
	}
	var today *rules.Finding
	for i := range v.Findings {
		if v.Findings[i].ID == v.Today.FindingID {
			today = &v.Findings[i]
			break
		}
	}
	if today == nil {
		return 0, 0, false
	}
	pick := func(a index.AgentIndex) (int, int, bool) {
		if a.Value == nil || a.Projection == nil {
			return 0, 0, false
		}
		return *a.Value, a.Projection.Value, true
	}
	if rules.IsMachineFinding(*today) {
		for _, a := range v.Index.Agents {
			if a.Value != nil {
				return pick(a)
			}
		}
		return 0, 0, false
	}
	if len(today.Evidence.Nodes) == 0 {
		return 0, 0, false
	}
	for _, a := range v.Index.Agents {
		if a.AgentID == today.Evidence.Nodes[0] {
			return pick(a)
		}
	}
	return 0, 0, false
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
