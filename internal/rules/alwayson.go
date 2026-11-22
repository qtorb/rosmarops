package rules

import (
	"sort"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-251 (CTO-62 §2, §3, option A; CTO-53 review point 2): «Can act without
// you» and «Who can give it orders».

// RuleActsWithoutYou: something outside can wake the agent (a wakes edge from
// an active inbound_channel or agent_schedule, W) and it runs commands, sends
// out or writes files without the person (U): the row's decider is nobody or
// model, or you when the agent takes approvals from the chat.
const RuleActsWithoutYou = "acts_without_you"

// AssumeLaunchStateUnknown: the wake source is configured, or turns on with a
// launch flag, but whether it runs is not visible at rest (CTO-62 §2,
// certainty potential). A premise.
const AssumeLaunchStateUnknown = "launch_state_unknown"

// Who can give it orders (CTO-53 review point 2): the agent's own line.
const (
	OrdersOnlyYou        = "only_you"
	OrdersListed         = "listed"
	OrdersAnyoneAdmitted = "anyone_admitted"
	OrdersUnknown        = "unknown"  // «—»: a launch flag, the cloud, or who is admitted is not seen
	OrdersNotRead        = "not_read" // «—» with «RosmarOps doesn't read this»
)

// The detail of an acts_without_you finding (Params.Scope): which wake
// source gives it, the first that applies.
const (
	WakeAnyone  = "anyone_admitted" // an open chat
	WakeListed  = "listed"          // a list of n people
	WakeUnknown = "unknown"         // it listens, RosmarOps does not see who
	WakeTasks   = "tasks"           // only its own scheduled tasks
)

// WakeLaunchFlag is Params.ResourceType when the source turns on only with a
// launch flag (the finding's second line).
const WakeLaunchFlag = "launch_flag_required"

// LaunchFlagOf is the flag that turns a product's channels on (CTO-53 §1.2).
func LaunchFlagOf(product string) string {
	if product == knowledge.ClaudeCodeProduct {
		return "--channels"
	}
	return ""
}

// OverviewOrders is the agent's «Who can give it orders» line.
type OverviewOrders struct {
	Value string `json:"value"`
	Count *int   `json:"count,omitempty"`
}

// wake is the strongest active wake source of an agent.
type wake struct {
	kind       string // WakeAnyone, WakeListed, WakeUnknown, WakeTasks
	level      int    // delegation level: 3, 2, 2, 1
	weight     int    // 12, 8, 8, 4
	platform   string
	count      int
	launchFlag bool
	potential  bool
	source     graph.Node
	chat       []graph.Node // the active inbound channels
}

// strongestWake is agent's strongest active wake source; ok false when none.
func strongestWake(g graph.Graph, agent string) (wake, bool) {
	var best wake
	found := false
	better := func(w wake) {
		if !found || w.weight > best.weight || (w.weight == best.weight && w.kind < best.kind) {
			best, found = w, true
		}
	}
	var chat []graph.Node
	tasks := 0
	for _, s := range graph.WakeSources(g, agent) {
		if !s.Active {
			continue
		}
		r := s.Resource
		switch r.Type {
		case graph.ResourceInboundChannel:
			chat = append(chat, r)
			w := wake{
				platform: r.Platform, source: r, launchFlag: r.Status == platform.ChannelLaunchFlagRequired,
				potential: r.Status != platform.ChannelEnabledInConfig,
			}
			switch {
			case r.Approvers == platform.ApproversAnyoneAdmitted || (r.AllowAll != nil && *r.AllowAll):
				w.kind, w.level, w.weight = WakeAnyone, 3, 12
			case r.Approvers == platform.ApproversListed && r.AllowlistCount != nil:
				w.kind, w.level, w.weight, w.count = WakeListed, 2, 8, *r.AllowlistCount
			default:
				w.kind, w.level, w.weight = WakeUnknown, 2, 8
			}
			better(w)
		case graph.ResourceAgentSchedule:
			tasks += *r.ActiveCount
			better(wake{kind: WakeTasks, level: 1, weight: 4, source: r})
		}
	}
	if found && best.kind == WakeTasks {
		best.count = tasks
	}
	best.chat = chat
	return best, found
}

// ordersOf is agent a's «Who can give it orders» line (CTO-53 §5.4 as
// signed): only you without an inbound channel; the list, anyone admitted, or
// «—» when it depends on a launch flag or who is admitted is not seen. Codex
// and Antigravity CLI have no documented place for their tasks: not read. The
// agents whose sources are only in the cloud are «—».
func ordersOf(g graph.Graph, a OverviewAgent) *OverviewOrders {
	if !a.Evaluated {
		return nil
	}
	switch a.Product {
	case knowledge.CodexProduct, knowledge.AntigravityCLIProduct:
		return &OverviewOrders{Value: OrdersNotRead}
	case knowledge.CursorProduct, knowledge.GitHubCopilotProduct, knowledge.GrokBuildProduct, knowledge.GrokBotProduct:
		return &OverviewOrders{Value: OrdersUnknown}
	}
	w, ok := strongestWake(g, a.AgentID)
	if !ok || w.kind == WakeTasks {
		return &OverviewOrders{Value: OrdersOnlyYou}
	}
	switch {
	case w.launchFlag:
		return &OverviewOrders{Value: OrdersUnknown}
	case w.kind == WakeAnyone:
		return &OverviewOrders{Value: OrdersAnyoneAdmitted}
	case w.kind == WakeListed:
		n := w.count
		return &OverviewOrders{Value: OrdersListed, Count: &n}
	}
	return &OverviewOrders{Value: OrdersUnknown}
}

// chatApproves reports that the agent takes approvals from one of its active
// channels, so «you» there is whoever answers the chat (CTO-62 §2, U):
// Hermes Agent when approvals.approvers is not only the host; Claude Code,
// whose channel relays permission prompts («Anyone who can reply through the
// channel can approve or deny tool use»).
func chatApproves(product string, chat []graph.Node) bool {
	for _, c := range chat {
		switch product {
		case knowledge.HermesProduct:
			if c.Approvers != platform.ApproversHostOnly {
				return true
			}
		case knowledge.ClaudeCodeProduct:
			return true
		}
	}
	return false
}

// withoutYouRows are the rows of U (CTO-62 §2).
var withoutYouRows = map[string]bool{RowRunsCommands: true, RowSendsOut: true, RowWritesFiles: true}

// actsWithoutYou reports U for agent a.
func actsWithoutYou(a OverviewAgent, chatYou bool) bool {
	ok := func(d string) bool {
		return d == DeciderNobody || d == DeciderModel || (chatYou && d == DeciderYou)
	}
	for _, c := range a.Rows {
		if !withoutYouRows[c.Row] {
			continue
		}
		if ok(c.Decider) || (c.Also != nil && ok(c.Also.Decider)) {
			return true
		}
	}
	return false
}

// actsWithoutYouFindings are the acts_without_you findings of the overview's
// agents (W and U), yellow, weighed in the index by Params.Scope (12, 8, 4).
func actsWithoutYouFindings(in Input, g graph.Graph, agents []OverviewAgent) []Finding {
	var out []Finding
	for _, a := range agents {
		if !a.Evaluated {
			continue
		}
		w, ok := strongestWake(g, a.AgentID)
		if !ok || !actsWithoutYou(a, chatApproves(a.Product, w.chat)) {
			continue
		}
		var assume []string
		conf := ConfidenceHigh
		if w.potential && w.kind != WakeTasks {
			assume, conf = []string{AssumeLaunchStateUnknown}, ConfidenceMedium
		}
		resType := w.source.Type
		if w.launchFlag {
			resType = WakeLaunchFlag
		}
		out = append(out, Finding{
			ID: findingID(RuleActsWithoutYou, a.AgentID), Rule: RuleActsWithoutYou, RuleVersion: RuleVersion,
			Severity: SeverityYellow, Confidence: conf,
			Params: Params{Agent: a.Agent, Service: w.platform, Scope: w.kind, ResourceType: resType, Count: w.count},
			Evidence: Evidence{
				Nodes: []string{a.AgentID, w.source.ID}, Edges: []string{}, Locations: []string{w.source.Path},
				Assumptions: append([]string{}, assume...),
			},
			Group:   group(in, []string{w.source.Path}),
			Actions: []Action{},
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// WakeWeight is the index weight of an acts_without_you finding (CTO-62
// §2.1, option A): 12 open to anyone admitted, 8 a list or unknown who, 4 only
// its own tasks.
func WakeWeight(f Finding) float64 {
	switch f.Params.Scope {
	case WakeAnyone:
		return 12
	case WakeTasks:
		return 4
	}
	return 8
}

// WakeDelegation is the delegation level and input of agent's strongest wake
// source on g (CTO-62 §2.2): 3 inbound:anyone_admitted, 2 inbound:listed, 1
// schedule; 0 and "" without one.
func WakeDelegation(g graph.Graph, agent string) (int, string) {
	w, ok := strongestWake(g, agent)
	if !ok {
		return 0, ""
	}
	switch w.kind {
	case WakeAnyone:
		return 3, "inbound:anyone_admitted"
	case WakeTasks:
		return 1, "schedule"
	}
	return 2, "inbound:listed"
}

// actingAgents are the overview agents that act without you (W and U).
func actingAgents(g graph.Graph, agents []OverviewAgent) map[string]bool {
	out := map[string]bool{}
	for _, a := range agents {
		if !a.Evaluated {
			continue
		}
		if w, ok := strongestWake(g, a.AgentID); ok && actsWithoutYou(a, chatApproves(a.Product, w.chat)) {
			out[a.AgentID] = true
		}
	}
	return out
}

// envCredentials are, for each agent that acts without you, the credentials
// its environment loads at start (CTO-62 §3): a location under its
// configuration folder, or received by an MCP server it loads.
func envCredentials(in Input, res Result) map[[2]string]bool {
	g := in.Graph
	acting := actingAgents(g, res.AgentsOverview)
	if len(acting) == 0 {
		return nil
	}
	nodes := map[string]graph.Node{}
	for _, n := range g.Nodes {
		nodes[n.ID] = n
	}
	loads := map[string]map[string]bool{} // agent → MCP servers it loads
	receives := map[string][]string{}     // credential → servers that receive it
	for _, e := range g.Edges {
		switch e.Type {
		case graph.EdgeLoads:
			if !LoadsInForce(e) {
				continue // a rejected project server is not loaded (BV-274)
			}
			if loads[e.From] == nil {
				loads[e.From] = map[string]bool{}
			}
			loads[e.From][e.To] = true
		case graph.EdgeReceives:
			receives[e.From] = append(receives[e.From], e.To)
		}
	}
	out := map[[2]string]bool{}
	for agent := range acting {
		dir := nodes[agent].ConfigPath
		for _, n := range g.Nodes {
			if n.Kind != graph.KindCredential {
				continue
			}
			in := false
			for _, f := range graph.CredentialFiles(n) {
				if dir != "" && graph.Within(dir, f) {
					in = true
				}
			}
			for _, srv := range receives[n.ID] {
				if loads[agent][srv] {
					in = true
				}
			}
			if in {
				out[[2]string{agent, n.ID}] = true
			}
		}
	}
	return out
}

// finishAlwaysOn adds to res, after the overview (BV-251): each agent's «Who
// can give it orders» line and memory, and the acts_without_you findings, with
// the person's dismissed and accepted lists applied as to every finding.
func finishAlwaysOn(in Input, res *Result) {
	g := in.Graph
	for i := range res.AgentsOverview {
		a := &res.AgentsOverview[i]
		a.Orders = ordersOf(g, *a)
		if !a.Evaluated {
			continue
		}
		for _, m := range graph.MemoryOf(g, a.AgentID) {
			om := OverviewMemory{Path: m.Path}
			if m.FileCount != nil {
				om.Files = *m.FileCount
			}
			if m.TotalBytes != nil {
				om.Bytes = *m.TotalBytes
			}
			a.Memory = append(a.Memory, om)
		}
	}
	acts := actsWithoutYouFindings(in, g, res.AgentsOverview)
	if len(acts) == 0 {
		return
	}
	dismissed := map[string]bool{}
	for _, d := range in.Dismissed {
		dismissed[d.ID] = true
	}
	accepted := map[string]string{}
	for _, a := range in.Accepted {
		accepted[a.ID] = a.Date
	}
	for _, f := range acts {
		if d, ok := accepted[f.ID]; ok {
			f.Accepted = &Accepted{Date: d}
		}
		if dismissed[f.ID] {
			res.Dismissed = append(res.Dismissed, f)
			continue
		}
		res.Findings = append(res.Findings, f)
	}
	sortFindings(res.Findings)
	sortFindings(res.Dismissed)
	res.Today = pickToday(res.Findings)
}

// AssumeSandboxAutoAllowByVersion (CTO-64 §2, advisor 17-11): Claude Code's
// runs row when its sandbox is effective and autoAllowBashIfSandboxed is not
// written. RosmarOps cannot read the installed version and assumes true (the
// default before 2.1.325). It changes no score and no class.
const AssumeSandboxAutoAllowByVersion = "sandbox_autoallow_default_by_version"

// autoAllowByVersion adds AssumeSandboxAutoAllowByVersion to Claude Code's
// runs row when its sandbox is effective and the key is not written.
func autoAllowByVersion(rows []OverviewCell, v *agentView) {
	if v == nil || v.node.Platform != knowledge.ClaudeCodeProduct || !graph.SandboxAutoAllowUnwritten(v.inForce, v.repoRoots) {
		return
	}
	for i := range rows {
		if rows[i].Row == RowRunsCommands {
			rows[i].Data.Assumptions = sortedUnique(append(rows[i].Data.Assumptions, AssumeSandboxAutoAllowByVersion))
		}
	}
}
