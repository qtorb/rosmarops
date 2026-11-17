package trust_test

// BV-249 §6 (CTO-58 §5, signed): the release's JSON files for the web —
// factory-scores.json and profile-scores.json — each validated against its
// schema in schemas/release/ by the test that writes it.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/declared"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/profiles"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/schemaval"
)

// validateRelease validates data against schemas/release/<name>.
func validateRelease(t *testing.T, name string, data []byte) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "schemas", "release", name))
	if err != nil {
		t.Fatal(err)
	}
	s, err := schemaval.Compile(raw)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	res, err := s.Validate(data)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if !res.Valid() {
		t.Errorf("%s: invalid: %v", name, res)
	}
}

func TestFactoryScoresSchema(t *testing.T) {
	doc, _ := r2Collect(t)
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	validateRelease(t, "factory-scores.v1.schema.json", data)
}

type psTemplate struct {
	Product     string      `json:"product"`
	Profile     string      `json:"profile"`
	OS          string      `json:"os"`
	Score       int         `json:"score"`
	Band        string      `json:"band"`
	Class       string      `json:"class"`
	ClassStatus string      `json:"class_status"`
	R2All       int         `json:"r2_all"`
	Findings    []fsFinding `json:"findings"`
}

type profileScores struct {
	Format            string       `json:"format"`
	RosmaropsVersion  string       `json:"rosmarops_version"`
	Commit            string       `json:"commit"`
	GeneratedAt       string       `json:"generated_at"`
	ReferenceComputer string       `json:"reference_computer"`
	IndexVersion      int          `json:"index_version"`
	RulesVersion      string       `json:"rules_version"`
	KnowledgeVersion  string       `json:"knowledge_version"`
	Templates         []psTemplate `json:"templates"`
}

// profilePins are the signed figures of the templates (BV-236, BV-248,
// BV-249): product/profile/os -> score and class. A template without a pin
// is written as measured.
var profilePins = map[string]struct {
	score int
	class string
}{
	"claude-code/P1/linux": {99, "B"}, "claude-code/P1/macos": {99, "B"},
	"claude-code/P2/linux": {99, "B"}, "claude-code/P2/macos": {99, "B"},
	"grok-build/P2/linux": {75, "B"}, "grok-build/P2/macos": {75, "B"},
}

// profileCollect measures every template on R2 per system, as the report
// does (the agent alone; its class with R2-todos changed the same way).
func profileCollect(t *testing.T) (doc profileScores, allMatch bool) {
	t.Helper()
	prev := graph.HostOS
	defer func() { graph.HostOS = prev }()
	allMatch = true
	idxVer := 0
	add := func(product, id, osName string, v report.V2, mod func(*pipeline.Input)) {
		got, band, aid := r2AgentIndexID(t, v, product)
		idxVer = v.Index.IndexVersion
		a := classOfSingle(t, v, product, mod)
		all, _ := r2AllReportWith(t, mod)
		findings, _ := r2Detail(v, aid)
		r2all := 0
		if all.Index.Machine.Value != nil {
			r2all = *all.Index.Machine.Value
		}
		if pin, ok := profilePins[product+"/"+id+"/"+osName]; ok && (pin.score != got || pin.class != a.Class) {
			allMatch = false
			t.Errorf("%s %s on %s: %d %s, want %d %s", product, id, osName, got, a.Class, pin.score, pin.class)
		}
		if findings == nil {
			findings = []fsFinding{}
		}
		doc.Templates = append(doc.Templates, psTemplate{
			Product: product, Profile: id, OS: osName, Score: got, Band: band, Class: a.Class,
			ClassStatus: a.ClassStatus, R2All: r2all, Findings: findings,
		})
	}
	for _, sys := range []struct{ goos, name string }{{"windows", "windows"}, {"darwin", "macos"}, {"linux", "linux"}} {
		graph.HostOS = sys.goos
		for _, id := range profiles.MeasuredOn(sys.goos) {
			v, mod := r2ClaudeCodeProfile(t, id)
			add(knowledge.ClaudeCodeProduct, id, sys.name, v, mod)
		}
		for _, dp := range pipeline.DeclaredProfiles {
			if dp.Product != declared.Cursor {
				continue
			}
			answers := dp.Answers
			add(knowledge.CursorProduct, dp.ID, sys.name, r2CursorWith(t, answers, false), func(in *pipeline.Input) { in.Cursor.Declared = answers })
		}
		if sys.goos != "windows" {
			v, mod := r2GrokP2(t)
			add(knowledge.GrokBuildProduct, profiles.P2, sys.name, v, mod)
		}
	}
	doc.Format = "rosmarops.profile-scores/1"
	doc.RosmaropsVersion = envOr("ROSMAROPS_VERSION", "dev")
	doc.Commit = os.Getenv("GITHUB_SHA")
	doc.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	doc.ReferenceComputer = "R2"
	doc.IndexVersion = idxVer
	doc.RulesVersion = graph.RulesVersion
	doc.KnowledgeVersion = knowledge.Composite
	return doc, allMatch
}

// TestProfileScores pins the signed template figures and validates
// profile-scores.json. With ROSMAROPS_PROFILE_SCORES_OUT (an absolute path),
// and only when every pin matches, it writes the file there for the release.
func TestProfileScores(t *testing.T) {
	doc, allMatch := profileCollect(t)
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	validateRelease(t, "profile-scores.v1.schema.json", data)
	for _, tp := range doc.Templates {
		t.Logf("%s %s %s: %d %s %s, R2-todos %d", tp.Product, tp.Profile, tp.OS, tp.Score, tp.Band, tp.Class, tp.R2All)
	}
	out := os.Getenv("ROSMAROPS_PROFILE_SCORES_OUT")
	if out == "" {
		return
	}
	if !filepath.IsAbs(out) {
		t.Fatalf("ROSMAROPS_PROFILE_SCORES_OUT must be an absolute path, got %q", out)
	}
	if !allMatch {
		return
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", out, err)
	}
}
