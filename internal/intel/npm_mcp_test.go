package intel_test

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/intel"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-154: the four @ifasn/*-doc-mcp npm releases are malicious packages that
// impersonate MCP servers. The npm coordinate RosmarOps crosses against intel
// comes from the MCP server invocation (npx/bunx/pnpm dlx) parsed in memory and
// from global node_modules read at rest — NOT from a per-repo package-lock.json,
// which is not an intel source today (see the BV-154 report, point 2). This test
// drives the realistic path: a Claude Code MCP server launched with
// `npx -y @ifasn/md-doc-mcp@<ver>`.
//
// A pinned malicious version yields package_known_malicious carrying its intel
// entry id (rosmarops-2026-0005) and the report shows the new threat-data stamp
// «... 2026.11.03.1 ...»; an unpinned command or an unlisted version yields
// nothing (only pinned coordinates are crossed, BV-100).
func TestMaliciousMCPServerNpm(t *testing.T) {
	const (
		home   = "/home/alice"
		cfgDir = home + "/.claude"
		cfg    = home + "/.claude.json"
	)
	salt := []byte("0123456789abcdef0123456789abcdef")
	now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)

	// mcpScan builds a one-server Claude Code scan whose server runs the given
	// npx arguments.
	mcpScan := func(args []string) platform.MCPScan {
		return platform.MCPScan{
			Sources: []platform.MCPSource{{
				Platform: "claude-code", ConfigPath: cfg, ConfigDir: cfgDir,
				Status: platform.MCPSourceRead, LoadsMCP: true,
			}},
			Servers: []platform.MCPServer{{
				Client: "claude-code", Name: "md-doc", Command: "npx", Args: args,
				ConfigPath: cfg, ConfigDir: cfgDir, LoadsMCP: true,
				Scope: "user", Transport: "stdio",
			}},
		}
	}

	cases := []struct {
		name       string
		args       []string
		wantIntel  string // "" means no package_known_malicious finding expected
		wantMarker bool
	}{
		{"pinned malicious", []string{"-y", "@ifasn/md-doc-mcp@1.0.4"}, "rosmarops-2026-0005", true},
		{"pinned malicious 0008 odd version", []string{"-y", "@ifasn/mmdd-doc-mcp@1.1.9-2"}, "rosmarops-2026-0008", true},
		{"unpinned (no version)", []string{"-y", "@ifasn/md-doc-mcp"}, "", false},
		{"pinned but unlisted version", []string{"-y", "@ifasn/md-doc-mcp@1.0.9"}, "", false},
	}

	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	matcher := intel.NewMatcher(mustLoad(t))

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			scan := mcpScan(c.args)
			frag, err := graph.FromMCPScan(scan, salt, now)
			if err != nil {
				t.Fatalf("FromMCPScan: %v", err)
			}
			g := graph.Build(frag, graph.Provenance{
				EngineVersion: "t", RulesVersion: graph.RulesVersion,
				KnowledgeVersion: graph.KnowledgeVersion, HostID: graph.HostID(salt),
			}, now)
			graph.CrossIntel(&g, scan.Servers, nil, matcher, now)

			v, _ := report.BuildV2(report.V2Input{
				Graph: &g, GraphSHA256: strings.Repeat("0", 64), Base: base,
				V1: report.Build("0.1.0-dev", nil, nil, 0, 0, 0, 0, nil), GeneratedAt: now,
				Memory: memory.File{}, Salt: salt, Home: home,
				IntelVersion: matcher.Version(), IntelPublishedAt: matcher.PublishedAt(),
			})

			var got []rules.Finding
			for _, f := range v.Findings {
				if f.Rule == rules.RulePackageKnownMalicious {
					got = append(got, f)
				}
			}
			if c.wantIntel == "" {
				if len(got) != 0 {
					t.Fatalf("%s: got %d package_known_malicious findings, want 0", c.name, len(got))
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("%s: got %d package_known_malicious findings, want 1", c.name, len(got))
			}
			f := got[0]
			if f.Severity != rules.SeverityRed {
				t.Errorf("%s: severity %s, want red", c.name, f.Severity)
			}
			if f.Params.IntelID != c.wantIntel {
				t.Errorf("%s: intel id %q, want %q", c.name, f.Params.IntelID, c.wantIntel)
			}
			if f.Params.Ecosystem != intel.EcosystemNPM {
				t.Errorf("%s: ecosystem %q, want npm", c.name, f.Params.Ecosystem)
			}

			// The report shows the new threat-data stamp, with the version
			// (BV-154 point 3). The number is language-independent; the label is
			// English until an es translation of report.intel_stamp exists.
			if v.Provenance.IntelVersion != "2026.11.03.1" {
				t.Errorf("provenance intel version %q, want 2026.11.03.1", v.Provenance.IntelVersion)
			}
			if c.wantMarker {
				cat, err := catalog.Load("en")
				if err != nil {
					t.Fatal(err)
				}
				page := report.RenderHTML(v, cat, home, time.UTC)
				if !strings.Contains(page, "2026.11.03.1") {
					t.Errorf("%s: rendered report does not show the threat-data version 2026.11.03.1", c.name)
				}
			}
		})
	}
}
