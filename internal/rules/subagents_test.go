package rules

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

func subagentInput(subs ...platform.ClaudeSubagent) Input {
	g := graph.Graph{Nodes: []graph.Node{
		{ID: "agent_client_other", Kind: graph.KindAgent, Type: graph.AgentAIClient, Platform: "claude-code", Name: "Claude Code", ConfigPath: "/srv/other/.claude"},
		{ID: "agent_client_claude", Kind: graph.KindAgent, Type: graph.AgentAIClient, Platform: "claude-code", Name: "Claude Code", ConfigPath: "/home/u/.claude"},
	}}
	return Input{Graph: g, Orchestrators: platform.OrchestratorScan{Subagents: subs}}
}

func TestSubagentFindings(t *testing.T) {
	user := platform.ClaudeSubagent{
		Path: "/home/u/.claude/agents/deploy.md", Dir: "/home/u/.claude/agents", Scope: platform.SubagentUser, Status: platform.MCPSourceRead,
		Name: "deployer", PermissionMode: "bypassPermissions", HookCommands: 1, InlineMCPServers: []string{"gh"},
	}
	project := platform.ClaudeSubagent{
		Path: "/r/.claude/agents/review.md", Dir: "/r/.claude/agents", Repo: "/r", Scope: platform.SubagentProject, Status: platform.MCPSourceRead,
		ToolsDeclared: true, PermissionMode: "acceptEdits",
	}
	quiet := platform.ClaudeSubagent{Path: "/r/.claude/agents/quiet.md", Scope: platform.SubagentProject, Status: platform.MCPSourceRead, ToolsDeclared: true, PermissionMode: "plan"}
	failed := platform.ClaudeSubagent{Path: "/r/.claude/agents/bad.md", Scope: platform.SubagentProject, Status: platform.MCPSourceFailed}
	fs := subagentFindings(subagentInput(user, project, quiet, failed))
	got := map[string]Finding{}
	for _, f := range fs {
		got[f.Rule+"/"+f.Params.TargetAgent] = f
	}
	if len(fs) != 5 {
		t.Fatalf("findings %v", got)
	}
	for key, sev := range map[string]string{
		RuleSubagentAllTools + "/deployer":       SeverityYellow,
		RuleSubagentPermissiveMode + "/deployer": SeverityRed,
		RuleSubagentHookCommand + "/deployer":    SeverityYellow,
		RuleSubagentInlineMCP + "/deployer":      SeverityYellow,
		RuleSubagentPermissiveMode + "/review":   SeverityYellow,
	} {
		f, ok := got[key]
		if !ok || f.Severity != sev {
			t.Errorf("%s: %+v", key, f)
			continue
		}
		if f.Evidence.Nodes[0] != "agent_client_claude" || f.Params.Agent != "Claude Code" || f.Confidence != ConfidenceHigh {
			t.Errorf("%s: on %v, confidence %s (the Claude Code of the user's folder)", key, f.Evidence.Nodes, f.Confidence)
		}
	}
	if f := got[RuleSubagentPermissiveMode+"/deployer"]; f.Params.Scope != "bypassPermissions" || f.Params.ResourceType != SubagentUserFolder {
		t.Errorf("deployer params %+v", f.Params)
	}
	if f := got[RuleSubagentPermissiveMode+"/review"]; f.Params.Scope != "acceptEdits" || f.Params.ResourceType != "" || f.Evidence.Locations[0] != "/r/.claude/agents/review.md" {
		t.Errorf("review params %+v, locations %v", f.Params, f.Evidence.Locations)
	}
	if got := subagentFindings(Input{Orchestrators: platform.OrchestratorScan{Subagents: []platform.ClaudeSubagent{user}}}); len(got) != 0 {
		t.Errorf("no Claude Code in the graph, no finding: %+v", got)
	}
	if dontAsk := subagentFindings(subagentInput(platform.ClaudeSubagent{Path: "/r/.claude/agents/x.md", Status: platform.MCPSourceRead, ToolsDeclared: true, PermissionMode: "dontAsk"})); len(dontAsk) != 1 || dontAsk[0].Severity != SeverityRed {
		t.Errorf("dontAsk: %+v", dontAsk)
	}
}

func TestSubagentName(t *testing.T) {
	if got := SubagentName(platform.ClaudeSubagent{Path: "/a/agents/code-reviewer.md"}); got != "code-reviewer" {
		t.Errorf("no name: %q, want the file name without .md", got)
	}
	if got := SubagentName(platform.ClaudeSubagent{Path: "/a/x.md", Name: "  my\tagent\u200b\n x "}); got != "my agent x" {
		t.Errorf("cleaned %q", got)
	}
	long := SubagentName(platform.ClaudeSubagent{Path: "/a/x.md", Name: strings.Repeat("é", 80)})
	if utf8.RuneCountInString(long) != 60 || !strings.HasSuffix(long, "…") {
		t.Errorf("cut %q (%d)", long, utf8.RuneCountInString(long))
	}
}

func TestPaseoListen(t *testing.T) {
	for listen, want := range map[string]bool{
		"": false, "127.0.0.1:6767": false, "localhost:6767": false, "[::1]:6767": false, "6767": false, ":6767": false,
		"/tmp/paseo.sock": false, "unix:///tmp/p.sock": false, `\\.\pipe\paseo`: false, "pipe://paseo": false,
		"0.0.0.0:6767": true, "192.168.1.5:6767": true, "[::]:6767": true, "paseo.local:6767": true,
	} {
		if got := paseoListensOnNetwork(listen); got != want {
			t.Errorf("paseoListensOnNetwork(%q) = %v, want %v", listen, got, want)
		}
	}
	home := "/home/u/.paseo"
	orch := graph.OrchestratorAgentID("paseo", home)
	in := Input{
		Graph: graph.Graph{Nodes: []graph.Node{{ID: orch, Kind: graph.KindAgent, Type: graph.AgentOrchestrator, Name: "Paseo", Platform: "paseo", ConfigPath: home}}},
		Orchestrators: platform.OrchestratorScan{Paseo: &platform.PaseoScan{
			Home: home, ConfigPath: home + "/config.json", ConfigStatus: platform.MCPSourceRead, Listen: "0.0.0.0:6767",
		}},
	}
	fs := orchestratorFindings(in)
	if len(fs) != 1 || fs[0].Rule != RuleOrchestratorNetworkListen || fs[0].Severity != SeverityYellow || fs[0].Params.Scope != PaseoListen ||
		fs[0].Evidence.Assumptions[0] != AssumeLaunchFlagsNotReadable || fs[0].Evidence.Locations[0] != home+"/config.json" {
		t.Errorf("Paseo on the LAN: %+v", fs)
	}
	in.Orchestrators.Paseo.Listen = "127.0.0.1:6767"
	if fs := orchestratorFindings(in); len(fs) != 0 {
		t.Errorf("Paseo on loopback: %+v", fs)
	}
}
