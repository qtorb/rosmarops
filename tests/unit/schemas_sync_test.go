// Package unit contains project-level unit tests that do not belong to a
// specific internal package.
package unit_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSchemasInSync verifies that each JSON schema file under schemas/ (the
// human-readable reference) is byte-identical to its counterpart under
// internal/schemas/ (the embedded copy used for runtime validation).
//
// If this test fails it means the two copies have diverged — update
// internal/schemas/ to match schemas/ (or vice-versa) before committing.
//
// This test is also the backing implementation of `task check-schemas`.
func TestSchemasInSync(t *testing.T) {
	// Locate repo root relative to this test file.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile is <repo>/tests/unit/schemas_sync_test.go
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")

	schemas := []string{
		"config.v1.json",
		"state.v1.json",
		"state.v2.json",
		"report.v1.json",
		"v2/graph.v2.schema.json",
		"v2/report.v2.schema.json",
		"v2/index-history.v1.schema.json",
		"v2/findings-memory.v1.schema.json",
		"v2/findings-memory.v2.schema.json",
		"intel/intel.v1.schema.json",
		"kit.v1.schema.json",
	}

	for _, name := range schemas {
		t.Run(name, func(t *testing.T) {
			canonical := filepath.Join(repoRoot, "schemas", filepath.FromSlash(name))
			embedded := filepath.Join(repoRoot, "internal", "schemas", filepath.FromSlash(name))

			canonicalBytes, err := os.ReadFile(canonical)
			if err != nil {
				t.Fatalf("read canonical %s: %v", canonical, err)
			}
			embeddedBytes, err := os.ReadFile(embedded)
			if err != nil {
				t.Fatalf("read embedded %s: %v", embedded, err)
			}

			if !bytes.Equal(canonicalBytes, embeddedBytes) {
				t.Errorf(
					"schema copies have diverged:\n  canonical: %s\n  embedded:  %s\n"+
						"Update internal/schemas/%s to match schemas/%s before committing.",
					canonical, embedded, name, name,
				)
			}
		})
	}
}
