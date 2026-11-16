package graph

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/declared"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// Grok Bot (BV-248, CTO-56 §2): RosmarOps does not read it at rest. Its agent
// exists only when the person says they use it (B0 = yes), and its one
// setting on this computer, «Execution on this computer» (B1), is declared:
//   - Ask every time: commands ask (and reading or sending goes through a
//     command), on the whole computer;
//   - Always allow: commands run, read and send without a person, on the whole
//     computer and to any destination; Auto-review does not cover it
//     («Auto Review governs only work inside the hosted computer»);
//   - Never allow: blocked.
//
// Its account settings (B2–B4) are not capabilities of this computer: the
// rules layer reads them for the risk kinds of its cloud work.

// GrokBotScopeMachine is the target of Grok Bot's reads and writes through a
// command: the whole computer.
const GrokBotScopeMachine = "machine"

// AssumeMeasuredLocalSetting is the assumption of a way through a Grok Bot
// setting read from ~/.grokbot/settings.json (BV-248, CTO-56 §7.1).
const AssumeMeasuredLocalSetting = "measured_local_setting"

// grokBotLocalRule is the rule of Grok Bot's measured local execution.
const grokBotLocalRule = "localToolPermission="

// GrokBotFacts are the facts of a Grok Bot scan (CTO-57 §4): presence, and
// B1 and B2 when settings.json was read.
func GrokBotFacts(scan platform.GrokBotScan) declared.GrokBotFacts {
	f := declared.GrokBotFacts{Present: scan.Present}
	if scan.Status == platform.MCPSourceRead {
		f.LocalRead, f.Local = true, declared.LeastLocal(scan.LocalPermissions)
		if scan.Accounts > 0 {
			f.AutoReviewRead, f.AutoReview = true, declared.On
			if scan.AutoReviewOff {
				f.AutoReview = declared.Off
			}
		}
	}
	return f
}

// FromGrokBot builds Grok Bot's fragment from its scan and the person's
// answers: no agent unless its app folder exists or B0 is yes. B1 is a
// configured capability when settings.json was read (measured), a declared one
// otherwise. A settings.json not read is a failed source.
func FromGrokBot(scan platform.GrokBotScan, answers map[string]string, discoveredAt time.Time) Fragment {
	b := newFragmentBuilder()
	switch scan.Status {
	case platform.MCPSourceRead:
		b.read[canonicalPath(scan.SettingsPath)] = scan.SettingsPath
	case platform.MCPSourceFailed:
		b.failed = append(b.failed, SourceFailed{Path: scan.SettingsPath, Reason: scan.Reason})
	}
	st := declared.ResolveGrokBot(GrokBotFacts(scan), declared.Clean(declared.GrokBot, answers))
	if !st.Present {
		return b.fragment()
	}
	configDir := ""
	if scan.SettingsPath != "" {
		configDir = filepath.Dir(scan.SettingsPath)
	}
	client := clientID(knowledge.GrokBotProduct, configDir)
	b.node(Node{
		ID: client, Kind: KindAgent, Type: AgentAIClient, Name: clientDisplayName(knowledge.GrokBotProduct),
		Platform: knowledge.GrokBotProduct, ConfigPath: configDir, DiscoveredAt: discoveredAt.UTC().Format(time.RFC3339),
	})
	a := st.Answers[declared.B1LocalExecution]
	measured := st.Measured[declared.B1LocalExecution]
	add := func(typ, approval, scope string) {
		if measured {
			c := capContext{b: b, client: client, file: scan.SettingsPath, appliesIn: platform.MCPScopeUser}
			c.add(Node{Type: typ, Approval: approval, TargetScope: scope, InForce: true}, grokBotLocalRule+a.Code+"/"+typ, "/localToolPermission", nil)
			return
		}
		n := Node{
			ID:   "cap_" + keyHash(KindCapability, BasisDeclared, client, declared.B1LocalExecution, typ),
			Kind: KindCapability, Type: typ, Basis: BasisDeclared, InForce: true,
			Approval: approval, TargetScope: scope, AppliesIn: platform.MCPScopeUser,
			Source: declaredSource(declared.GrokBot, declared.B1LocalExecution, a.Code, a.Declared),
		}
		if b.node(n) {
			b.edge(Edge{ID: edgeID(EdgeHas, client, n.ID), Type: EdgeHas, From: client, To: n.ID, Source: n.Source})
		}
	}
	switch a.Code {
	case declared.LocalAlways:
		add(CapExecuteShell, ApprovalAllow, scopeAll)
		add(CapReadFiles, ApprovalAllow, GrokBotScopeMachine)
		add(CapWriteFiles, ApprovalAllow, GrokBotScopeMachine)
		add(CapNetwork, ApprovalAllow, scopeAll)
	case declared.LocalNever:
		add(CapExecuteShell, ApprovalDeny, scopeAll)
		add(CapNetwork, ApprovalDeny, scopeAll)
	default: // Ask every time
		add(CapExecuteShell, ApprovalAsk, scopeAll)
		add(CapNetwork, ApprovalAsk, scopeAll)
	}
	return b.fragment()
}

// grokBotInference is Grok Bot's: with Always allow, a reaches edge to every
// credential and a writes_config_of edge to every agent resource with a file,
// through its declared commands.
type grokBotInference struct {
	client           Node
	readCap, writeCp *Node
}

func (p prepared) grokBotInference(client Node) grokBotInference {
	in := grokBotInference{client: client}
	for i := range p.caps[client.ID] {
		c := p.caps[client.ID][i]
		if !c.InForce || c.Approval != ApprovalAllow || c.TargetScope != GrokBotScopeMachine {
			continue
		}
		switch c.Type {
		case CapReadFiles:
			in.readCap = &p.caps[client.ID][i]
		case CapWriteFiles:
			in.writeCp = &p.caps[client.ID][i]
		}
	}
	return in
}

func (in grokBotInference) reaches(all []Node) []Edge {
	if in.readCap == nil {
		return nil
	}
	var out []Edge
	for _, cred := range all {
		if cred.Kind == KindCredential {
			out = append(out, in.edge(EdgeReaches, cred.ID, *in.readCap))
		}
	}
	return out
}

func (in grokBotInference) writes(all []Node) []Edge {
	if in.writeCp == nil {
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
		out = append(out, in.edge(EdgeWritesConfigOf, res.ID, *in.writeCp))
	}
	return out
}

func (in grokBotInference) edge(typ, to string, c Node) Edge {
	e := Edge{
		ID:          "edge_" + typ + "_" + keyHash(typ, in.client.ID, to, c.ID),
		Type:        typ,
		From:        in.client.ID,
		To:          to,
		Source:      c.Source,
		Certainty:   CertaintyInferred,
		DerivedFrom: []string{in.client.ID, c.ID, to},
		Assumptions: append(baseAssumptions(), GrokBotAssumption(c)),
	}
	if typ == EdgeReaches {
		e.Via = c.ID
	}
	return e
}

// GrokBotAssumption is the assumption of a way through one of Grok Bot's local
// capabilities: measured_local_setting when read, else the declared one.
func GrokBotAssumption(c Node) string {
	if c.Basis == BasisConfigured && strings.HasPrefix(c.Rule, grokBotLocalRule) {
		return AssumeMeasuredLocalSetting
	}
	return DeclaredAssumption(c)
}
