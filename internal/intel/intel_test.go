package intel_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/intel"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/pyenv"
)

// TestLoadEmbeddedSnapshot is the schema-validation guard (BV-100): the
// embedded data/intel.json must parse and validate against intel.v1, or the
// binary would fail-secure on every run.
func TestLoadEmbeddedSnapshot(t *testing.T) {
	snap, err := intel.Load()
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
			if p.Ecosystem != intel.EcosystemPyPI || p.Package != "litellm" {
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
	m := intel.NewMatcher(mustLoad(t))

	cases := []struct {
		name          string
		eco, pkg, ver string
		want          bool
	}{
		{"exact", intel.EcosystemPyPI, "litellm", "1.107.0", true},
		{"case-folded name", intel.EcosystemPyPI, "LiteLLM", "1.107.0", true}, // normalizes to litellm
		{"other version", intel.EcosystemPyPI, "litellm", "1.106.0", false},
		{"unpinned", intel.EcosystemPyPI, "litellm", "", false},
		{"wrong ecosystem", intel.EcosystemNPM, "litellm", "1.107.0", false},
		{"unknown package", intel.EcosystemPyPI, "requests", "2.0.0", false},
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
	var m *intel.Matcher
	if _, ok := m.Match(intel.EcosystemPyPI, "litellm", "1.107.0"); ok {
		t.Error("nil matcher matched")
	}
	if m.Artifacts(intel.EcosystemPyPI, "litellm") != nil {
		t.Error("nil matcher returned artifacts")
	}
}

func mustLoad(t *testing.T) *intel.Snapshot {
	t.Helper()
	snap, err := intel.Load()
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

// BV-106: the snapshot 2026.10.29.1 carries the four signed entries.
func TestSnapshotEntries(t *testing.T) {
	snap := mustLoad(t)
	if snap.Version != "2026.10.29.1" || snap.PublishedAt != "2026-10-29" {
		t.Errorf("snapshot %s of %s, want 2026.10.29.1 of 2026-10-29", snap.Version, snap.PublishedAt)
	}
	want := map[string]string{
		"rosmarops-2026-0001": "litellm 1.107.0",
		"rosmarops-2026-0002": "claudeopusfile 0.1.1 0.1.2",
		"rosmarops-2026-0003": "claudeopuseni 0.1.0 0.1.1",
		"rosmarops-2026-0004": "claudeopusenifile 0.1.3",
	}
	for _, p := range snap.Packages {
		got := p.Package + " " + strings.Join(p.Versions, " ")
		if w, ok := want[p.ID]; !ok || got != w || p.Ecosystem != intel.EcosystemPyPI {
			t.Errorf("%s: %s/%s, want pypi/%s", p.ID, p.Ecosystem, got, w)
		}
		delete(want, p.ID)
	}
	for id := range want {
		t.Errorf("%s is not in the snapshot", id)
	}
}

// BV-106, by the LiteLLM test's path (tests/trust, TestNoSecretFromIntelCross):
// claudeopuseni 0.1.1 installed in a project's .venv is read by the Python
// collector, crossed with the snapshot and reported as package_known_malicious;
// 0.1.2, not listed, is not.
func TestClaudeOpusEniInVenv(t *testing.T) {
	for _, c := range []struct {
		version string
		want    int
	}{{"0.1.1", 1}, {"0.1.2", 0}} {
		t.Run(c.version, func(t *testing.T) {
			root := t.TempDir()
			site := filepath.Join(root, "repo", ".venv", "Lib", "site-packages")
			dist := filepath.Join(site, "claudeopuseni-"+c.version+".dist-info")
			if err := os.MkdirAll(dist, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dist, "METADATA"), []byte("Name: claudeopuseni\nVersion: "+c.version+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			py := pyenv.Scan([]string{site})
			if len(py.Packages) != 1 {
				t.Fatalf("pyenv read %d packages, want 1: %+v", len(py.Packages), py.Packages)
			}
			var obs []graph.PackageObservation
			for _, p := range py.Packages {
				obs = append(obs, graph.PackageObservation{Ecosystem: intel.EcosystemPyPI, Name: p.Name, Version: p.Version, Path: p.Path})
			}

			salt := []byte("0123456789abcdef0123456789abcdef")
			now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
			base, err := knowledge.ClaudeCode()
			if err != nil {
				t.Fatal(err)
			}
			matcher := intel.NewMatcher(mustLoad(t))
			frag, err := graph.FromMCPScan(platform.MCPScan{}, salt, now)
			if err != nil {
				t.Fatal(err)
			}
			g := graph.Build(frag, graph.Provenance{
				EngineVersion: "t", RulesVersion: graph.RulesVersion,
				KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt),
			}, now)
			graph.CrossIntel(&g, nil, obs, matcher, now)
			v, _ := report.BuildV2(report.V2Input{
				Graph: &g, GraphSHA256: strings.Repeat("0", 64), Base: base,
				V1: report.Build("0.1.0-dev", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: now,
				Memory: memory.File{}, Salt: salt, Home: root,
				IntelVersion: matcher.Version(), IntelPublishedAt: matcher.PublishedAt(),
			})
			got := 0
			for _, f := range v.Findings {
				if f.Rule == rules.RulePackageKnownMalicious {
					got++
					if f.Severity != rules.SeverityRed {
						t.Errorf("%s is %s, want red", f.ID, f.Severity)
					}
				}
			}
			if got != c.want {
				t.Errorf("claudeopuseni %s: %d package_known_malicious findings, want %d", c.version, got, c.want)
			}
		})
	}
}
