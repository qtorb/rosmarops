package rules

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

const pcDir = "/home/u/.paperclip/instances/default"

func boolp(b bool) *bool { return &b }

// orchInput is a graph with one Paperclip instance and the agents it can
// launch, with the given approval overrides, and the scan the rules read.
func orchInput(in platform.PaperclipInstance, svc *platform.PaperclipService, installed string, npm []platform.PaperclipPackage, launches map[string]string) Input {
	orch := graph.PaperclipAgentID(pcDir)
	g := graph.Graph{Nodes: []graph.Node{{ID: orch, Kind: graph.KindAgent, Type: graph.AgentOrchestrator, Name: "Paperclip", Platform: "paperclip", ConfigPath: pcDir}}}
	for name, override := range launches {
		id := "agent_client_" + name
		g.Nodes = append(g.Nodes, graph.Node{ID: id, Kind: graph.KindAgent, Type: graph.AgentAIClient, Name: name})
		g.Edges = append(g.Edges, graph.Edge{
			ID: "edge_launches_" + name, Type: graph.EdgeLaunches, From: orch, To: id,
			ApprovalOverride: override, Certainty: graph.CertaintyInferred, Assumptions: []string{graph.AssumeOrchestratorAgentConfigured},
		})
	}
	in.Dir, in.ConfigPath, in.ConfigStatus = pcDir, pcDir+"/config.json", platform.MCPSourceRead
	return Input{Graph: g, Paperclip: platform.PaperclipScan{
		Instances: []platform.PaperclipInstance{in}, Service: svc,
		Installed: installed, InstallPath: "/home/u/.paperclip/cli/install.json", NPMCache: npm,
	}}
}

func bySeverity(fs []Finding) map[string]string {
	out := map[string]string{}
	for _, f := range fs {
		key := f.Rule
		if f.Params.TargetAgent != "" {
			key += "/" + f.Params.TargetAgent
		}
		if f.Params.Scope != "" {
			key += "/" + f.Params.Scope
		}
		out[key] = f.Severity
	}
	return out
}

func TestOrchestratorSkipsApprovals(t *testing.T) {
	got := bySeverity(orchestratorFindings(orchInput(platform.PaperclipInstance{}, nil, "", nil,
		map[string]string{"Claude Code": graph.ApprovalSkipsAll, "Codex": graph.ApprovalOverrideUnknown, "Other": graph.ApprovalKeepsAgentSettings})))
	if got["orchestrator_skips_approvals/Claude Code"] != SeverityRed || got["orchestrator_skips_approvals/Codex"] != SeverityYellow {
		t.Errorf("findings %v", got)
	}
	if _, ok := got["orchestrator_skips_approvals/Other"]; ok {
		t.Error("an agent whose settings are kept is not a finding")
	}
	for _, f := range orchestratorFindings(orchInput(platform.PaperclipInstance{}, nil, "", nil, map[string]string{"Claude Code": graph.ApprovalSkipsAll})) {
		if f.Confidence != ConfidenceMedium {
			t.Errorf("%s confidence %s: which agents Paperclip has configured is an assumption", f.Rule, f.Confidence)
		}
	}
}

func TestOrchestratorExposure(t *testing.T) {
	cases := []struct {
		name string
		in   platform.PaperclipInstance
		svc  *platform.PaperclipService
		want map[string]string
	}{
		{"local trusted loopback", platform.PaperclipInstance{DeploymentMode: "local_trusted", Bind: "loopback"}, nil, map[string]string{}},
		{"defaults", platform.PaperclipInstance{}, nil, map[string]string{}},
		{
			"local trusted on the LAN",
			platform.PaperclipInstance{DeploymentMode: "local_trusted", Bind: "lan"},
			nil,
			map[string]string{"orchestrator_network_listen": SeverityYellow},
		},
		{
			"authenticated, LAN, sign-up open",
			platform.PaperclipInstance{DeploymentMode: "authenticated", Bind: "lan", SignUpDisabled: boolp(false)},
			nil,
			map[string]string{"orchestrator_network_listen": SeverityRed, "orchestrator_open_signup": SeverityRed},
		},
		{
			"authenticated, host 0.0.0.0, sign-up closed",
			platform.PaperclipInstance{DeploymentMode: "authenticated", Host: "0.0.0.0", SignUpDisabled: boolp(true)},
			nil,
			map[string]string{"orchestrator_network_listen": SeverityYellow},
		},
		{
			"authenticated, loopback, sign-up open",
			platform.PaperclipInstance{DeploymentMode: "authenticated", Host: "127.0.0.1"},
			nil,
			map[string]string{"orchestrator_open_signup": SeverityYellow},
		},
		{
			"the service unit closes sign-up",
			platform.PaperclipInstance{DeploymentMode: "authenticated", Bind: "lan"},
			&platform.PaperclipService{Path: "/u/paperclip.service", Env: map[string]string{"PAPERCLIP_AUTH_DISABLE_SIGN_UP": "true"}},
			map[string]string{"orchestrator_network_listen": SeverityYellow},
		},
		{
			"the service unit binds to the LAN",
			platform.PaperclipInstance{DeploymentMode: "local_trusted", Bind: "loopback"},
			&platform.PaperclipService{Path: "/u/paperclip.service", Env: map[string]string{"PAPERCLIP_BIND": "lan"}},
			map[string]string{"orchestrator_network_listen": SeverityYellow},
		},
	}
	for _, c := range cases {
		fs := orchestratorFindings(orchInput(c.in, c.svc, "", nil, nil))
		got := bySeverity(fs)
		if len(got) != len(c.want) {
			t.Errorf("%s: findings %v, want %v", c.name, got, c.want)
			continue
		}
		for r, sev := range c.want {
			if got[r] != sev {
				t.Errorf("%s: %s = %q, want %q", c.name, r, got[r], sev)
			}
		}
		for _, f := range fs {
			assumed := len(f.Evidence.Assumptions) == 1 && f.Evidence.Assumptions[0] == AssumeLaunchFlagsNotReadable
			if (c.svc == nil) != assumed {
				t.Errorf("%s: %s assumptions %v (launch_flags_not_readable only without a service unit)", c.name, f.Rule, f.Evidence.Assumptions)
			}
		}
	}
}

func TestOrchestratorVulnerableVersion(t *testing.T) {
	npm := []platform.PaperclipPackage{{Version: "2026.410.0", Path: "/home/u/.npm/_npx/a/node_modules/paperclipai/package.json"}, {Version: "2026.1117.0", Path: "/home/u/.npm/_npx/b/node_modules/paperclipai/package.json"}}
	got := bySeverity(orchestratorFindings(orchInput(platform.PaperclipInstance{}, nil, "2026.415.0", npm, nil)))
	if got["orchestrator_vulnerable_version/installed"] != SeverityRed || got["orchestrator_vulnerable_version/npm_cache"] != SeverityRed || len(got) != 2 {
		t.Errorf("findings %v", got)
	}
	if got := orchestratorFindings(orchInput(platform.PaperclipInstance{}, nil, "2026.1117.0", nil, nil)); len(got) != 0 {
		t.Errorf("2026.1117.0 is fixed: %+v", got)
	}
}

func TestLoopbackHost(t *testing.T) {
	for host, want := range map[string]bool{
		"": true, "localhost": true, "127.0.0.1": true, "127.1.2.3": true, "::1": true, "[::1]": true,
		"0.0.0.0": false, "192.168.1.10": false, "127.0.0": false, "127.0.0.256": false, "paperclip.local": false,
	} {
		if got := loopbackHost(host); got != want {
			t.Errorf("loopbackHost(%q) = %v, want %v", host, got, want)
		}
	}
}

// BV-268: the approvals rule reads every orchestrator's launches edges, with
// or without a Paperclip. An observed edge is high confidence and its
// finding points at the file that set the mode; Orca's factory default is a
// premise.
func TestOrchestratorSkipsApprovalsWithoutPaperclip(t *testing.T) {
	paseo := graph.OrchestratorAgentID("paseo", "/home/u/.paseo")
	orca := graph.OrchestratorAgentID("orca", "/home/u/.config/Orca")
	g := graph.Graph{
		Nodes: []graph.Node{
			{ID: paseo, Kind: graph.KindAgent, Type: graph.AgentOrchestrator, Name: "Paseo", Platform: "paseo", ConfigPath: "/home/u/.paseo"},
			{ID: orca, Kind: graph.KindAgent, Type: graph.AgentOrchestrator, Name: "Orca", Platform: "orca", ConfigPath: "/home/u/.config/Orca"},
			{ID: "agent_client_claude", Kind: graph.KindAgent, Type: graph.AgentAIClient, Name: "Claude Code"},
		},
		Edges: []graph.Edge{
			{
				ID: "edge_launches_paseo", Type: graph.EdgeLaunches, From: paseo, To: "agent_client_claude", Source: "/home/u/.paseo/agents/p/a.json",
				Certainty: graph.CertaintyObserved, ApprovalOverride: graph.ApprovalSkipsAll, Basis: "configured",
			},
			{
				ID: "edge_launches_orca", Type: graph.EdgeLaunches, From: orca, To: "agent_client_claude", Source: "kb:orca/claude@2026-11-20",
				Certainty: graph.CertaintyInferred, ApprovalOverride: graph.ApprovalSkipsAll, Basis: "product_default",
				Assumptions: []string{AssumeOrchestratorSettingsNotRead},
			},
		},
	}
	fs := orchestratorFindings(Input{Graph: g})
	if len(fs) != 2 {
		t.Fatalf("findings %+v", fs)
	}
	for _, f := range fs {
		if f.Rule != RuleOrchestratorSkipsApprovals || f.Severity != SeverityRed {
			t.Errorf("%+v", f)
		}
		switch f.Params.Agent {
		case "Paseo":
			if f.Confidence != ConfidenceHigh || f.Evidence.Locations[0] != "/home/u/.paseo/agents/p/a.json" {
				t.Errorf("Paseo: confidence %s, locations %v", f.Confidence, f.Evidence.Locations)
			}
		case "Orca":
			if f.Confidence != ConfidenceMedium || f.Evidence.Locations[0] != "/home/u/.config/Orca" {
				t.Errorf("Orca: confidence %s, locations %v", f.Confidence, f.Evidence.Locations)
			}
		}
	}
}

// BV-278: the orchestrator findings come after the rest, and the person's
// lists apply to them too: a dismissed one leaves the scored set, an accepted
// one carries its date.
func TestOrchestratorFindingsFollowThePersonsLists(t *testing.T) {
	in := orchInput(platform.PaperclipInstance{}, nil, "", nil,
		map[string]string{"Claude Code": graph.ApprovalSkipsAll, "Codex": graph.ApprovalOverrideUnknown})
	ids := map[string]string{}
	for _, f := range Evaluate(in).Findings {
		if f.Rule == RuleOrchestratorSkipsApprovals {
			ids[f.Params.TargetAgent] = f.ID
		}
	}
	if len(ids) != 2 {
		t.Fatalf("findings %v", ids)
	}
	in.Dismissed = []config.AllowEntryDismissed{{ID: ids["Claude Code"], Date: "2026-11-23"}}
	in.Accepted = []config.AllowEntryAccepted{{ID: ids["Codex"], Date: "2026-11-23"}}
	res := Evaluate(in)
	if len(res.Dismissed) != 1 || res.Dismissed[0].ID != ids["Claude Code"] {
		t.Errorf("dismissed %v", res.Dismissed)
	}
	for _, f := range res.Findings {
		switch f.ID {
		case ids["Claude Code"]:
			t.Error("a dismissed finding still counts")
		case ids["Codex"]:
			if f.Accepted == nil || f.Accepted.Date != "2026-11-23" {
				t.Errorf("accepted finding without its date: %+v", f.Accepted)
			}
		}
	}
}
