package rules

import (
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-268 (CTO-76 §5): Claude Code's subagents. Each rule is one signed
// sentence on the Claude Code agent, in the agent_config group. Its params
// reuse the report's existing fields (decision of 2026-11-20, option a):
// target_agent is the subagent's name, scope its permissionMode, and
// resource_type "user" when the file is in the user's folder, where it runs
// in every project without the folder-trust question.
const (
	RuleSubagentAllTools       = "subagent_all_tools"
	RuleSubagentPermissiveMode = "subagent_permissive_mode"
	RuleSubagentHookCommand    = "subagent_hook_command"
	RuleSubagentInlineMCP      = "subagent_inline_mcp"
)

// SubagentUserFolder is Params.ResourceType of a subagent in the user's
// folder.
const SubagentUserFolder = "user"

// subagentNameMax is the longest subagent name shown, «…» included, as
// session-check shows a configuration name (BV-226 §1).
const subagentNameMax = 60

// SubagentName is the subagent as shown: its header's name, or its file name
// without .md; control and format characters dropped, spaces collapsed, and
// cut to 60 characters with «…».
func SubagentName(s platform.ClaudeSubagent) string {
	name := s.Name
	if strings.TrimSpace(name) == "" {
		name = strings.TrimSuffix(filepath.Base(s.Path), filepath.Ext(s.Path))
	}
	var b strings.Builder
	space := false
	for _, r := range name {
		if r == utf8.RuneError || unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || unicode.IsSpace(r) {
			if !space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = true
			continue
		}
		space = false
		b.WriteRune(r)
	}
	t := strings.TrimSpace(b.String())
	if utf8.RuneCountInString(t) > subagentNameMax {
		t = strings.TrimSpace(string([]rune(t)[:subagentNameMax-1])) + "…"
	}
	return t
}

// claudeCodeAgent is the Claude Code ai_client the subagents belong to: the
// one whose folder holds the user's agents/ folder, else the first.
func claudeCodeAgent(g graph.Graph, userDir string) (graph.Node, bool) {
	var first graph.Node
	found := false
	for _, n := range g.Nodes {
		if n.Kind != graph.KindAgent || n.Type != graph.AgentAIClient || n.Platform != "claude-code" {
			continue
		}
		if userDir != "" && filepath.Clean(n.ConfigPath) == filepath.Clean(userDir) {
			return n, true
		}
		if !found {
			first, found = n, true
		}
	}
	return first, found
}

// subagentFindings are the four rules over the subagents read.
func subagentFindings(in Input) []Finding {
	subs := in.Orchestrators.Subagents
	if len(subs) == 0 {
		return nil
	}
	userDir := ""
	for _, s := range subs {
		if s.Scope == platform.SubagentUser {
			userDir = filepath.Dir(s.Dir)
			break
		}
	}
	agent, ok := claudeCodeAgent(in.Graph, userDir)
	if !ok {
		return nil
	}
	var out []Finding
	for _, s := range subs {
		if s.Status != platform.MCPSourceRead {
			continue
		}
		mk := func(rule, sev, mode string) Finding {
			p := Params{Agent: agent.Name, TargetAgent: SubagentName(s), Scope: mode, Count: 1}
			if s.Scope == platform.SubagentUser {
				p.ResourceType = SubagentUserFolder
			}
			f := Finding{
				ID: findingID(rule, agent.ID, s.Path), Rule: rule, RuleVersion: RuleVersion, Severity: sev,
				Params: p,
				Evidence: Evidence{
					Nodes: []string{agent.ID}, Edges: []string{}, Locations: []string{s.Path}, Assumptions: []string{},
				},
				Group: group(in, []string{s.Path}), Actions: []Action{},
			}
			f.Confidence = confidence(f.Evidence.Assumptions)
			return f
		}
		if !s.ToolsDeclared {
			out = append(out, mk(RuleSubagentAllTools, SeverityYellow, ""))
		}
		switch s.PermissionMode {
		case "bypassPermissions", "dontAsk":
			out = append(out, mk(RuleSubagentPermissiveMode, SeverityRed, s.PermissionMode))
		case "acceptEdits", "auto":
			out = append(out, mk(RuleSubagentPermissiveMode, SeverityYellow, s.PermissionMode))
		}
		if s.HookCommands > 0 {
			out = append(out, mk(RuleSubagentHookCommand, SeverityYellow, ""))
		}
		if len(s.InlineMCPServers) > 0 {
			out = append(out, mk(RuleSubagentInlineMCP, SeverityYellow, ""))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
