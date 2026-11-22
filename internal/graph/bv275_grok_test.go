package graph_test

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-275: Grok Build's layers in the documented precedence, custom sandbox
// profiles and its own hooks.

const bv275Grok = "/home/ana/.grok"

func bv275Layer(kind, path, profile string, system bool) platform.GrokLayer {
	return platform.GrokLayer{Kind: kind, Path: path, Status: platform.MCPSourceRead, SandboxProfile: profile, System: system}
}

func grokProfileCap(t *testing.T, scan platform.GrokScan) graph.Node {
	t.Helper()
	old := graph.HostOS
	graph.HostOS = "linux"
	t.Cleanup(func() { graph.HostOS = old })
	f := graph.FromGrok(scan, platform.ClaudeSettingsScan{}, platform.CursorScan{}, time.Unix(0, 0))
	for _, n := range f.Nodes {
		if n.Kind == graph.KindCapability && strings.HasPrefix(n.Rule, "sandbox.profile = ") {
			return n
		}
	}
	t.Fatalf("no sandbox profile capability: %+v", f.Nodes)
	return graph.Node{}
}

// The last layer that sets [sandbox] profile wins: the user's config over the
// managed config, the requirements over both.
func TestBV275_GrokLayerPrecedence(t *testing.T) {
	scan := platform.GrokScan{ConfigDir: bv275Grok, Exists: true, Layers: []platform.GrokLayer{
		bv275Layer(platform.GrokLayerManaged, "/etc/grok/managed_config.toml", "read-only", true),
		bv275Layer(platform.GrokLayerManaged, bv275Grok+"/managed_config.toml", "strict", false),
		bv275Layer(platform.GrokLayerUser, bv275Grok+"/config.toml", "workspace", false),
	}}
	if c := grokProfileCap(t, scan); c.TargetScope != "workspace" {
		t.Errorf("user over managed: %q", c.TargetScope)
	}
	scan.Layers = append(scan.Layers, bv275Layer(platform.GrokLayerRequirements, "/etc/grok/requirements.toml", "strict", true))
	if c := grokProfileCap(t, scan); c.TargetScope != "strict" || !strings.HasSuffix(c.Source, "/etc/grok/requirements.toml#/sandbox/profile") {
		t.Errorf("system requirements pin: %q from %s", c.TargetScope, c.Source)
	}
}

// A custom profile counts as the built-in profile it extends; an unknown
// name stays as it is.
func TestBV275_GrokCustomProfile(t *testing.T) {
	scan := platform.GrokScan{
		ConfigDir: bv275Grok, Exists: true,
		Layers: []platform.GrokLayer{bv275Layer(platform.GrokLayerUser, bv275Grok+"/config.toml", "project", false)},
		Sandboxes: []platform.GrokSandboxFile{{
			Path: bv275Grok + "/sandbox.toml", Status: platform.MCPSourceRead,
			Profiles: map[string]platform.GrokSandboxProfile{"project": {Extends: "strict"}},
		}},
	}
	c := grokProfileCap(t, scan)
	if c.TargetScope != "strict" || c.Rule != "sandbox.profile = project" {
		t.Errorf("custom profile: scope %q rule %q", c.TargetScope, c.Rule)
	}
	scan.Layers[0].SandboxProfile = "unknown"
	if c := grokProfileCap(t, scan); c.TargetScope != "unknown" {
		t.Errorf("unknown profile: %q", c.TargetScope)
	}
}

// Grok's own hooks are its executables: the user's, and a project's only in
// a trusted folder.
func TestBV275_GrokOwnHooks(t *testing.T) {
	scan := platform.GrokScan{ConfigDir: bv275Grok, Exists: true, Hooks: []platform.GrokHookFile{
		{Path: bv275Grok + "/hooks/safety.json", Status: platform.MCPSourceRead, Trusted: true},
		{Path: "/home/ana/ok/.grok/hooks/p.json", Root: "/home/ana/ok", Status: platform.MCPSourceRead, Trusted: true},
		{Path: "/home/ana/no/.grok/hooks/p.json", Root: "/home/ana/no", Status: platform.MCPSourceRead},
		{Path: bv275Grok + "/hooks/broken.json", Status: platform.MCPSourceFailed, Reason: "parse_error"},
	}}
	f := graph.FromGrok(scan, platform.ClaudeSettingsScan{}, platform.CursorScan{}, time.Unix(0, 0))
	got := map[string]bool{}
	for _, n := range f.Nodes {
		if n.Type == graph.ResourceAgentExecutable {
			got[n.Path] = true
		}
	}
	if len(got) != 2 || !got[bv275Grok+"/hooks/safety.json"] || !got["/home/ana/ok/.grok/hooks/p.json"] {
		t.Errorf("executables: %v", got)
	}
	failed := false
	for _, s := range f.Coverage.SourcesFailed {
		failed = failed || s.Path == bv275Grok+"/hooks/broken.json"
	}
	if !failed {
		t.Errorf("broken hooks file not in coverage: %+v", f.Coverage)
	}
}
