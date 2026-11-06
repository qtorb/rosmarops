package graph

import (
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// Codex target scopes of configured capabilities (CTO-16 §4). The overview
// cells (codexCells) read them: any_path is the whole machine, working_directory
// the project it works in, workspace_sandbox a command the sandbox runs without
// asking, any_destination the network.
const (
	CodexScopeAnyPath        = "any_path"
	CodexScopeWorkingDir     = "working_directory"
	CodexScopeSandboxShell   = "workspace_sandbox"
	CodexScopeNetwork        = "any_destination"
	CodexScopeOutsideWorkdir = "outside_working_directory"
)

// FromCodex builds OpenAI Codex's fragment (BV-140, CTO-16 §4–§5): its
// ai_client when CODEX_HOME exists; one agent_config resource per config layer
// read; and the configured capabilities of each in-force layer (the user config
// and the trusted project configs) from sandbox_mode / default_permissions and
// approval_policy. Product-default capabilities come from the knowledge base
// (built where every evaluated agent's are), MCP servers and credentials from
// the shared MCP pipeline; this fragment holds only what is Codex-specific. It
// is a pure function.
func FromCodex(scan platform.CodexScan, discoveredAt time.Time) Fragment {
	b := newFragmentBuilder()
	if !scan.Exists {
		return b.fragment()
	}
	seen := discoveredAt.UTC().Format(time.RFC3339)
	client := clientID(knowledge.CodexProduct, scan.ConfigDir)
	b.node(Node{
		ID: client, Kind: KindAgent, Type: AgentAIClient, Name: clientDisplayName(knowledge.CodexProduct),
		Platform: knowledge.CodexProduct, ConfigPath: scan.ConfigDir, DiscoveredAt: seen,
	})
	for _, l := range scan.Layers {
		switch l.Status {
		case platform.MCPSourceFailed:
			b.failed = append(b.failed, SourceFailed{Path: l.Path, Reason: l.Reason})
			continue
		case platform.MCPSourceMissing:
			continue
		}
		b.read[canonicalPath(l.Path)] = l.Path
		// Every read layer file is an agent_config resource of Codex, so that
		// another agent writing it counts against Codex (own_config_writable).
		b.node(Node{ID: configResourceID(l.Path), Kind: KindResource, Type: ResourceAgentConfig, Path: l.Path, Configures: client, DiscoveredAt: seen})

		// Only the user config and the project configs contribute capabilities.
		// Profiles are chosen with --profile at launch (not readable at rest),
		// and requirements restrict rather than add; both are kept as resources
		// but add no capability node (CTO-16 §2).
		appliesIn, inForce, notInForce, caps := "", false, "", false
		switch l.Kind {
		case platform.CodexLayerUser:
			appliesIn, inForce, caps = platform.MCPScopeUser, true, true
		case platform.CodexLayerProject:
			appliesIn, caps = platform.MCPScopeProject+":"+canonicalPath(l.Root), true
			if l.Trusted {
				inForce = true
			} else {
				notInForce = ReasonWorkspaceUntrusted // CTO-16 §2: an untrusted project's config does not load
			}
		}
		if !caps {
			continue
		}
		for _, c := range codexConfiguredCaps(l) {
			id := codexCapID(client, l.Kind, l.Path, c.typ, c.scope)
			src := "codex:" + l.Kind + ":" + c.rule
			b.node(Node{
				ID: id, Kind: KindCapability, Type: c.typ, Basis: BasisConfigured,
				InForce: inForce, NotInForceReason: notInForce,
				Approval: c.approval, TargetScope: c.scope, AppliesIn: appliesIn, Rule: c.rule, Source: src,
			})
			b.edge(Edge{ID: edgeID(EdgeHas, client, id), Type: EdgeHas, From: client, To: id, Source: src})
		}
	}
	return b.fragment()
}

// codexCap is one configured capability to emit.
type codexCap struct {
	typ      string
	approval string
	scope    string
	rule     string
}

// codexConfiguredCaps derives a layer's configured capabilities from its
// sandbox_mode / default_permissions and approval_policy (CTO-16 §4). A layer
// that sets neither contributes no capability.
func codexConfiguredCaps(l platform.CodexLayer) []codexCap {
	var out []codexCap
	switch sandbox := codexEffectiveSandbox(l); sandbox {
	case "danger-full-access":
		out = append(out,
			codexCap{CapReadFiles, ApprovalAllow, CodexScopeAnyPath, "sandbox_mode: danger-full-access"},
			codexCap{CapWriteFiles, ApprovalAllow, CodexScopeAnyPath, "sandbox_mode: danger-full-access"},
			codexCap{CapExecuteShell, ApprovalAllow, scopeAll, "sandbox_mode: danger-full-access"},
			codexCap{CapNetwork, ApprovalAllow, CodexScopeNetwork, "sandbox_mode: danger-full-access"})
	case "workspace-write":
		net := ApprovalAsk
		if l.NetworkAccess != nil && *l.NetworkAccess {
			net = ApprovalAllow
		}
		out = append(out,
			codexCap{CapReadFiles, ApprovalAllow, CodexScopeAnyPath, "sandbox_mode: workspace-write"},
			codexCap{CapWriteFiles, ApprovalAllow, CodexScopeWorkingDir, "sandbox_mode: workspace-write"},
			codexCap{CapWriteFiles, ApprovalAsk, CodexScopeOutsideWorkdir, "sandbox_mode: workspace-write"},
			codexCap{CapExecuteShell, ApprovalAllow, CodexScopeSandboxShell, "sandbox_mode: workspace-write"},
			codexCap{CapNetwork, net, CodexScopeNetwork, "sandbox_mode: workspace-write"})
	case "read-only":
		out = append(out,
			codexCap{CapReadFiles, ApprovalAllow, CodexScopeAnyPath, "sandbox_mode: read-only"},
			codexCap{CapWriteFiles, ApprovalAsk, scopeAll, "sandbox_mode: read-only"},
			codexCap{CapExecuteShell, ApprovalAsk, scopeAll, "sandbox_mode: read-only"},
			codexCap{CapNetwork, ApprovalAsk, CodexScopeNetwork, "sandbox_mode: read-only"})
	}
	if l.HasApprovalPolicy && l.ApprovalPolicy != "" {
		rule := "approval_policy: " + l.ApprovalPolicy
		if l.ApprovalsReviewer != "" {
			rule += "; approvals_reviewer: " + l.ApprovalsReviewer
		}
		out = append(out, codexCap{CapPermissionMode, ApprovalAllow, l.ApprovalPolicy, rule})
	}
	return out
}

// codexEffectiveSandbox is the layer's sandbox mode: sandbox_mode, else the
// mode named by default_permissions (:read-only | :workspace |
// :danger-full-access), else "" (CTO-16 §4).
func codexEffectiveSandbox(l platform.CodexLayer) string {
	if l.SandboxMode != "" {
		return l.SandboxMode
	}
	switch l.DefaultPermissions {
	case ":read-only":
		return "read-only"
	case ":workspace":
		return "workspace-write"
	case ":danger-full-access":
		return "danger-full-access"
	}
	return ""
}

func codexCapID(client, kind, path, typ, scope string) string {
	return "cap_" + keyHash(KindCapability, BasisConfigured, client, kind, canonicalPath(path), typ, scope)
}
