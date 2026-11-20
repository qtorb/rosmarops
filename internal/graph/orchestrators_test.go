package graph

import (
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

func orchFragment() Fragment {
	settings := "/home/u/.claude/settings.json"
	seen := "2026-11-20T09:00:00Z"
	return Fragment{Nodes: []Node{
		{ID: "agent_client_claude", Kind: KindAgent, Type: AgentAIClient, Platform: "claude-code", Name: "Claude Code", ConfigPath: "/home/u/.claude", DiscoveredAt: seen},
		{ID: "agent_client_codex", Kind: KindAgent, Type: AgentAIClient, Platform: "codex", Name: "Codex", ConfigPath: "/home/u/.codex", DiscoveredAt: seen},
		{ID: "agent_client_gemini", Kind: KindAgent, Type: AgentAIClient, Platform: "gemini-cli", Name: "Gemini CLI", ConfigPath: "/home/u/.gemini", DiscoveredAt: seen},
		{ID: configResourceID(settings), Kind: KindResource, Type: ResourceAgentConfig, Path: settings, Configures: "agent_client_claude", DiscoveredAt: seen},
	}}
}

func launchesOf(f Fragment, orch string) map[string]Edge {
	out := map[string]Edge{}
	for _, e := range f.Edges {
		if e.Type == EdgeLaunches && e.From == orch {
			out[e.To] = e
		}
	}
	return out
}

func TestAddOrchestrators(t *testing.T) {
	kb, err := knowledge.LoadOrchestrators()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
	scan := platform.OrchestratorScan{
		Paseo: &platform.PaseoScan{
			Home: "/home/u/.paseo", ConfigPath: "/home/u/.paseo/config.json", ConfigStatus: platform.MCPSourceRead,
			APIKeys: []string{"/features/dictation/stt/apiKey"}, ConfigMode: "0600", ConfigOwner: OwnerCurrentUser,
			Agents: []platform.PaseoAgent{
				{Path: "/home/u/.paseo/agents/p/a1.json", Provider: "claude", Mode: "default", Status: platform.MCPSourceRead},
				{Path: "/home/u/.paseo/agents/p/a2.json", Provider: "claude", Mode: "bypassPermissions", Status: platform.MCPSourceRead},
				{Path: "/home/u/.paseo/agents/p/a3.json", Provider: "codex", Mode: "full-access", Status: platform.MCPSourceRead, Archived: true},
			},
			Schedules: []platform.PaseoSchedule{
				{Path: "/home/u/.paseo/schedules/s1.json", ScheduleStatus: "active", Provider: "codex", Mode: "auto", Status: platform.MCPSourceRead},
				{Path: "/home/u/.paseo/schedules/s2.json", ScheduleStatus: "paused", Provider: "codex", Mode: "full-access", Status: platform.MCPSourceRead},
			},
		},
		ClaudeSquad: &platform.ClaudeSquadScan{
			Dir: "/home/u/.claude-squad", ConfigPath: "/home/u/.claude-squad/config.json", ConfigStatus: platform.MCPSourceRead,
			DefaultProgram: "claude", Profiles: []string{"codex", "gemini --model x"}, StatePath: "/home/u/.claude-squad/state.json",
			Instances: []platform.ClaudeSquadInstance{{Program: "claude", AutoYes: true}, {Program: "codex", AutoYes: true}},
		},
		Orca: &platform.OrcaScan{
			UserData: "/home/u/.config/Orca",
			Hooks:    []platform.OrcaHook{{Agent: "claude-code", ConfigPath: "/home/u/.claude/settings.json"}, {Agent: "cursor", ConfigPath: "/home/u/.cursor/hooks.json"}},
		},
	}
	f := AddOrchestrators(orchFragment(), scan, kb, []byte("salt"), now)
	if _, err := Encode(Build(f, Provenance{EngineVersion: "t", RulesVersion: RulesVersion, KnowledgeVersion: "k", HostID: "host_0123456789abcdef"}, now)); err != nil {
		t.Fatalf("the graph is not a valid graph.v2: %v", err)
	}

	paseo := launchesOf(f, OrchestratorAgentID(knowledge.PaseoProduct, "/home/u/.paseo"))
	if e := paseo["agent_client_claude"]; e.ApprovalOverride != ApprovalSkipsAll || e.Certainty != CertaintyObserved || e.Basis != "configured" || e.Source != "/home/u/.paseo/agents/p/a2.json" {
		t.Errorf("Paseo → Claude Code: %+v", e)
	}
	if e := paseo["agent_client_codex"]; e.ApprovalOverride != ApprovalKeepsAgentSettings || e.Source != "/home/u/.paseo/schedules/s1.json" {
		t.Errorf("Paseo → Codex (archived agent and paused schedule do not count): %+v", e)
	}
	wakes, creds := 0, 0
	for _, e := range f.Edges {
		if e.Type == EdgeWakes && e.Certainty == CertaintyObserved {
			wakes++
		}
	}
	for _, n := range f.Nodes {
		if n.Kind == KindCredential && n.IdentifiedBy == IdentifiedByPath && n.Service == "openai" && n.Fingerprint == "" {
			creds++
		}
	}
	if wakes != 1 || creds != 1 {
		t.Errorf("Paseo: %d wakes (one active schedule), %d credentials known by path", wakes, creds)
	}

	squad := launchesOf(f, OrchestratorAgentID(knowledge.ClaudeSquadProduct, "/home/u/.claude-squad"))
	if e := squad["agent_client_claude"]; e.ApprovalOverride != ApprovalSkipsAll || e.Source != "/home/u/.claude-squad/state.json" {
		t.Errorf("Claude Squad → Claude Code with an auto_yes session: %+v", e)
	}
	if e := squad["agent_client_codex"]; e.ApprovalOverride != ApprovalKeepsAgentSettings {
		t.Errorf("Claude Squad's auto_yes answers no Codex prompt: %+v", e)
	}
	if e := squad["agent_client_gemini"]; e.ApprovalOverride != ApprovalKeepsAgentSettings || e.Source != "/home/u/.claude-squad/config.json" {
		t.Errorf("Claude Squad → Gemini without auto_yes: %+v", e)
	}

	orcaID := OrchestratorAgentID(knowledge.OrcaProduct, "/home/u/.config/Orca")
	orca := launchesOf(f, orcaID)
	if e := orca["agent_client_claude"]; e.ApprovalOverride != ApprovalSkipsAll || e.Certainty != CertaintyInferred || e.Basis != "product_default" ||
		e.Source != "kb:orca/claude@"+knowledge.OrchestratorsVersion || !contains(e.Assumptions, AssumeOrchestratorSettingsNotRead) {
		t.Errorf("Orca → Claude Code: %+v", e)
	}
	writes := 0
	for _, e := range f.Edges {
		if e.Type == EdgeWritesConfigOf && e.From == orcaID {
			writes++
		}
	}
	if writes != 1 {
		t.Errorf("Orca writes_config_of %d, want Claude Code's settings only (Cursor's resource is not in the graph)", writes)
	}
}
