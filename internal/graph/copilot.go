package graph

import (
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// GitHub Copilot effective permission levels (BV-140, CTO-16 Copilot §4).
const (
	copilotManual   = "manual"    // Manual permissions: what is not auto-approved asks (you)
	copilotAllowAll = "allow_all" // global auto-approve / Allow all / Autopilot: nobody
	copilotAssisted = "assisted"  // Assisted: an LLM judge decides (model, reviewer_model_permits)
	copilotBlocked  = "blocked"   // chat.agent.enabled = false: agent mode off

	// copilotTerminalAll is the target scope of a terminal catch-all rule
	// (chat.tools.terminal.autoApprove with /.*/: true): commands run without
	// asking at the scope the rule was set (CTO-16 Copilot §7 case 3).
	copilotTerminalAll = "terminal_all"
)

// FromCopilot builds GitHub Copilot's fragment in VS Code (BV-140, CTO-16
// Copilot §4–§5): its ai_client when Copilot is present; one agent_config
// resource per settings layer and MCP file read (so another agent's write can
// target them); the effective permission level as a permission_mode capability
// (manual by product default, or configured allow_all / assisted); a
// policy-disabled global auto-approve as a not-in-force capability; and the
// terminal catch-all as a configured execute_shell allow. Product defaults, the
// MCP servers and the inference edges are added by the shared pipelines. It is a
// pure function. extensionPresent marks the VS Code extension as the other
// presence signal.
func FromCopilot(scan platform.CopilotScan, extensionPresent bool, discoveredAt time.Time) Fragment {
	b := newFragmentBuilder()
	if !scan.Present && !extensionPresent {
		return b.fragment()
	}
	seen := discoveredAt.UTC().Format(time.RFC3339)
	client := clientID(knowledge.GitHubCopilotProduct, scan.ConfigDir)
	b.node(Node{
		ID: client, Kind: KindAgent, Type: AgentAIClient, Name: clientDisplayName(knowledge.GitHubCopilotProduct),
		Platform: knowledge.GitHubCopilotProduct, ConfigPath: scan.ConfigDir, DiscoveredAt: seen,
	})

	// Product defaults, built here rather than through the generic ProductDefaults
	// loop (like Hermes): the built-in tools read and write only inside the
	// workspace (CTO-16 Copilot §4), so the inference reaches workspace
	// credentials and writes workspace config. The cells decide the decider from
	// the level; these capabilities give the inference its edges.
	for _, d := range []struct{ typ, entry string }{{CapReadFiles, "read_workspace"}, {CapWriteFiles, "write_workspace"}} {
		id := "cap_" + keyHash(KindCapability, BasisProductDefault, client, d.typ, "workspace")
		src := "kb:" + knowledge.GitHubCopilotProduct + "/" + d.entry + "@" + knowledge.GitHubCopilotVersion
		b.node(Node{
			ID: id, Kind: KindCapability, Type: d.typ, Basis: BasisProductDefault, InForce: true,
			Approval: ApprovalAllow, TargetScope: "workspace", Source: src,
		})
		b.edge(Edge{ID: edgeID(EdgeHas, client, id), Type: EdgeHas, From: client, To: id, Source: src})
	}

	// 1. One agent_config resource per settings layer and MCP file read: these
	// are Copilot's own configuration, which another agent with write access in
	// the repo or profile can rewrite (CTO-16 Copilot §5).
	for _, l := range scan.Layers {
		switch l.Status {
		case platform.MCPSourceFailed:
			b.failed = append(b.failed, SourceFailed{Path: l.Path, Reason: l.Reason})
			continue
		case platform.MCPSourceRead:
			b.read[canonicalPath(l.Path)] = l.Path
			b.node(Node{ID: configResourceID(l.Path), Kind: KindResource, Type: ResourceAgentConfig, Path: l.Path, Configures: client, DiscoveredAt: seen})
		}
	}
	for _, s := range scan.MCP {
		if s.Status == platform.MCPSourceRead {
			b.read[canonicalPath(s.Path)] = s.Path
			b.node(Node{ID: configResourceID(s.Path), Kind: KindResource, Type: ResourceAgentConfig, Path: s.Path, Configures: client, DiscoveredAt: seen})
		}
	}

	// 2. The effective permission level. Manual is the product default (no
	// configured capability); allow_all and assisted are configured. A user
	// global auto-approve disabled by policy is a not-in-force capability.
	level, levelRule, userAllowAllOverridden := copilotResolveLevel(scan)
	if level == copilotAllowAll || level == copilotAssisted || level == copilotBlocked {
		id := copilotCapID(client, "mode", scan.ConfigDir, CapPermissionMode, level)
		src := "copilot:" + levelRule
		b.node(Node{
			ID: id, Kind: KindCapability, Type: CapPermissionMode, Basis: BasisConfigured, InForce: true,
			Approval: ApprovalAllow, TargetScope: level, AppliesIn: platform.MCPScopeUser, Rule: levelRule, Source: src,
		})
		b.edge(Edge{ID: edgeID(EdgeHas, client, id), Type: EdgeHas, From: client, To: id, Source: src})
	}
	if userAllowAllOverridden {
		id := copilotCapID(client, "mode_off", scan.ConfigDir, CapPermissionMode, copilotAllowAll)
		src := "copilot:chat.tools.global.autoApprove: true"
		b.node(Node{
			ID: id, Kind: KindCapability, Type: CapPermissionMode, Basis: BasisConfigured, InForce: false,
			NotInForceReason: ReasonModeNotAllowedInScope,
			Approval:         ApprovalAllow, TargetScope: copilotAllowAll, AppliesIn: platform.MCPScopeUser,
			Rule: "chat.tools.global.autoApprove: true", Source: src,
		})
		b.edge(Edge{ID: edgeID(EdgeHas, client, id), Type: EdgeHas, From: client, To: id, Source: src})
	}

	// 3. Terminal catch-all: a chat.tools.terminal.autoApprove /.*/: true in a
	// user or project layer runs every command without asking at that scope
	// (CTO-16 Copilot §7 case 3). A project layer's scope is its repo root.
	for _, l := range scan.Layers {
		if l.Status != platform.MCPSourceRead || l.TerminalAutoApproveAll == nil || !*l.TerminalAutoApproveAll {
			continue
		}
		if l.Kind == platform.CopilotLayerProject && l.Root == "" {
			continue
		}
		appliesIn := platform.MCPScopeUser
		if l.Kind == platform.CopilotLayerProject {
			appliesIn = platform.MCPScopeProject + ":" + canonicalPath(l.Root)
		}
		id := copilotCapID(client, "terminal", l.Path, CapExecuteShell, copilotTerminalAll)
		src := "copilot:" + l.Kind + ":chat.tools.terminal.autoApprove"
		b.node(Node{
			ID: id, Kind: KindCapability, Type: CapExecuteShell, Basis: BasisConfigured, InForce: true,
			Approval: ApprovalAllow, TargetScope: copilotTerminalAll, AppliesIn: appliesIn,
			Rule: "chat.tools.terminal.autoApprove: /.*/ = true", Source: src,
		})
		b.edge(Edge{ID: edgeID(EdgeHas, client, id), Type: EdgeHas, From: client, To: id, Source: src})
	}
	return b.fragment()
}

// copilotResolveLevel is Copilot's effective permission level, the rule that set
// it, and whether a user/profile global auto-approve was overridden (disabled)
// by an enterprise policy (CTO-16 Copilot §4, §7 case 4). The managed policy
// layer wins over the user and profile layers.
func copilotResolveLevel(scan platform.CopilotScan) (level, rule string, userAllowAllOverridden bool) {
	var policyAllowAll, policyAssisted, policyDisables, userAllowAll, userAssisted, agentOff bool
	var userRule, policyRule string
	for _, l := range scan.Layers {
		if l.Status != platform.MCPSourceRead {
			continue
		}
		switch l.Kind {
		case platform.CopilotLayerManaged:
			if l.GlobalAutoApprove != nil {
				if *l.GlobalAutoApprove {
					policyAllowAll, policyRule = true, "policy ChatToolsAutoApprove: 1"
				} else {
					policyDisables = true
				}
			}
			if l.Approvals == "allowAll" || l.Mode == "autopilot" {
				policyAllowAll, policyRule = true, "policy ChatDefaultConfiguration"
			}
			if l.Approvals == "assisted" {
				policyAssisted, policyRule = true, "policy ChatDefaultConfiguration: assisted"
			}
			if l.AgentEnabled != nil && !*l.AgentEnabled {
				agentOff = true
			}
		case platform.CopilotLayerUser, platform.CopilotLayerProfile:
			if l.GlobalAutoApprove != nil && *l.GlobalAutoApprove {
				userAllowAll, userRule = true, "chat.tools.global.autoApprove: true"
			}
			if l.PermissionsDefault == "autoApprove" || l.PermissionsDefault == "autopilot" {
				userAllowAll, userRule = true, "chat.permissions.default: "+l.PermissionsDefault
			}
			if l.Approvals == "allowAll" || l.Mode == "autopilot" {
				userAllowAll, userRule = true, "chat.defaultConfiguration"
			}
			if l.Approvals == "assisted" {
				userAssisted, userRule = true, "chat.defaultConfiguration: assisted"
			}
			if l.AgentEnabled != nil && !*l.AgentEnabled {
				agentOff = true
			}
		}
	}
	switch {
	case agentOff:
		return copilotBlocked, "chat.agent.enabled: false", false
	case policyAllowAll:
		return copilotAllowAll, policyRule, false
	case policyAssisted:
		return copilotAssisted, policyRule, userAllowAll
	case policyDisables:
		// The policy forbids global auto-approve: the user setting is not in force.
		return copilotManual, "policy ChatToolsAutoApprove: 0", userAllowAll
	case userAllowAll:
		return copilotAllowAll, userRule, false
	case userAssisted:
		return copilotAssisted, userRule, false
	default:
		return copilotManual, "", false
	}
}

// copilotCapID derives a Copilot configured-capability id from its natural key.
func copilotCapID(client, kind, path, typ, scope string) string {
	return "cap_" + keyHash(KindCapability, BasisConfigured, client, kind, canonicalPath(path), typ, scope)
}

// CopilotMCPServers converts the MCP servers Copilot loads to platform.MCPServer
// for the shared MCP pipeline (BV-140, CTO-16 Copilot §2): the VS Code `servers`
// files and the portable `mcpServers` files, with their secret env and headers.
func CopilotMCPServers(scan platform.CopilotScan) []platform.MCPServer {
	if !scan.Present {
		return nil
	}
	var out []platform.MCPServer
	for _, s := range scan.MCP {
		if s.Status != platform.MCPSourceRead {
			continue
		}
		scope := platform.MCPScopeUser
		if s.Root != "" {
			scope = platform.MCPScopeProject
		}
		ptrKey := "/" + s.Format + "/"
		for _, srv := range s.Servers {
			out = append(out, platform.MCPServer{
				Client:     knowledge.GitHubCopilotProduct,
				Name:       srv.ID,
				Command:    srv.Command,
				Args:       srv.Args,
				URL:        srv.URL,
				EnvKeys:    srv.EnvKeys,
				Env:        srv.Env,
				Headers:    srv.Headers,
				ConfigPath: s.Path,
				ConfigDir:  scan.ConfigDir,
				Pointer:    ptrKey + srv.ID,
				LoadsMCP:   true,
				Scope:      scope,
				ScopePath:  s.Root,
			})
		}
	}
	return out
}

// copilotInference is Copilot's reaches and writes (BV-140, CTO-16 Copilot §4).
// Built-in agent tools read and write only inside the workspace folders, so
// reaches covers the credentials in a repo root and writes covers the agent
// resources in a repo root. A chat.agent.enabled = false turns both off.
type copilotInference struct {
	client   Node
	roots    []string
	level    string
	readCap  *Node
	writeCap *Node
}

func (p prepared) copilotInference(client Node, repoRoots []string) copilotInference {
	in := copilotInference{client: client, roots: append([]string{}, repoRoots...), level: copilotManual}
	for i := range p.caps[client.ID] {
		c := p.caps[client.ID][i]
		if !c.InForce {
			continue
		}
		switch {
		case c.Type == CapPermissionMode && c.Basis == BasisConfigured:
			switch c.TargetScope {
			case copilotAllowAll, copilotAssisted, copilotBlocked:
				in.level = c.TargetScope
			}
		case c.Type == CapReadFiles && c.Approval == ApprovalAllow && in.readCap == nil:
			in.readCap = &p.caps[client.ID][i]
		case c.Type == CapWriteFiles && c.Approval == ApprovalAllow && in.writeCap == nil:
			in.writeCap = &p.caps[client.ID][i]
		}
	}
	return in
}

// reaches is one reaches edge per credential inside a workspace root: Copilot's
// built-in tools read only within the workspace (its own session files are
// excused in the rules layer, not here). None when the agent is off.
func (in copilotInference) reaches(all []Node) []Edge {
	if in.readCap == nil || in.level == copilotBlocked {
		return nil
	}
	var out []Edge
	for _, cred := range all {
		if cred.Kind != KindCredential || !credFileInWorkspace(cred, in.inWorkspace) {
			continue
		}
		out = append(out, in.edge(EdgeReaches, cred.ID, *in.readCap))
	}
	return out
}

// writes is one writes_config_of edge per agent resource inside a workspace
// root: Copilot's edit tool applies workspace edits without asking (manual or
// allow_all) or with the reviewer model (assisted). None when the agent is off.
func (in copilotInference) writes(all []Node) []Edge {
	if in.level == copilotBlocked || in.writeCap == nil {
		return nil
	}
	var out []Edge
	for _, res := range all {
		if res.Kind != KindResource || !isAgentResource(res.Type) {
			continue
		}
		file, ok := resourceFile(res.Path)
		if !ok || !in.inWorkspace(file) {
			continue
		}
		out = append(out, in.edge(EdgeWritesConfigOf, res.ID, *in.writeCap))
	}
	return out
}

// inWorkspace reports a file inside one of the repo roots.
func (in copilotInference) inWorkspace(file string) bool {
	if file == "" {
		return false
	}
	for _, r := range in.roots {
		if within(r, file) {
			return true
		}
	}
	return false
}

func (in copilotInference) edge(typ, to string, capNode Node) Edge {
	assume := baseAssumptions(AssumeUIModeChoiceUnknown)
	if in.level == copilotAssisted {
		assume = append(assume, AssumeReviewerModelPermits)
	}
	e := Edge{
		ID:          "edge_" + typ + "_" + keyHash(typ, in.client.ID, to, capNode.ID),
		Type:        typ,
		From:        in.client.ID,
		To:          to,
		Source:      capNode.Source,
		Certainty:   CertaintyInferred,
		DerivedFrom: []string{in.client.ID, capNode.ID, to},
		Assumptions: assume,
	}
	if typ == EdgeReaches {
		e.Via = capNode.ID
	}
	return e
}
