package graph

import (
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// Antigravity CLI effective permission levels (BV-140, CTO-16 Antigravity §4).
const (
	agRequestReview  = "request-review" // default: every command asks (you)
	agAlwaysProceed  = "always-proceed" // approve everything (nobody)
	agAuto           = "auto"           // agentMode auto: a reviewer model decides (model)
	agScopeMachine   = "machine"        // allowNonWorkspaceAccess: reads/writes the whole machine
	agScopeWorkspace = "workspace"      // default: only the project
)

// FromAntigravityCLI builds the Antigravity CLI's fragment (BV-140, CTO-16
// Antigravity §4): its ai_client when present; an agent_config resource for its
// settings.json; its hooks as agent_executables; product-default read/write
// capabilities scoped to the workspace (or the machine with
// allowNonWorkspaceAccess); the effective permission level; and, under
// always-proceed, configured shell and network allow capabilities. Its MCP
// servers and their config resources come through the shared pipeline. It is a
// pure function.
func FromAntigravityCLI(scan platform.AntigravityScan, discoveredAt time.Time) Fragment {
	b := newFragmentBuilder()
	if !scan.Present {
		return b.fragment()
	}
	seen := discoveredAt.UTC().Format(time.RFC3339)
	client := clientID(knowledge.AntigravityCLIProduct, scan.ConfigDir)
	b.node(Node{
		ID: client, Kind: KindAgent, Type: AgentAIClient, Name: clientDisplayName(knowledge.AntigravityCLIProduct),
		Platform: knowledge.AntigravityCLIProduct, ConfigPath: scan.ConfigDir, DiscoveredAt: seen,
	})

	// settings.json is the CLI's own configuration (own_config_writable when a
	// workspace write reaches it, i.e. with allowNonWorkspaceAccess).
	if scan.Settings.Status == platform.MCPSourceRead {
		b.read[canonicalPath(scan.Settings.Path)] = scan.Settings.Path
		b.node(Node{ID: configResourceID(scan.Settings.Path), Kind: KindResource, Type: ResourceAgentConfig, Path: scan.Settings.Path, Configures: client, DiscoveredAt: seen})
	}
	// Hooks: an agent_executable another repo agent can rewrite (CTO-16
	// Antigravity §5, §7 D).
	for _, h := range scan.Hooks {
		b.read[canonicalPath(h.Path)] = h.Path
		b.node(Node{ID: AntigravityHookID(h.Path, client), Kind: KindResource, Type: ResourceAgentExecutable, Path: h.Path, Configures: client, DiscoveredAt: seen})
	}

	nonWorkspace := scan.Settings.AllowNonWorkspaceAccess
	fileScope := agScopeWorkspace
	if nonWorkspace {
		fileScope = agScopeMachine
	}
	for _, d := range []struct{ typ, entry string }{{CapReadFiles, "read_workspace"}, {CapWriteFiles, "write_workspace"}} {
		id := "cap_" + keyHash(KindCapability, BasisProductDefault, client, d.typ, fileScope)
		src := "kb:" + knowledge.AntigravityCLIProduct + "/" + d.entry + "@" + knowledge.AntigravityCLIVersion
		b.node(Node{
			ID: id, Kind: KindCapability, Type: d.typ, Basis: BasisProductDefault, InForce: true,
			Approval: ApprovalAllow, TargetScope: fileScope, Source: src,
		})
		b.edge(Edge{ID: edgeID(EdgeHas, client, id), Type: EdgeHas, From: client, To: id, Source: src})
	}

	level, levelRule := antigravityResolveLevel(scan.Settings)
	if level == agAlwaysProceed || level == agAuto {
		id := antigravityCapID(client, "mode", scan.ConfigDir, CapPermissionMode, level)
		src := "antigravity:" + levelRule
		b.node(Node{
			ID: id, Kind: KindCapability, Type: CapPermissionMode, Basis: BasisConfigured, InForce: true,
			Approval: ApprovalAllow, TargetScope: level, AppliesIn: platform.MCPScopeUser, Rule: levelRule, Source: src,
		})
		b.edge(Edge{ID: edgeID(EdgeHas, client, id), Type: EdgeHas, From: client, To: id, Source: src})
		if level == agAlwaysProceed {
			// Approve everything: shell and network run without asking.
			for _, typ := range []string{CapExecuteShell, CapNetwork} {
				cid := antigravityCapID(client, "approve", scan.ConfigDir, typ, scopeAll)
				b.node(Node{
					ID: cid, Kind: KindCapability, Type: typ, Basis: BasisConfigured, InForce: true,
					Approval: ApprovalAllow, TargetScope: scopeAll, AppliesIn: platform.MCPScopeUser, Rule: levelRule, Source: src,
				})
				b.edge(Edge{ID: edgeID(EdgeHas, client, cid), Type: EdgeHas, From: client, To: cid, Source: src})
			}
		}
	}
	return b.fragment()
}

// AntigravityHookID is the id of an Antigravity hook as its executable resource.
func AntigravityHookID(path, client string) string {
	return "res_agent_executable_" + keyHash(ResourceAgentExecutable, canonicalPath(path), client)
}

// antigravityResolveLevel is the CLI's effective permission level and the rule
// that set it (CTO-16 Antigravity §4): always-proceed (or permissions.allow
// command(*)) approves everything; agentMode auto hands escalations to a
// reviewer model; otherwise the default request-review asks.
func antigravityResolveLevel(s platform.AntigravitySettings) (level, rule string) {
	if s.ToolPermission == agAlwaysProceed {
		return agAlwaysProceed, "toolPermission: always-proceed"
	}
	for _, r := range s.Allow {
		if strings.ReplaceAll(strings.TrimSpace(r), " ", "") == "command(*)" {
			return agAlwaysProceed, "permissions.allow: command(*)"
		}
	}
	if s.AgentMode == agAuto {
		return agAuto, "agentMode: auto"
	}
	return agRequestReview, ""
}

func antigravityCapID(client, kind, path, typ, scope string) string {
	return "cap_" + keyHash(KindCapability, BasisConfigured, client, kind, canonicalPath(path), typ, scope)
}

// antigravityInference is the CLI's reaches and writes (CTO-16 Antigravity §4):
// it reads and writes inside the workspace by default, the whole machine with
// allowNonWorkspaceAccess. agentMode auto hands the write to a reviewer model.
type antigravityInference struct {
	client   Node
	roots    []string
	level    string
	machine  bool
	readCap  *Node
	writeCap *Node
}

func (p prepared) antigravityInference(client Node, repoRoots []string) antigravityInference {
	in := antigravityInference{client: client, roots: append([]string{}, repoRoots...), level: agRequestReview}
	for i := range p.caps[client.ID] {
		c := p.caps[client.ID][i]
		if !c.InForce {
			continue
		}
		switch {
		case c.Type == CapPermissionMode && c.Basis == BasisConfigured && (c.TargetScope == agAlwaysProceed || c.TargetScope == agAuto):
			in.level = c.TargetScope
		case c.Type == CapReadFiles && c.Approval == ApprovalAllow && in.readCap == nil:
			in.readCap = &p.caps[client.ID][i]
			in.machine = in.machine || c.TargetScope == agScopeMachine
		case c.Type == CapWriteFiles && c.Approval == ApprovalAllow && in.writeCap == nil:
			in.writeCap = &p.caps[client.ID][i]
			in.machine = in.machine || c.TargetScope == agScopeMachine
		}
	}
	return in
}

func (in antigravityInference) reaches(all []Node) []Edge {
	if in.readCap == nil {
		return nil
	}
	var out []Edge
	for _, cred := range all {
		if cred.Kind != KindCredential {
			continue
		}
		if !in.machine && !credFileInWorkspace(cred, in.inWorkspace) {
			continue
		}
		out = append(out, in.edge(EdgeReaches, cred.ID, *in.readCap))
	}
	return out
}

func (in antigravityInference) writes(all []Node) []Edge {
	if in.writeCap == nil {
		return nil
	}
	var out []Edge
	for _, res := range all {
		if res.Kind != KindResource || !isAgentResource(res.Type) {
			continue
		}
		file, ok := resourceFile(res.Path)
		if !ok {
			continue
		}
		if !in.machine && !in.inWorkspace(file) {
			continue
		}
		out = append(out, in.edge(EdgeWritesConfigOf, res.ID, *in.writeCap))
	}
	return out
}

func (in antigravityInference) inWorkspace(file string) bool {
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

func (in antigravityInference) edge(typ, to string, capNode Node) Edge {
	assume := baseAssumptions(AssumeLaunchFlagsNotReadable)
	if in.level == agAuto {
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

// AntigravityCLIMCPServers is a no-op placeholder: the CLI's MCP servers already
// come through ListMCPServers as client antigravity-cli (BV-140, decision 2);
// nothing extra is converted here.
