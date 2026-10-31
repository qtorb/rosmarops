package report_test

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/results"
)

// buildSpaceViewScenario builds a v2 report from the shared graph fixture
// (one service_role key read by Claude Code, one DATABASE_URL in a template)
// with its OS set, so a space view validates against the schema.
func buildSpaceViewScenario(t *testing.T) (report.V2, report.SpaceView) {
	t.Helper()
	g := v2Graph(t)
	v1 := report.Build("t", nil, nil, 0, 0, 0, 0, nil)
	v := buildV2(t, &g, v1)
	v.OS = "windows"
	sv, err := report.BuildSpaceView(v, &g, "/h", "0.6.0-dev")
	if err != nil {
		t.Fatalf("BuildSpaceView: %v", err)
	}
	return v, sv
}

// TestBuildSpaceView_ValidatesAndTexts: the space view validates against
// space-view.v1, carries the run's identity, and every finding has its texts
// in en and es, taken from the catalog (BV-119, AP-17 §2).
func TestBuildSpaceView_ValidatesAndTexts(t *testing.T) {
	v, sv := buildSpaceViewScenario(t)

	if _, err := report.RenderSpaceViewJSON(sv); err != nil {
		t.Fatalf("space view does not validate: %v", err)
	}
	if sv.SchemaVersion != "space-view.v1" {
		t.Errorf("schema_version = %q", sv.SchemaVersion)
	}
	if sv.RosmaropsVersion != "0.6.0" { // the "-dev" build suffix is dropped
		t.Errorf("rosmarops_version = %q, want 0.6.0", sv.RosmaropsVersion)
	}
	if sv.OS != "windows" || sv.HostID == "" || sv.GeneratedAt != v.GeneratedAt {
		t.Errorf("identity: os=%q host=%q generated_at=%q", sv.OS, sv.HostID, sv.GeneratedAt)
	}
	if len(sv.Findings) == 0 {
		t.Fatal("no findings — test would be vacuous")
	}
	for _, f := range sv.Findings {
		for _, lang := range []string{"en", "es"} {
			tx, ok := f.Text[lang]
			if !ok {
				t.Errorf("finding %s has no %s text", f.ID, lang)
				continue
			}
			if tx.Title == "" || tx.WhatItMeans == "" {
				t.Errorf("finding %s (%s): empty title or what_it_means", f.ID, lang)
			}
			if strings.Contains(tx.Title, "{") || strings.Contains(tx.WhatItMeans, "{") {
				t.Errorf("finding %s (%s): a placeholder was left in the text", f.ID, lang)
			}
		}
	}
	// A credential of the graph becomes a key with a 64-hex fingerprint and a
	// label taken from the catalog, with no secret value anywhere.
	if len(sv.Keys) == 0 {
		t.Fatal("no keys — the graph has a credential, so this is a bug")
	}
	hex64 := regexp.MustCompile(`^[0-9a-f]{64}$`)
	for _, k := range sv.Keys {
		if !hex64.MatchString(k.Fingerprint) {
			t.Errorf("key fingerprint %q is not 64 hex (and carries no hmac-sha256: prefix)", k.Fingerprint)
		}
		if k.Label["en"] == "" {
			t.Errorf("key %s has no English label", k.Fingerprint)
		}
	}
}

// TestBuildSpaceView_NoFindingText is the stopper check (BV-119 point 1): if a
// finding's title or what_it_means came back empty for any rule, the builder
// would be relying on a catalog text that does not exist. The scenario covers
// the credential rules; this guards that every emitted text is non-empty, so a
// missing key would fail here rather than silently ship blank text.
func TestBuildSpaceView_EveryTextPresent(t *testing.T) {
	_, sv := buildSpaceViewScenario(t)
	for _, f := range sv.Findings {
		en := f.Text["en"]
		if en.Title == "" || en.WhatItMeans == "" {
			t.Errorf("finding %s (%s) has no catalog text — stopper", f.ID, f.Rule)
		}
	}
}

// TestResultsPackage_RoundTrip seals the space view with a kit key, writes it
// to the reports directory, reads it back and opens it: the content is the
// same space view and validates against the schema. A wrong key fails.
func TestResultsPackage_RoundTrip(t *testing.T) {
	v, sv := buildSpaceViewScenario(t)
	g := v2Graph(t)
	want, err := report.RenderSpaceViewJSON(sv)
	if err != nil {
		t.Fatal(err)
	}
	kitKey := []byte("kitkitkitkitkitkitkitkitkitkit12") // 32 bytes
	dir := t.TempDir()
	path, err := report.WriteResultsPackage(v, &g, "/h", "0.6.0", kitKey, strings.Repeat("a", 32), strings.Repeat("b", 32), v2Now, dir)
	if err != nil {
		t.Fatalf("WriteResultsPackage: %v", err)
	}
	if filepath.Dir(path) != dir || !strings.HasSuffix(path, ".locked.json") {
		t.Errorf("package written to %s", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("package mode is %o, want 0600", info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := results.Parse(raw)
	if err != nil {
		t.Fatalf("parse package: %v", err)
	}
	got, err := results.Open(kitKey, pkg)
	if err != nil {
		t.Fatalf("open package: %v", err)
	}
	if string(got) != string(want) {
		t.Error("the decrypted content is not the space view that was sealed")
	}
	wrong := []byte("WRONGWRONGWRONGWRONGWRONGWRONG12")
	if _, err := results.Open(wrong, pkg); err == nil {
		t.Error("a wrong kit key opened the package")
	}
}

// TestResultsPackage_NoGraph: with no graph there is no content to seal, so
// nothing is written (mirrors a run with a kit but no graph).
func TestResultsPackage_NoGraph(t *testing.T) {
	v, _ := buildSpaceViewScenario(t)
	path, err := report.WriteResultsPackage(v, nil, "/h", "0.6.0", []byte("kitkitkitkitkitkitkitkitkitkit12"), strings.Repeat("a", 32), strings.Repeat("b", 32), v2Now, t.TempDir())
	if err != nil {
		t.Fatalf("WriteResultsPackage(nil graph): %v", err)
	}
	if path != "" {
		t.Errorf("a package was written without a graph: %s", path)
	}
}
