package graph_test

import (
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-140 (piece 1c): FromCodex builds Codex's ai_client, an agent_config
// resource per read layer, and the configured capabilities of the in-force
// layers — the user config and the trusted project config, not the untrusted
// one (CTO-16 §4, workspace_untrusted).
func TestFromCodex(t *testing.T) {
	seen := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	trustedRoot := `C:\src\ok`
	untrustedRoot := `C:\src\no`
	net := false
	scan := platform.CodexScan{
		ConfigDir: `C:\Users\a\.codex`,
		Exists:    true,
		Layers: []platform.CodexLayer{
			{
				Kind: platform.CodexLayerUser, Path: `C:\Users\a\.codex\config.toml`, Status: platform.MCPSourceRead,
				ApprovalPolicy: "never", HasApprovalPolicy: true, SandboxMode: "danger-full-access", NetworkAccess: &net,
			},
			{
				Kind: platform.CodexLayerProfile, Path: `C:\Users\a\.codex\work.config.toml`, Status: platform.MCPSourceRead,
				SandboxMode: "read-only",
			},
			{
				Kind: platform.CodexLayerProject, Path: trustedRoot + `\.codex\config.toml`, Root: trustedRoot, Status: platform.MCPSourceRead,
				Trusted: true, ApprovalPolicy: "on-request", HasApprovalPolicy: true, ApprovalsReviewer: "auto_review", SandboxMode: "workspace-write",
			},
			{
				Kind: platform.CodexLayerProject, Path: untrustedRoot + `\.codex\config.toml`, Root: untrustedRoot, Status: platform.MCPSourceRead,
				Trusted: false, SandboxMode: "danger-full-access",
			},
			{Kind: platform.CodexLayerRequirements, Path: `C:\ProgramData\OpenAI\Codex\requirements.toml`, Status: platform.MCPSourceRead, SandboxMode: "read-only"},
		},
	}
	f := graph.FromCodex(scan, seen)

	var client graph.Node
	configs := 0
	caps := map[string][]graph.Node{} // appliesIn → capabilities
	for _, n := range f.Nodes {
		switch {
		case n.Kind == graph.KindAgent && n.Type == graph.AgentAIClient:
			client = n
		case n.Kind == graph.KindResource && n.Type == graph.ResourceAgentConfig:
			configs++
		case n.Kind == graph.KindCapability:
			caps[n.AppliesIn] = append(caps[n.AppliesIn], n)
		}
	}
	if client.Platform != knowledge.CodexProduct || client.ConfigPath != scan.ConfigDir {
		t.Fatalf("ai_client: %+v", client)
	}
	// Every read layer (5) is an agent_config resource.
	if configs != 5 {
		t.Errorf("agent_config resources = %d, want 5", configs)
	}
	// User (danger-full-access) in force: write any_path allow; shell * allow.
	user := caps[platform.MCPScopeUser]
	if !hasCodexCap(user, graph.CapWriteFiles, graph.ApprovalAllow, graph.CodexScopeAnyPath, true, "") {
		t.Errorf("user write any_path allow in force not found: %+v", user)
	}
	if !hasCodexCap(user, graph.CapExecuteShell, graph.ApprovalAllow, "*", true, "") {
		t.Errorf("user shell * allow in force not found")
	}
	if !hasCodexCap(user, graph.CapPermissionMode, graph.ApprovalAllow, "never", true, "") {
		t.Errorf("user permission_mode never not found")
	}
	// The profile and the requirements layers add no capability.
	for scope := range caps {
		if scope == platform.MCPScopeManaged {
			t.Errorf("requirements layer emitted capabilities")
		}
	}
	// Trusted project in force; its permission_mode carries approvals_reviewer.
	tproj := caps[platform.MCPScopeProject+":"+"c:/src/ok"]
	if !hasCodexCap(tproj, graph.CapWriteFiles, graph.ApprovalAllow, graph.CodexScopeWorkingDir, true, "") {
		t.Errorf("trusted project write working_directory allow not found: %+v", tproj)
	}
	// Untrusted project not in force, reason workspace_untrusted.
	uproj := caps[platform.MCPScopeProject+":"+"c:/src/no"]
	if len(uproj) == 0 {
		t.Fatalf("untrusted project emitted no capabilities")
	}
	for _, c := range uproj {
		if c.InForce || c.NotInForceReason != graph.ReasonWorkspaceUntrusted {
			t.Errorf("untrusted project cap should be not-in-force/workspace_untrusted: %+v", c)
		}
	}
}

func hasCodexCap(caps []graph.Node, typ, approval, scope string, inForce bool, reason string) bool {
	for _, c := range caps {
		if c.Type == typ && c.Approval == approval && c.TargetScope == scope && c.InForce == inForce && c.NotInForceReason == reason && c.Basis == graph.BasisConfigured {
			return true
		}
	}
	return false
}
