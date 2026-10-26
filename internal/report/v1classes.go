package report

import (
	"strings"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/rules"
)

// The three destinations of the v1 categories (decision 2026-10-24,
// memoria, §2.1, M5). The v1 JSON sidecar keeps every v1 finding; the v2
// report reads them by destination.
//
// toV2 are expressed by v2: the repository secrets by cred_in_git, the VS
// Code terminal env by the credential collector, a new MCP server or agent
// extension by the memory, a stdio command by the agent_executable
// resource. retired give no useful action. Every other category, an
// unknown one included (fail-secure), stays in the technical report.
var (
	toV2 = map[string]bool{
		"secret_tracked": true, "secret_tracked_file": true, "secret_in_history": true, "env_tracked": true,
		"private_key_detected": true, "vscode_terminal_env_secret": true, "mcp_server_new": true,
		"mcp_server_user_writable_command": true,
	}
	retired = map[string]bool{
		"uncommitted_changes_above_threshold": true, "dependency_added": true, "dependency_lifecycle_script_new": true,
		"workflow_new_or_modified": true, "vscode_settings_modified": true, "git_hook_new_or_modified": true,
		"mcp_server_modified": true,
	}
	// signalCategory maps the technical-report categories of §2.1 (the
	// code's names where they differ from report.v1's) to the code of their
	// line in «Other signals on this computer» (adjustment B).
	signalCategory = map[string]string{
		"workflow_dangerous_pattern": "workflow_dangerous_pattern", "git_hook_with_network": "git_hook_with_network",
		"core_hookspath_redirected": "core_hooks_path_redirected", "core_hooks_path_redirected": "core_hooks_path_redirected",
		"git_alias_with_shell": "git_alias_with_shell", "lifecycle_script_with_network": "dependency_lifecycle_script_network",
		"dependency_lifecycle_script_network": "dependency_lifecycle_script_network",
		"shell_profile_modified":              "shell_profile_modified", "av_exclusion_new": "av_exclusion_new",
		"scheduled_task_new": "scheduled_task_new", "scheduled_task_logon_trigger": "scheduled_task_new",
		"global_package_new": "global_package_new", "npm_registry_non_standard": "npm_registry_non_standard",
		"suspicious_script_unexpected_location": "suspicious_script_unexpected_location",
		"vscode_workspace_trust_disabled":       "vscode_workspace_trust_disabled",
	}
)

// Signal is one line of «Other signals on this computer»: a technical-report
// category with red findings, and how many.
type Signal struct {
	Category string `json:"category"`
	Count    int    `json:"count"`
}

// agentExtension is the product name of a new VS Code extension the
// knowledge base marks as an agent.
func agentExtension(f finding.Finding) (string, bool) {
	if f.Category != "vscode_extension_new" {
		return "", false
	}
	name, ok := knowledge.AgentExtensions[strings.ToLower(f.Evidence.ExtensionID)]
	return name, ok
}

// technical reports whether a v1 finding stays in the technical report.
func technical(f finding.Finding) bool {
	if _, agent := agentExtension(f); agent {
		return false
	}
	return !toV2[f.Category] && !retired[f.Category]
}

// technicalFindings are the v1 findings of the technical report.
func technicalFindings(fs []finding.Finding) []finding.Finding {
	var out []finding.Finding
	for _, f := range fs {
		if technical(f) {
			out = append(out, f)
		}
	}
	return out
}

// signals are the technical-report categories with a red finding, in the
// order of §2.1's list, with their count.
func signals(fs []finding.Finding) []Signal {
	counts := map[string]int{}
	for _, f := range fs {
		if c, ok := signalCategory[f.Category]; ok && f.Severity == finding.SeverityRed && technical(f) {
			counts[c]++
		}
	}
	out := []Signal{}
	for _, c := range SignalOrder {
		if counts[c] > 0 {
			out = append(out, Signal{Category: c, Count: counts[c]})
		}
	}
	return out
}

// SignalOrder is the order of the technical-report list of §2.1.
var SignalOrder = []string{
	"workflow_dangerous_pattern", "git_hook_with_network", "core_hooks_path_redirected", "git_alias_with_shell",
	"dependency_lifecycle_script_network", "shell_profile_modified", "av_exclusion_new", "scheduled_task_new",
	"global_package_new", "npm_registry_non_standard", "suspicious_script_unexpected_location",
	"vscode_workspace_trust_disabled",
}

// newAgents are the agents new since the last run (M5): a new agent
// extension, by name, sorted. A new MCP server is not an agent (v3 §13.3):
// newMCPServers.
func newAgents(fs []finding.Finding) []string {
	var out []string
	for _, f := range fs {
		if name, ok := agentExtension(f); ok {
			out = append(out, name)
		}
	}
	return uniqueSorted(out)
}

// newMCPServers are the MCP servers new since the last run
// (mcp_server_new), by name, sorted (v3 §13.3).
func newMCPServers(fs []finding.Finding) []string {
	var out []string
	for _, f := range fs {
		if f.Category == "mcp_server_new" && f.Evidence.MCPServer != "" {
			out = append(out, f.Evidence.MCPServer)
		}
	}
	return uniqueSorted(out)
}

// envFilesTracked counts the .env files tracked by git that hold no
// recognisable secret (§2.1): env_tracked findings whose file is not a
// file of a committed secret.
func envFilesTracked(fs []finding.Finding, committed []finding.CommittedSecret) int {
	withSecret := map[string]bool{}
	for _, c := range committed {
		if c.Presence == finding.PresenceTracked {
			withSecret[graph.CanonicalPath(c.Repo)+"\x00"+c.File] = true
		}
	}
	files := map[string]bool{}
	for _, f := range fs {
		if f.Category != "env_tracked" {
			continue
		}
		k := graph.CanonicalPath(f.Evidence.RepoPath) + "\x00" + f.Evidence.File
		if !withSecret[k] {
			files[k] = true
		}
	}
	return len(files)
}

// committedOf gathers the committed secrets of the repository findings.
func committedOf(fs []finding.Finding) []finding.CommittedSecret {
	var out []finding.CommittedSecret
	for _, f := range fs {
		out = append(out, f.Committed...)
	}
	return out
}

// setPostureCount sets a posture code's count.
func setPostureCount(ps []rules.Posture, code string, n int) {
	for i := range ps {
		if ps[i].Code == code {
			ps[i].Count = n
		}
	}
}
