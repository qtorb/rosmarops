package intel

import "testing"

// TestLoadEmbeddedSnapshot is the schema-validation guard (BV-100): the
// embedded data/intel.json must parse and validate against intel.v1, or the
// binary would fail-secure on every run.
func TestLoadEmbeddedSnapshot(t *testing.T) {
	snap, err := Load()
	if err != nil {
		t.Fatalf("embedded snapshot does not load/validate: %v", err)
	}
	if snap.Version == "" || snap.PublishedAt == "" {
		t.Fatalf("snapshot header missing: %+v", snap)
	}
	if len(snap.Packages) == 0 {
		t.Fatal("snapshot has no packages")
	}
	// The first signed entry (rosmarops-2026-0001): LiteLLM 1.107.0 (PyPI).
	var found bool
	for _, p := range snap.Packages {
		if p.ID == "rosmarops-2026-0001" {
			found = true
			if p.Ecosystem != EcosystemPyPI || p.Package != "litellm" {
				t.Errorf("entry 0001 is %s/%s, want pypi/litellm", p.Ecosystem, p.Package)
			}
			if len(p.Versions) != 1 || p.Versions[0] != "1.107.0" {
				t.Errorf("entry 0001 versions %v, want [1.107.0]", p.Versions)
			}
		}
	}
	if !found {
		t.Error("the LiteLLM entry rosmarops-2026-0001 is not in the snapshot")
	}
}

// TestMatcher checks exact, pinned matching and the ecosystem name normalization.
func TestMatcher(t *testing.T) {
	m := NewMatcher(mustLoad(t))

	cases := []struct {
		name          string
		eco, pkg, ver string
		want          bool
	}{
		{"exact", EcosystemPyPI, "litellm", "1.107.0", true},
		{"case-folded name", EcosystemPyPI, "LiteLLM", "1.107.0", true}, // normalizes to litellm
		{"other version", EcosystemPyPI, "litellm", "1.106.0", false},
		{"unpinned", EcosystemPyPI, "litellm", "", false},
		{"wrong ecosystem", EcosystemNPM, "litellm", "1.107.0", false},
		{"unknown package", EcosystemPyPI, "requests", "2.0.0", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			match, ok := m.Match(c.eco, c.pkg, c.ver)
			if ok != c.want {
				t.Fatalf("Match(%q,%q,%q) = %v, want %v", c.eco, c.pkg, c.ver, ok, c.want)
			}
			if ok {
				if match.IntelID == "" || match.Package != "litellm" || match.Version != c.ver {
					t.Errorf("match carries %+v", match)
				}
			}
		})
	}
}

// TestNilMatcher is defensive: a nil Matcher (invalid snapshot, fail-secure)
// never panics and never matches.
func TestNilMatcher(t *testing.T) {
	var m *Matcher
	if _, ok := m.Match(EcosystemPyPI, "litellm", "1.107.0"); ok {
		t.Error("nil matcher matched")
	}
	if m.Artifacts(EcosystemPyPI, "litellm") != nil {
		t.Error("nil matcher returned artifacts")
	}
}

func mustLoad(t *testing.T) *Snapshot {
	t.Helper()
	snap, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	return snap
}
