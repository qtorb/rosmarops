package report_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
	"github.com/qtorb/rosmarops/internal/schemas"
	"github.com/qtorb/rosmarops/internal/schemaval"
)

// distinctJWT is a distinct, valid Supabase service_role JWT per i (its ref is
// 20 lowercase letters encoding i), so each test credential is its own key and
// its own finding — an Albert-like run with many projects.
func distinctJWT(i int) string {
	b := []byte("aaaaaaaaaaaaaaaaaaaa") // 20 lowercase letters (the ref pattern)
	for k, n := 19, i; k >= 0 && n > 0; k-- {
		b[k] = byte('a' + n%26)
		n /= 26
	}
	return seg(`{"alg":"HS256","typ":"JWT"}`) + "." +
		seg(fmt.Sprintf(`{"iss":"supabase","ref":%q,"role":"service_role"}`, string(b))) +
		".ZmFrZS1zaWduYXR1cmUtZmlsbGVy"
}

// BV-177: the report field of a real test report validates against the BA-54
// schema and carries report in EN and ES.
func TestReportFieldValidatesAndBothLangs(t *testing.T) {
	_, sv := buildSpaceViewScenario(t)

	// It validates (RenderSpaceViewJSON compiles and validates against
	// space-view.v1, the BA-54 schema).
	js, err := report.RenderSpaceViewJSON(sv)
	if err != nil {
		t.Fatalf("space view with report does not validate: %v", err)
	}
	r := sv.Report
	if r == nil {
		t.Fatal("no report field")
	}
	// step1 in both languages, no placeholder left.
	for _, pair := range []struct {
		name          string
		t             report.SVT
		noPlaceholder bool
	}{
		{"headline", r.Step1.Headline, true},
		{"counts", r.Step1.Counts, true},
		{"meta", r.Step1.Meta, true},
		{"how_fix_body", r.HowFixBody, true},
		{"ai_export", r.AIExport, false}, // the AI export is Markdown that may contain {
	} {
		if pair.t.EN == "" || pair.t.ES == "" {
			t.Errorf("%s: empty EN or ES (%q / %q)", pair.name, pair.t.EN, pair.t.ES)
		}
		if pair.noPlaceholder && (strings.Contains(pair.t.EN, "{") || strings.Contains(pair.t.ES, "{")) {
			t.Errorf("%s: a placeholder was left in the text", pair.name)
		}
	}
	// A row per finding, in findings order, each with both languages where set.
	if len(r.Rows) != len(sv.Findings) {
		t.Fatalf("rows=%d findings=%d: want one row per finding", len(r.Rows), len(sv.Findings))
	}
	for i, row := range r.Rows {
		if row.FindingID != sv.Findings[i].ID {
			t.Errorf("row %d finding_id=%q, want %q (findings order)", i, row.FindingID, sv.Findings[i].ID)
		}
		if row.Confidence == nil || row.Confidence.EN == "" || row.Confidence.ES == "" {
			t.Errorf("row %s: confidence missing a language", row.FindingID)
		}
		for _, s := range row.FixSteps {
			if s.EN == "" || s.ES == "" {
				t.Errorf("row %s: a fix step is missing a language", row.FindingID)
			}
		}
	}
	// The whole thing is really bilingual: the JSON holds Spanish too.
	if !strings.Contains(string(js), `"es"`) {
		t.Error("the rendered package carries no Spanish")
	}
}

// TestReportClampsOversize (BV-177 point 3): texts over their limit are cut at
// the end with «…» and lists over their limit keep the first items; the
// package still validates, so an oversize report is never dropped.
func TestReportClampsOversize(t *testing.T) {
	// Many credentials in many files -> many findings, each with content; plus a
	// very long location to force text truncation.
	var entries []platform.CredentialEntry
	for i := 0; i < 20; i++ {
		entries = append(entries, platform.CredentialEntry{
			Path:  fmt.Sprintf("/src/a/p%02d/%s/.env.local", i, strings.Repeat("deeplongsegment", 60)),
			Line:  i + 1,
			Key:   "SUPABASE_SERVICE_ROLE_KEY",
			Value: secrets.NewSecretValue(distinctJWT(i)),
		})
	}
	g := v2GraphWith(t, entries, nil)
	v1 := report.Build("t", nil, nil, 0, 0, 0, 0, nil)
	v := buildV2(t, &g, v1)
	v.OS = "windows"
	sv, err := report.BuildSpaceView(v, &g, "/h", "0.6.0")
	if err != nil {
		t.Fatal(err)
	}
	// Validates despite the oversize input: every text and list is within its
	// schema limit, so the package is never dropped for size (BV-177 point 3).
	// The «…» truncation of an over-limit text itself is proven by TestClipRunes.
	if _, err := report.RenderSpaceViewJSON(sv); err != nil {
		t.Fatalf("oversize report did not clamp to a valid package: %v", err)
	}
	r := sv.Report
	if r == nil || len(r.Rows) == 0 {
		t.Fatal("no report rows")
	}
	checkT := func(name string, tt report.SVT, max int) {
		if len([]rune(tt.EN)) > max || len([]rune(tt.ES)) > max {
			t.Errorf("%s over %d runes (en=%d es=%d)", name, max, len([]rune(tt.EN)), len([]rune(tt.ES)))
		}
	}
	checkT("headline", r.Step1.Headline, 300)
	if len(r.Rows) > 500 {
		t.Errorf("rows over 500: %d", len(r.Rows))
	}
	for _, row := range r.Rows {
		if len(row.Locations) > 5 {
			t.Errorf("row %s has %d locations (max 5)", row.FindingID, len(row.Locations))
		}
		for _, loc := range row.Locations {
			if len([]rune(loc)) > 400 {
				t.Errorf("a location was not clamped to 400 runes: %d", len([]rune(loc)))
			}
		}
		if len(row.FixSteps) > 6 || len(row.Assumptions) > 5 {
			t.Errorf("row %s lists over limit: fix=%d assum=%d", row.FindingID, len(row.FixSteps), len(row.Assumptions))
		}
		for _, s := range row.FixSteps {
			checkT("fix_step", s, 1000)
		}
	}
}

// TestReportPackageWellUnder5MB (BV-177 point 5): an Albert-like run with many
// projects and findings seals to a package far under the 5 MB limit. The real
// 48-project figure is measured by the e2e-realistic CI job; this bounds it
// with a synthetic run of comparable finding volume.
func TestReportPackageWellUnder5MB(t *testing.T) {
	var entries []platform.CredentialEntry
	for i := 0; i < 250; i++ {
		entries = append(entries, platform.CredentialEntry{
			Path:  fmt.Sprintf("/src/a/project%03d/service/.env.local", i),
			Line:  i%40 + 1,
			Key:   "SUPABASE_SERVICE_ROLE_KEY",
			Value: secrets.NewSecretValue(distinctJWT(i)),
		})
	}
	g := v2GraphWith(t, entries, nil)
	v1 := report.Build("t", nil, nil, 0, 0, 0, 0, nil)
	v := buildV2(t, &g, v1)
	v.OS = "windows"
	sv, err := report.BuildSpaceView(v, &g, "/h", "0.7.6")
	if err != nil {
		t.Fatal(err)
	}
	js, err := report.RenderSpaceViewJSON(sv)
	if err != nil {
		t.Fatalf("package does not validate: %v", err)
	}
	const limit = 5 * 1024 * 1024
	t.Logf("BV-177: space-view package for %d findings is %d bytes (%.1f KB), limit %d (5 MB)", len(sv.Findings), len(js), float64(len(js))/1024, limit)
	if len(js) >= limit {
		t.Fatalf("package is %d bytes, not under 5 MB", len(js))
	}
	// «Well under»: comfortably below half the limit even at this finding volume
	// (the real 48-project figure is smaller; the e2e-realistic CI job measures
	// it).
	if len(js) > limit/2 {
		t.Errorf("package is %d bytes — not well under 5 MB", len(js))
	}
}

// TestBV177TestdataValidates checks the committed testdata package (generated by
// this branch, full report in EN and ES). Run with WRITE_BV177=1 to regenerate
// it. The advisor passes this file through the production validator.
func TestBV177TestdataValidates(t *testing.T) {
	path := filepath.Join("..", "..", "cmd", "rosmarops", "testdata", "bv177", "space-view.v1.json")
	if os.Getenv("WRITE_BV177") == "1" {
		_, sv := buildSpaceViewScenario(t)
		js, err := report.RenderSpaceViewJSON(sv)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, js, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s (%d bytes)", path, len(js))
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("testdata package missing (run WRITE_BV177=1 to generate): %v", err)
	}
	schema, err := schemaval.Compile(schemas.SpaceViewSchema)
	if err != nil {
		t.Fatal(err)
	}
	res, err := schema.Validate(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid() {
		t.Fatalf("testdata package does not validate: %v", res.Errors())
	}
	s := string(raw)
	if !strings.Contains(s, `"report"`) || !strings.Contains(s, `"ai_export"`) {
		t.Error("testdata package has no report/ai_export")
	}
	if !strings.Contains(s, `"en"`) || !strings.Contains(s, `"es"`) {
		t.Error("testdata package is not bilingual")
	}
}
