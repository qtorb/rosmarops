package graph

import (
	"sort"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// The orchestrators of CTO-76 in the graph (BV-268). Each is an orchestrator
// agent with a launches edge to each installed agent it starts:
//   - Paseo and Claude Squad keep the mode of what they start in plain
//     files, so their launches edges are observed, basis configured, with
//     the file as their source;
//   - Orca keeps it in its database, which RosmarOps does not open, so its
//     edges are inferred from its factory default (basis product_default,
//     source kb:orca/<agent>@<version>) with the assumption
//     orchestrator_settings_not_read.
//
// Paseo's OpenAI keys are credentials identified by their path (as
// Paperclip's), its active schedules agent_schedule resources that wake it;
// Orca's hooks in another agent's configuration are writes_config_of edges.

// AssumeOrchestratorSettingsNotRead: a launches edge from an orchestrator's
// factory default assumes the person has not changed that setting, which is
// only readable in its database.
const AssumeOrchestratorSettingsNotRead = "orchestrator_settings_not_read"

// OrchestratorAgentID is the orchestrator node of a product's folder.
func OrchestratorAgentID(product, dir string) string {
	return "agent_orchestrator_" + keyHash(AgentOrchestrator, product, canonicalPath(dir))
}

// clientsByProduct are f's ai_client nodes by their product code.
func clientsByProduct(f Fragment) map[string][]Node {
	out := map[string][]Node{}
	for _, n := range f.Nodes {
		if n.Kind == KindAgent && n.Type == AgentAIClient {
			out[n.Platform] = append(out[n.Platform], n)
		}
	}
	return out
}

// AddOrchestrators adds scan's Paseo, Claude Squad and Orca to f. Agents
// and their configuration resources must already be in f.
func AddOrchestrators(f Fragment, scan platform.OrchestratorScan, kb knowledge.OrchestratorsBase, salt []byte, now time.Time) Fragment {
	if scan.Paseo == nil && scan.ClaudeSquad == nil && scan.Orca == nil {
		return f
	}
	seen := now.UTC().Format(time.RFC3339)
	clients := clientsByProduct(f)
	b := newFragmentBuilder()
	orchestrator := func(product, dir string) string {
		id := OrchestratorAgentID(product, dir)
		b.node(Node{ID: id, Kind: KindAgent, Type: AgentOrchestrator, Name: clientDisplayName(product), Platform: product, ConfigPath: dir, DiscoveredAt: seen})
		return id
	}
	// observed is one observed launches edge per installed agent: skips_all
	// from the first file that sets a skipping mode, else keeps its settings
	// from the first file that names it.
	type pick struct {
		skips  bool
		source string
		launch knowledge.OrchestratorLaunch
	}
	observed := func(orch string, picks map[string]pick) {
		agents := make([]string, 0, len(picks))
		for a := range picks {
			agents = append(agents, a)
		}
		sort.Strings(agents)
		for _, a := range agents {
			p := picks[a]
			override := ApprovalKeepsAgentSettings
			if p.skips {
				override = ApprovalSkipsAll
			}
			for _, n := range clients[a] {
				b.edge(Edge{
					ID: edgeID(EdgeLaunches, orch, n.ID), Type: EdgeLaunches, From: orch, To: n.ID,
					Source: p.source, Certainty: CertaintyObserved, ApprovalOverride: override,
					EnvPassthrough: p.launch.EnvPassthrough, Basis: "configured",
					DerivedFrom: []string{orch, n.ID}, Assumptions: []string{},
				})
			}
		}
	}
	note := func(picks map[string]pick, l knowledge.OrchestratorLaunch, skips bool, source string) {
		cur, ok := picks[l.Agent]
		if !ok || (skips && !cur.skips) {
			picks[l.Agent] = pick{skips: skips, source: source, launch: l}
		}
	}

	if p := scan.Paseo; p != nil {
		kp, _ := kb.Product(knowledge.PaseoProduct)
		orch := orchestrator(knowledge.PaseoProduct, p.Home)
		picks := map[string]pick{}
		for _, a := range p.Agents {
			if a.Status != platform.MCPSourceRead || a.Archived {
				continue
			}
			if l, ok := kp.LaunchByID(a.Provider); ok {
				note(picks, l, l.Skips(a.Mode), a.Path)
			}
		}
		for _, s := range p.Schedules {
			if s.Status != platform.MCPSourceRead || s.ScheduleStatus != "active" {
				continue
			}
			sch := Node{
				ID:   "res_agent_schedule_" + keyHash(ResourceAgentSchedule, orch, canonicalPath(s.Path)),
				Kind: KindResource, Type: ResourceAgentSchedule, Path: s.Path, DiscoveredAt: seen,
			}
			b.node(sch)
			b.edge(Edge{ID: edgeID(EdgeWakes, sch.ID, orch), Type: EdgeWakes, From: sch.ID, To: orch, Source: s.Path + "#/cadence", Certainty: CertaintyObserved})
			if l, ok := kp.LaunchByID(s.Provider); ok {
				note(picks, l, l.Skips(s.Mode), s.Path)
			}
		}
		observed(orch, picks)
		for _, ptr := range p.APIKeys {
			loc := p.ConfigPath + "#" + ptr
			fp := PathFingerprint(salt, loc)
			b.node(Node{
				ID: "cred_" + keyHash(fp), Kind: KindCredential, Type: CredentialAPIKey, Service: "openai",
				Locations: []string{loc}, DiscoveredAt: seen,
				IdentifiedBy: IdentifiedByPath, PathFingerprint: fp, FileOwner: ownerOr(p.ConfigOwner), FileMode: p.ConfigMode,
			})
		}
	}

	if c := scan.ClaudeSquad; c != nil && c.ConfigStatus != platform.MCPSourceFailed {
		kc, _ := kb.Product(knowledge.ClaudeSquadProduct)
		orch := orchestrator(knowledge.ClaudeSquadProduct, c.Dir)
		picks := map[string]pick{}
		match := func(program string) (knowledge.OrchestratorLaunch, bool) {
			fields := strings.Fields(program)
			for _, l := range kc.Launches {
				switch {
				case l.ProgramMatch == "exact" && program == l.ID:
					return l, true
				case l.ProgramMatch == "prefix" && strings.HasPrefix(program, l.ID):
					return l, true
				case l.ProgramMatch == "" && len(fields) > 0 && fields[0] == l.ID:
					return l, true
				}
			}
			return knowledge.OrchestratorLaunch{}, false
		}
		programs := append([]string{}, c.Profiles...)
		if c.DefaultProgram != "" || c.ConfigStatus == platform.MCPSourceMissing {
			def := c.DefaultProgram
			if def == "" {
				def = "claude" // config/config.go:17 at v1.0.20
			}
			programs = append([]string{def}, programs...)
		}
		for _, prog := range programs {
			if l, ok := match(prog); ok {
				note(picks, l, c.AutoYes && l.Skips("auto_yes"), c.ConfigPath)
			}
		}
		for _, in := range c.Instances {
			if l, ok := match(in.Program); ok {
				note(picks, l, in.AutoYes && l.Skips("auto_yes"), c.StatePath)
			}
		}
		observed(orch, picks)
	}

	if o := scan.Orca; o != nil {
		ko, _ := kb.Product(knowledge.OrcaProduct)
		dir := o.UserData
		if dir == "" {
			dir = o.Home
		}
		orch := orchestrator(knowledge.OrcaProduct, dir)
		for _, l := range ko.Launches {
			for _, n := range clients[l.Agent] {
				b.edge(Edge{
					ID: edgeID(EdgeLaunches, orch, n.ID), Type: EdgeLaunches, From: orch, To: n.ID,
					Source: ko.Ref(l.ID), Certainty: CertaintyInferred, ApprovalOverride: l.DefaultOverride,
					EnvPassthrough: l.EnvPassthrough, Basis: "product_default",
					DerivedFrom: []string{orch, n.ID}, Assumptions: baseAssumptions(AssumeOrchestratorSettingsNotRead),
				})
			}
		}
		ids := map[string]bool{}
		for _, n := range f.Nodes {
			ids[n.ID] = true
		}
		for _, h := range o.Hooks {
			res := configResourceID(h.ConfigPath)
			if !ids[res] {
				continue
			}
			b.edge(Edge{
				ID: edgeID(EdgeWritesConfigOf, orch, res), Type: EdgeWritesConfigOf, From: orch, To: res,
				Source: h.ConfigPath + "#/hooks", Certainty: CertaintyInferred,
				DerivedFrom: []string{orch, res}, Assumptions: baseAssumptions(),
			})
		}
	}
	return Merge(f, b.fragment())
}
