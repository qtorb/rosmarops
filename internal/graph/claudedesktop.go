package graph

import (
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// Claude Desktop in the graph (BV-277). Outside Cowork, which is not read
// and not scored, what the app may do on the computer goes through MCP tools
// (the servers of claude_desktop_config.json, through the shared pipeline,
// and its desktop extensions). From the factory it asks before each MCP
// tool call (knowledge base entry mcp_ask), but each tool can be set to
// «Always allow», and those approvals live in the app's databases, which
// are never opened. So, as for Cursor, the least controlled case is
// assumed: one capability, invoke_mcp_tool allow, from the knowledge base
// entry mcp_always_allow_assumed (ui_mode_choice_unknown in the overview).
// When the administrator's policy turns off persistent «Always allow»
// approvals (mcpPersistentAlwaysAllowEnabled false), the capability is
// configured ask, with that policy as its rule and source.

// claudeDesktopMCPEntry is the knowledge base entry the capability rests
// on without a policy: the least controlled case.
const claudeDesktopMCPEntry = "mcp_always_allow_assumed"

// FromClaudeDesktop builds Claude Desktop's fragment: its ai_client, its
// capability to call MCP tools, and its managed policy as a source read or
// failed. It is a pure function.
func FromClaudeDesktop(scan platform.ClaudeDesktopScan, base knowledge.Base, discoveredAt time.Time) Fragment {
	b := newFragmentBuilder()
	if !scan.Present {
		return b.fragment()
	}
	seen := discoveredAt.UTC().Format(time.RFC3339)
	client := clientID(knowledge.ClaudeDesktopProduct, scan.ConfigDir)
	b.node(Node{
		ID: client, Kind: KindAgent, Type: AgentAIClient, Name: clientDisplayName(knowledge.ClaudeDesktopProduct),
		Platform: knowledge.ClaudeDesktopProduct, ConfigPath: scan.ConfigDir, DiscoveredAt: seen,
	})
	m := scan.Managed
	switch m.Status {
	case platform.MCPSourceFailed:
		b.failed = append(b.failed, SourceFailed{Path: m.Path, Reason: m.Reason})
	case platform.MCPSourceRead:
		b.read[canonicalPath(m.Path)] = m.Path
	}
	approval, basis, rule, source := "", BasisProductDefault, "", ""
	for _, e := range base.Entries {
		if e.ID == claudeDesktopMCPEntry {
			approval, source = e.Approval, "kb:"+knowledge.ClaudeDesktopProduct+"/"+e.ID+"@"+base.Version
		}
	}
	if approval == "" {
		return b.fragment()
	}
	if m.Status == platform.MCPSourceRead && m.PersistentAlwaysAllow != nil && !*m.PersistentAlwaysAllow {
		basis, rule, source = BasisConfigured, "mcpPersistentAlwaysAllowEnabled: false", m.Path+"#mcpPersistentAlwaysAllowEnabled"
		approval = ApprovalAsk
	}
	id := "cap_" + keyHash(KindCapability, basis, client, CapInvokeMCPTool, scopeAll, platform.MCPScopeUser, source)
	b.node(Node{
		ID: id, Kind: KindCapability, Type: CapInvokeMCPTool, Basis: basis, InForce: true,
		Approval: approval, TargetScope: scopeAll, AppliesIn: platform.MCPScopeUser, Rule: rule, Source: source,
	})
	b.edge(Edge{ID: edgeID(EdgeHas, client, id), Type: EdgeHas, From: client, To: id, Source: source})
	return b.fragment()
}

// ClaudeDesktopServers applies Claude Desktop's managed policy to the MCP
// servers of its configuration file: with isLocalDevMcpEnabled false the app
// loads none of them, so they stay on disk (their keys are still
// credentials) but load nothing.
func ClaudeDesktopServers(servers []platform.MCPServer, scan platform.ClaudeDesktopScan) []platform.MCPServer {
	m := scan.Managed
	if m.Status != platform.MCPSourceRead || m.LocalMCP == nil || *m.LocalMCP {
		return servers
	}
	out := make([]platform.MCPServer, len(servers))
	for i, s := range servers {
		if s.Client == knowledge.ClaudeDesktopProduct {
			s.LoadsMCP = false
		}
		out[i] = s
	}
	return out
}
