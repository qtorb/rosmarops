package rules

import (
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// The orchestrator rules (BV-264, CTO-75 §6), on the Paperclip instances of
// the graph and the scan they were built from:
//   - orchestrator_skips_approvals: a launches edge says Paperclip can start an
//     installed agent skipping its approvals (red), or may do so depending on
//     how the agent was created (yellow, Codex);
//   - orchestrator_vulnerable_version: the version in cli/install.json, or one
//     in npm's cache, is older than the one that fixes its known serious flaws
//     (red);
//   - orchestrator_network_listen: the instance listens beyond this computer
//     (red with open sign-up in authenticated mode, yellow otherwise);
//   - orchestrator_open_signup: in authenticated mode anyone who reaches it can
//     create an account (red when it also listens beyond this computer,
//     yellow otherwise). In local_trusted there are no accounts.
//
// Listen and sign-up come from config.json and, when there is one, a service
// unit's PAPERCLIP_* variables. Paperclip started by hand with other variables
// leaves no trace at rest: without a unit those findings carry
// launch_flags_not_readable.
const (
	RuleOrchestratorSkipsApprovals    = "orchestrator_skips_approvals"
	RuleOrchestratorVulnerableVersion = "orchestrator_vulnerable_version"
	RuleOrchestratorNetworkListen     = "orchestrator_network_listen"
	RuleOrchestratorOpenSignup        = "orchestrator_open_signup"
)

// AssumeOrchestratorAgentConfigured is the graph's assumption of a launches
// edge: the orchestrator has the agent configured, which is only in its
// database. A premise: one is enough for confidence medium.
const AssumeOrchestratorAgentConfigured = graph.AssumeOrchestratorAgentConfigured

// Where an orchestrator_vulnerable_version finding's version was found.
const (
	VersionInstalled = "installed"
	VersionNPMCache  = "npm_cache"
)

// orchestratorFindings are the four rules' findings, sorted by id.
func orchestratorFindings(in Input) []Finding {
	if len(in.Paperclip.Instances) == 0 {
		return nil
	}
	kb, err := knowledge.LoadPaperclip()
	if err != nil {
		return nil
	}
	nodes := map[string]graph.Node{}
	for _, n := range in.Graph.Nodes {
		nodes[n.ID] = n
	}
	var out []Finding
	for _, e := range in.Graph.Edges {
		if e.Type != graph.EdgeLaunches {
			continue
		}
		orch, agent := nodes[e.From], nodes[e.To]
		sev := ""
		switch e.ApprovalOverride {
		case graph.ApprovalSkipsAll:
			sev = SeverityRed
		case graph.ApprovalOverrideUnknown:
			sev = SeverityYellow
		default:
			continue
		}
		f := Finding{
			ID: findingID(RuleOrchestratorSkipsApprovals, orch.ID, agent.ID), Rule: RuleOrchestratorSkipsApprovals, RuleVersion: RuleVersion,
			Severity: sev,
			Params:   Params{Agent: orch.Name, TargetAgent: agent.Name, Count: 1},
			Evidence: Evidence{
				Nodes: []string{orch.ID, agent.ID}, Edges: []string{e.ID}, Locations: []string{orch.ConfigPath},
				Assumptions: append([]string{}, e.Assumptions...),
			},
			Group: group(in, []string{orch.ConfigPath}), Actions: []Action{},
		}
		f.Confidence = confidence(f.Evidence.Assumptions)
		out = append(out, f)
	}
	for _, inst := range in.Paperclip.Instances {
		orch, ok := nodes[graph.PaperclipAgentID(inst.Dir)]
		if !ok {
			continue
		}
		out = append(out, exposureFindings(in, orch, inst)...)
	}
	// The version is the computer's Paperclip, not an instance's: it goes on
	// the first instance.
	if orch, ok := firstPaperclip(in, nodes); ok {
		fixed := kb.FixedVersion.Version
		if v := in.Paperclip.Installed; v != "" && knowledge.VersionBefore(v, fixed) {
			out = append(out, versionFinding(in, orch, v, VersionInstalled, in.Paperclip.InstallPath))
		}
		for _, p := range in.Paperclip.NPMCache {
			if knowledge.VersionBefore(p.Version, fixed) {
				out = append(out, versionFinding(in, orch, p.Version, VersionNPMCache, p.Path))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func firstPaperclip(in Input, nodes map[string]graph.Node) (graph.Node, bool) {
	ids := make([]string, 0, len(in.Paperclip.Instances))
	for _, inst := range in.Paperclip.Instances {
		ids = append(ids, graph.PaperclipAgentID(inst.Dir))
	}
	sort.Strings(ids)
	for _, id := range ids {
		if n, ok := nodes[id]; ok {
			return n, true
		}
	}
	return graph.Node{}, false
}

func versionFinding(in Input, orch graph.Node, version, where, path string) Finding {
	return Finding{
		ID: findingID(RuleOrchestratorVulnerableVersion, orch.ID, path, version), Rule: RuleOrchestratorVulnerableVersion, RuleVersion: RuleVersion,
		Severity: SeverityRed, Confidence: ConfidenceHigh,
		Params:   Params{Agent: orch.Name, Version: version, Scope: where, Count: 1},
		Evidence: Evidence{Nodes: []string{orch.ID}, Edges: []string{}, Locations: []string{path}, Assumptions: []string{}},
		Group:    group(in, []string{path}), Actions: []Action{},
	}
}

// exposureFindings are an instance's network_listen and open_signup.
func exposureFindings(in Input, orch graph.Node, inst platform.PaperclipInstance) []Finding {
	if inst.ConfigStatus != platform.MCPSourceRead {
		return nil
	}
	svc := in.Paperclip.Service
	mode, signUpOff := graph.PaperclipEffective(inst, svc)
	network := paperclipListensOnNetwork(inst, svc)
	openSignup := mode == "authenticated" && !signUpOff
	locs := []string{inst.ConfigPath}
	assume := []string{AssumeLaunchFlagsNotReadable}
	if svc != nil {
		locs = append(locs, svc.Path)
		assume = []string{}
	}
	mk := func(rule, sev string) Finding {
		return Finding{
			ID: findingID(rule, orch.ID), Rule: rule, RuleVersion: RuleVersion, Severity: sev,
			Confidence: confidence(assume),
			Params:     Params{Agent: orch.Name, Count: 1},
			Evidence:   Evidence{Nodes: []string{orch.ID}, Edges: []string{}, Locations: append([]string{}, locs...), Assumptions: append([]string{}, assume...)},
			Group:      group(in, locs), Actions: []Action{},
		}
	}
	var out []Finding
	if network {
		sev := SeverityYellow
		if openSignup {
			sev = SeverityRed
		}
		out = append(out, mk(RuleOrchestratorNetworkListen, sev))
	}
	if openSignup {
		sev := SeverityYellow
		if network {
			sev = SeverityRed
		}
		out = append(out, mk(RuleOrchestratorOpenSignup, sev))
	}
	return out
}

// paperclipListensOnNetwork reports whether an instance listens beyond this
// computer (CTO-75 §6): bind lan, tailnet or custom, or a host other than a
// loopback address. A service unit's PAPERCLIP_BIND wins over config.json.
func paperclipListensOnNetwork(inst platform.PaperclipInstance, svc *platform.PaperclipService) bool {
	bind := inst.Bind
	if svc != nil && svc.Env["PAPERCLIP_BIND"] != "" {
		bind = svc.Env["PAPERCLIP_BIND"]
	}
	switch strings.ToLower(bind) {
	case "lan", "tailnet", "custom":
		return true
	case "loopback":
		return false
	}
	return !loopbackHost(inst.Host)
}

// loopbackHost reports a host that only this computer reaches: empty (the
// default, 127.0.0.1), localhost, 127.0.0.0/8 or ::1. Parsed by hand: no
// package but internal/spacenet may import net (invariant 1).
func loopbackHost(host string) bool {
	h := strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]"))
	switch h {
	case "", "localhost", "::1", "0:0:0:0:0:0:0:1":
		return true
	}
	parts := strings.Split(h, ".")
	if len(parts) != 4 || parts[0] != "127" {
		return false
	}
	for _, p := range parts {
		if p == "" || len(p) > 3 || strings.Trim(p, "0123456789") != "" {
			return false
		}
		n := 0
		for _, c := range p {
			n = n*10 + int(c-'0')
		}
		if n > 255 {
			return false
		}
	}
	return true
}
