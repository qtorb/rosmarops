package graph

import (
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

func capsOf(f Fragment) map[string]Node {
	out := map[string]Node{}
	for _, n := range f.Nodes {
		if n.Kind == KindCapability {
			out[n.Type+"|"+n.TargetScope+"|"+n.AppliesIn] = n
		}
	}
	return out
}

// BV-276: with no settings, the factory permissions; a user's file and a
// project's file change them, each in its scope; the managed file wins.
func TestFromOpenCode(t *testing.T) {
	now := time.Date(2026, 11, 22, 12, 0, 0, 0, time.UTC)
	oc, err := knowledge.OpenCode()
	if err != nil {
		t.Fatal(err)
	}
	kilo, err := knowledge.KiloCode()
	if err != nil {
		t.Fatal(err)
	}
	encode := func(f Fragment) {
		t.Helper()
		if _, err := Encode(Build(f, Provenance{EngineVersion: "t", RulesVersion: RulesVersion, KnowledgeVersion: "k", HostID: "host_0123456789abcdef"}, now)); err != nil {
			t.Fatalf("not a valid graph.v2: %v", err)
		}
	}

	// Factory: OpenCode allows everything but reading outside the project;
	// Kilo Code asks before commands. Commands that run without asking give
	// the terminal read, from the factory default too (BV-276).
	fresh := platform.OpenCodeScan{Product: "opencode", Present: true, ConfigDir: "/h/.config/opencode"}
	f := FromOpenCode(fresh, oc, now)
	encode(f)
	c := capsOf(f)
	for key, want := range map[string]string{
		"read_files|workspace|user": ApprovalAllow, "read_files|*|user": ApprovalAsk, "write_files|workspace|user": ApprovalAllow,
		"execute_shell|*|user": ApprovalAllow, "network|any_destination|user": ApprovalAllow, "invoke_mcp_tool|*|user": ApprovalAllow,
		"read_files|machine|user": ApprovalAllow,
	} {
		if n, ok := c[key]; !ok || n.Approval != want || n.Basis != BasisProductDefault || n.Source == "" {
			t.Errorf("opencode factory %s: %+v", key, n)
		}
	}
	kf := capsOf(FromOpenCode(platform.OpenCodeScan{Product: "kilo-code", Present: true, ConfigDir: "/h/.config/kilo"}, kilo, now))
	if kf["execute_shell|*|user"].Approval != ApprovalAsk {
		t.Errorf("kilo factory bash %+v", kf["execute_shell|*|user"])
	}
	if n := c["read_files|machine|user"]; !IsTerminalRead(n) || n.Basis != BasisProductDefault || n.Source != "kb:opencode/shell_allow@"+knowledge.OpenCodeVersion {
		t.Errorf("factory terminal read %+v", n)
	}
	if _, ok := kf["read_files|machine|user"]; ok {
		t.Error("Kilo Code asks before commands: no terminal read")
	}

	// Settings: the user denies bash and edit asks; the project allows
	// everything; the managed file denies webfetch everywhere.
	set := platform.OpenCodeScan{Product: "opencode", Present: true, ConfigDir: "/h/.config/opencode", Layers: []platform.OpenCodeLayer{
		{
			Kind: platform.OpenCodeLayerUser, Path: "/h/.config/opencode/opencode.json", Status: platform.MCPSourceRead,
			Permission: map[string]platform.OpenCodeRule{"bash": {Action: "deny"}, "edit": {Patterns: map[string]string{"*": "ask"}}},
		},
		{
			Kind: platform.OpenCodeLayerProject, Path: "/r/opencode.json", Root: "/r", Status: platform.MCPSourceRead,
			Permission: map[string]platform.OpenCodeRule{"*": {Action: "allow"}},
		},
		{
			Kind: platform.OpenCodeLayerManaged, Path: "/etc/opencode/opencode.json", Status: platform.MCPSourceRead,
			Permission: map[string]platform.OpenCodeRule{"webfetch": {Action: "deny"}},
		},
		{Kind: platform.OpenCodeLayerProject, Path: "/r2/opencode.json", Root: "/r2", Status: platform.MCPSourceFailed, Reason: "parse"},
	}}
	f = FromOpenCode(set, oc, now)
	encode(f)
	c = capsOf(f)
	if n := c["execute_shell|*|user"]; n.Approval != ApprovalDeny || n.Basis != BasisConfigured || n.Rule != "permission.bash: deny" {
		t.Errorf("user bash %+v", n)
	}
	if n := c["write_files|workspace|user"]; n.Approval != ApprovalAsk {
		t.Errorf("user edit %+v", n)
	}
	if n := c["network|any_destination|user"]; n.Approval != ApprovalDeny || n.Source != "/etc/opencode/opencode.json#/permission/webfetch" {
		t.Errorf("managed webfetch %+v", n)
	}
	if n := c["execute_shell|*|project:/r"]; n.Approval != ApprovalAllow || n.Rule != "permission.*: allow" {
		t.Errorf("project bash %+v", n)
	}
	if n := c["read_files|machine|project:/r"]; n.Approval != ApprovalAllow || !IsTerminalRead(n) {
		t.Errorf("a file that allows bash gives the terminal read: %+v", n)
	}
	if n := c["network|any_destination|project:/r"]; n.Approval == ApprovalAllow {
		t.Errorf("the managed file wins over the project too: %+v", n)
	}
	if len(f.Coverage.SourcesFailed) != 1 {
		t.Errorf("failed sources %+v", f.Coverage.SourcesFailed)
	}
}
