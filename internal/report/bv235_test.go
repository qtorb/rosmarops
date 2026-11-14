package report_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/qtorb/rosmarops/internal/schemaval"
)

// BV-235 (CTO-48 §2.3, BA-90): the space package of this version validates
// against the extended space-view.v1, and, without its new fields, against
// the space-view.v1 of v0.11.7: packages keep the same shape otherwise.
func TestBV235SpaceViewOldSchemaCompatible(t *testing.T) {
	g, v2 := maximalReport(t)
	sv := mustValidSpaceView(t, v2, g) // the extended schema
	var doc map[string]any
	if err := json.Unmarshal(sv, &doc); err != nil {
		t.Fatal(err)
	}
	newAgent := []string{"letter", "factory_score", "factory_letter", "groups", "kinds"}
	seen := map[string]bool{}
	for _, a := range doc["agents"].([]any) {
		m := a.(map[string]any)
		for _, k := range newAgent {
			if _, ok := m[k]; ok {
				seen[k] = true
			}
			delete(m, k)
		}
	}
	for _, k := range []string{"letter", "factory_letter", "kinds"} {
		if !seen[k] {
			t.Errorf("no agent carries %s: the package does not exercise the new fields", k)
		}
	}
	for _, f := range doc["findings"].([]any) {
		delete(f.(map[string]any), "accepted_date")
	}
	if r, ok := doc["report"].(map[string]any); ok {
		for _, row := range r["rows"].([]any) {
			delete(row.(map[string]any), "accepted_date")
		}
	}
	raw, err := os.ReadFile(filepath.Join("testdata", "space-view.v1.0117.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	old, err := schemaval.Compile(raw)
	if err != nil {
		t.Fatal(err)
	}
	stripped, _ := json.Marshal(doc)
	res, err := old.Validate(stripped)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid() {
		t.Errorf("without the new fields, the package fails the v0.11.7 space-view.v1: %v", res.Errors())
	}
}
