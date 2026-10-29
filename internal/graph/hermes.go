package graph

import (
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// isolatedBackends are Hermes Agent terminal backends that run its terminal,
// file and code tools inside an isolated environment, not on the computer
// (BV-111, contract §1; Hermes docs, user-guide/features/tools).
var isolatedBackends = map[string]bool{
	"docker": true, "ssh": true, "modal": true, "daytona": true, "vercel_sandbox": true,
}

// IsolatedBackend reports whether a terminal.backend value runs the tools off
// the computer.
func IsolatedBackend(b string) bool { return isolatedBackends[b] }

// HermesScopeIsolated marks a read/write/shell capability that runs inside an
// isolated backend: it does not reach the files of the computer (BV-111).
const HermesScopeIsolated = "isolated_backend"

// HermesDefaults builds the capability nodes of every Hermes Agent ai_client
// in nodes from its knowledge base and its config.yaml (BV-111): the
// product_default read/write/shell/network/invoke_mcp_tool capabilities — read,
// write and shell marked configured and scoped to the sandbox when the backend
// is isolated — and the permission_mode capability of approvals.mode (smart
// when absent). It is a pure function.
func HermesDefaults(nodes []Node, base knowledge.Base, scan platform.HermesScan) Fragment {
	byDir := map[string]platform.HermesConfig{}
	for _, c := range scan.Configs {
		byDir[canonicalPath(c.ConfigDir)] = c
	}
	b := newFragmentBuilder()
	for _, n := range nodes {
		if n.Kind != KindAgent || n.Type != AgentAIClient || n.Platform != base.Product {
			continue
		}
		cfg := byDir[canonicalPath(n.ConfigPath)]
		isolated := isolatedBackends[cfg.TerminalBackend]
		for _, e := range base.Entries {
			if !e.IsCapability() {
				continue
			}
			basis, scope, rule := BasisProductDefault, e.TargetScope, ""
			if isolated && (e.Type == CapReadFiles || e.Type == CapWriteFiles || e.Type == CapExecuteShell) {
				// The tool runs in the sandbox: a configured scope that does not
				// reach the computer's files (contract §1).
				basis, scope, rule = BasisConfigured, HermesScopeIsolated, "terminal.backend: "+cfg.TerminalBackend
			}
			src := "kb:" + base.Product + "/" + e.ID + "@" + base.Version
			id := hermesCapID(n.ID, basis, e.Type, e.ID)
			b.node(Node{
				ID: id, Kind: KindCapability, Type: e.Type, Basis: basis, InForce: true,
				Approval: e.Approval, TargetScope: scope, Rule: rule, Source: src,
			})
			b.edge(Edge{ID: edgeID(EdgeHas, n.ID, id), Type: EdgeHas, From: n.ID, To: id, Source: src})
		}
		// permission_mode from approvals.mode (configured): smart when absent.
		mode := cfg.ApprovalMode
		if mode == "" {
			mode = "smart"
		}
		src := "hermes:approvals.mode"
		id := hermesCapID(n.ID, BasisConfigured, CapPermissionMode, "approvals_mode")
		b.node(Node{
			ID: id, Kind: KindCapability, Type: CapPermissionMode, Basis: BasisConfigured, InForce: true,
			Approval: ApprovalAllow, TargetScope: mode, Rule: "approvals.mode: " + mode, AppliesIn: "user", Source: src,
		})
		b.edge(Edge{ID: edgeID(EdgeHas, n.ID, id), Type: EdgeHas, From: n.ID, To: id, Source: src})
	}
	return b.fragment()
}

func hermesCapID(client, basis, typ, entry string) string {
	return "cap_" + keyHash(KindCapability, basis, client, typ, entry)
}

// hermesInference holds what one Hermes Agent client's edges are inferred
// from (BV-111). Hermes reads and writes any path with its tools (allow), so
// it reaches every credential and writes every agent resource — unless its
// terminal backend is isolated, which keeps the tools off the computer.
type hermesInference struct {
	client   Node
	readCap  *Node // read_files any_path, in force
	writeCap *Node // write_files any_path, in force
}

func (p prepared) hermesInference(client Node) hermesInference {
	in := hermesInference{client: client}
	for i := range p.caps[client.ID] {
		c := p.caps[client.ID][i]
		if !c.InForce || c.Approval != ApprovalAllow || c.TargetScope != "any_path" {
			continue
		}
		switch c.Type {
		case CapReadFiles:
			in.readCap = &p.caps[client.ID][i]
		case CapWriteFiles:
			in.writeCap = &p.caps[client.ID][i]
		}
	}
	return in
}

// reaches is one reaches edge per credential Hermes can read, except its own
// account files (its read tool refuses its own credential stores and .env,
// contract §1): the whole computer is in scope.
func (in hermesInference) reaches(all []Node) []Edge {
	if in.readCap == nil {
		return nil
	}
	var out []Edge
	for _, cred := range all {
		if cred.Kind != KindCredential || in.owns(cred) {
			continue
		}
		out = append(out, in.edge(EdgeReaches, cred.ID, *in.readCap))
	}
	return out
}

// writes is one writes_config_of edge per agent resource with a file that
// Hermes can write, except its own config.yaml (its write tool blocks its own
// config, contract §1): its own instructions (SOUL.md) are kept.
func (in hermesInference) writes(all []Node) []Edge {
	if in.writeCap == nil {
		return nil
	}
	var out []Edge
	for _, res := range all {
		if res.Kind != KindResource || !isAgentResource(res.Type) {
			continue
		}
		if res.Type == ResourceAgentConfig && res.Configures == in.client.ID {
			continue // its own config.yaml is blocked
		}
		if _, ok := resourceFile(res.Path); !ok {
			continue
		}
		out = append(out, in.edge(EdgeWritesConfigOf, res.ID, *in.writeCap))
	}
	return out
}

// owns reports that a credential is Hermes's own account file (under its
// configuration folder): its read tool refuses those.
func (in hermesInference) owns(cred Node) bool {
	files := credentialFiles(cred)
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		if !within(in.client.ConfigPath, f) {
			return false
		}
	}
	return true
}

func (in hermesInference) edge(typ, to string, cap Node) Edge {
	e := Edge{
		ID:          "edge_" + typ + "_" + keyHash(typ, in.client.ID, to, cap.ID),
		Type:        typ,
		From:        in.client.ID,
		To:          to,
		Source:      cap.Source,
		Certainty:   CertaintyInferred,
		DerivedFrom: []string{in.client.ID, cap.ID, to},
		Assumptions: baseAssumptions(),
	}
	if typ == EdgeReaches {
		e.Via = cap.ID
	}
	return e
}
