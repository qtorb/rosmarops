package graph

import (
	"strconv"
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// clineScope is the scope of Cline's read and write capabilities: the whole
// machine (CTO-22 Cline §4 — the code does not distinguish inside-project from
// outside yet).
const clineScope = "machine"

// FromCline builds the Cline VS Code extension's fragment (BV-193, CTO-22 Cline
// §2–§5): its ai_client (identified by ~/.cline/data, decision 9) when present
// or the extension is installed; an agent_config resource for globalState.json;
// its effective auto-approval capabilities (read, edit, commands, browser, MCP);
// and its repository hooks as agent_executables. Its MCP servers and
// credentials come through the shared pipelines. It is a pure function.
func FromCline(scan platform.ClineScan, extensionPresent bool, discoveredAt time.Time) Fragment {
	b := newFragmentBuilder()
	if !scan.Present && !extensionPresent {
		return b.fragment()
	}
	seen := discoveredAt.UTC().Format(time.RFC3339)
	client := clientID(knowledge.ClineProduct, scan.ConfigDir)
	b.node(Node{
		ID: client, Kind: KindAgent, Type: AgentAIClient, Name: clientDisplayName(knowledge.ClineProduct),
		Platform: knowledge.ClineProduct, ConfigPath: scan.ConfigDir, DiscoveredAt: seen,
	})

	// globalState.json is Cline's configuration (own_config_writable: another
	// agent that writes it turns on executeSafeCommands, CTO-22 Cline §5).
	if scan.GlobalStateStatus == platform.MCPSourceRead {
		gs := scan.ConfigDir + "/globalState.json"
		b.read[canonicalPath(gs)] = gs
		b.node(Node{ID: configResourceID(gs), Kind: KindResource, Type: ResourceAgentConfig, Path: gs, Configures: client, DiscoveredAt: seen})
	}

	read, edit, cmd, browser, mcp := clineEffectiveActions(scan)
	basis := BasisProductDefault
	if scan.HasAutoApproval || scan.Yolo {
		basis = BasisConfigured
	}
	// When a capability is configured, the schema requires a rule and it must
	// name the literal setting that decides it, with no secret (BV-202 C5,
	// CTO-28). The whole globalState.json key path is the Source pointer. A
	// product-default capability carries neither.
	gs := scan.ConfigDir + "/globalState.json"
	yoloKey := scan.YoloKey
	if scan.Yolo && yoloKey == "" {
		yoloKey = "yoloModeToggled" // the scan collapsed both toggles into one bool
	}
	for _, c := range []struct {
		typ    string
		allow  bool
		scope  string
		action string // the autoApprovalSettings.actions key this capability reads
	}{
		{CapReadFiles, read, clineScope, "readFiles"},
		{CapWriteFiles, edit, clineScope, "editFiles"},
		{CapExecuteShell, cmd, scopeAll, "executeSafeCommands"},
		{CapNetwork, browser, scopeAll, "useBrowser"},
		{CapInvokeMCPTool, mcp, scopeAll, "useMcp"},
	} {
		approval := ApprovalAsk
		if c.allow {
			approval = ApprovalAllow
		}
		id := "cap_" + keyHash(KindCapability, basis, client, c.typ, c.scope)
		// Source and rule depend on what put the capability in force.
		src := "cline:auto_approval:" + c.typ
		rule := ""
		switch {
		case basis != BasisConfigured:
			// product default: no rule, keep the generic source.
		case scan.Yolo:
			// The legacy yolo toggle approves everything, so it is the deciding
			// setting for every capability.
			rule = yoloKey + ": true"
			src = gs + "#/" + yoloKey
		default: // HasAutoApproval: each capability is decided by its own action.
			rule = "autoApprovalSettings.actions." + c.action + ": " + strconv.FormatBool(c.allow)
			src = gs + "#/autoApprovalSettings/actions/" + c.action
		}
		n := Node{
			ID: id, Kind: KindCapability, Type: c.typ, Basis: basis, InForce: true,
			Approval: approval, TargetScope: c.scope, AppliesIn: platform.MCPScopeUser, Source: src,
		}
		if basis == BasisConfigured {
			n.Rule = rule
		}
		b.node(n)
		b.edge(Edge{ID: edgeID(EdgeHas, client, id), Type: EdgeHas, From: client, To: id, Source: src})
	}

	// Repository hooks Cline runs as processes (CTO-22 Cline §5): an
	// agent_executable with the script's project-relative path, so the rules
	// layer emits project_hook_runs_repo_script. Only when hooks are enabled.
	if scan.HooksEnabled {
		for _, h := range scan.Hooks {
			rel, ok := relTo(h.Root, h.Path)
			if !ok {
				continue
			}
			b.read[canonicalPath(h.Path)] = h.Path
			b.node(Node{ID: AntigravityHookID(h.Path, client), Kind: KindResource, Type: ResourceAgentExecutable, Path: h.Path, Configures: client, DiscoveredAt: seen, ScriptPath: rel, ScriptSize: h.Size})
		}
	}
	return b.fragment()
}

// clineEffectiveActions resolves Cline's five auto-approval actions (CTO-22
// Cline §4): a legacy yolo toggle approves everything; otherwise the saved
// autoApprovalSettings, or the fresh-install defaults (read, edit, browser and
// MCP without asking; commands asking).
func clineEffectiveActions(scan platform.ClineScan) (read, edit, cmd, browser, mcp bool) {
	if scan.Yolo {
		return true, true, true, true, true
	}
	if scan.HasAutoApproval {
		return scan.ReadFiles, scan.EditFiles, scan.ExecuteSafeCommands, scan.UseBrowser, scan.UseMcp
	}
	return true, true, false, true, true
}

// ClineMCPServers is a no-op placeholder: Cline's MCP servers already come
// through ListMCPServers as client cline (BV-193); nothing extra is converted.

// clineInference is Cline's reaches and writes (CTO-22 Cline §4–§5): it reads
// the machine without asking (reaches every credential) and, when editFiles is
// on, writes every other agent's configuration without asking.
type clineInference struct {
	client   Node
	readCap  *Node
	writeCap *Node
}

func (p prepared) clineInference(client Node, _ []string) clineInference {
	in := clineInference{client: client}
	var shellCap *Node
	for i := range p.caps[client.ID] {
		c := p.caps[client.ID][i]
		if !c.InForce || c.Approval != ApprovalAllow {
			continue
		}
		switch c.Type {
		case CapReadFiles:
			if in.readCap == nil {
				in.readCap = &p.caps[client.ID][i]
			}
		case CapWriteFiles:
			if in.writeCap == nil {
				in.writeCap = &p.caps[client.ID][i]
			}
		case CapExecuteShell:
			if shellCap == nil {
				shellCap = &p.caps[client.ID][i]
			}
		}
	}
	// Running any command without asking lets Cline write another agent's
	// configuration even when the edit tool is not auto-approved (CTO-22 Cline
	// §4): the shell is the via when there is no write-allow.
	if in.writeCap == nil {
		in.writeCap = shellCap
	}
	return in
}

func (in clineInference) reaches(all []Node) []Edge {
	if in.readCap == nil {
		return nil
	}
	var out []Edge
	for _, cred := range all {
		if cred.Kind != KindCredential {
			continue
		}
		out = append(out, in.edge(EdgeReaches, cred.ID, *in.readCap))
	}
	return out
}

func (in clineInference) writes(all []Node) []Edge {
	if in.writeCap == nil {
		return nil
	}
	var out []Edge
	for _, res := range all {
		if res.Kind != KindResource || !isAgentResource(res.Type) {
			continue
		}
		if _, ok := resourceFile(res.Path); !ok {
			continue
		}
		out = append(out, in.edge(EdgeWritesConfigOf, res.ID, *in.writeCap))
	}
	return out
}

func (in clineInference) edge(typ, to string, capNode Node) Edge {
	e := Edge{
		ID:          "edge_" + typ + "_" + keyHash(typ, in.client.ID, to, capNode.ID),
		Type:        typ,
		From:        in.client.ID,
		To:          to,
		Source:      capNode.Source,
		Certainty:   CertaintyInferred,
		DerivedFrom: []string{in.client.ID, capNode.ID, to},
		Assumptions: baseAssumptions(),
	}
	if typ == EdgeReaches {
		e.Via = capNode.ID
	}
	return e
}
