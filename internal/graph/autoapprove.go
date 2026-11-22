package graph

import (
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/platform"
)

// ProjectAutoApproval is a Claude Code settings file whose
// enableAllProjectMcpServers: true takes effect somewhere (BV-274,
// mcp_project_autoapprove): Claude Code then approves, on its own, every MCP
// server a project's .mcp.json brings.
type ProjectAutoApproval struct {
	ConfigPath string
	Scope      string // MCPScopeManaged, MCPScopeUser, MCPScopeProject, MCPScopeLocal
	ScopePath  string // the repository root of a project or local file
}

// ProjectAutoApprovals are the settings files whose
// enableAllProjectMcpServers: true applies, with the precedence approvalState
// uses (settings-reference#enableallprojectmcpservers; mcp#project-server-
// approvals-and-workspace-trust):
//   - a managed or user file: in a folder the person has not trusted, a true
//     in either is enough, so it always applies somewhere;
//   - a project or local file: only in its trusted folder, and only when no
//     file of higher precedence (managed, then local, then project, then user)
//     sets the key first.
func ProjectAutoApprovals(scan platform.ClaudeSettingsScan) []ProjectAutoApproval {
	policy := resolveManaged(scan.Managed)
	var out []ProjectAutoApproval
	add := func(f platform.ClaudeSettingsFile, scope string) {
		out = append(out, ProjectAutoApproval{ConfigPath: f.ConfigPath, Scope: scope, ScopePath: f.ScopePath})
	}
	isTrue := func(f platform.ClaudeSettingsFile) bool {
		return f.EnableAllProjectMcpServers != nil && *f.EnableAllProjectMcpServers
	}
	for _, f := range policy.docs {
		if isTrue(f) {
			add(f, platform.MCPScopeManaged)
		}
	}
	var user []platform.ClaudeSettingsFile
	roots := map[string]bool{}
	for _, f := range scan.Files {
		if f.Status != platform.MCPSourceRead {
			continue
		}
		switch f.Scope {
		case platform.MCPScopeUser:
			user = append(user, f)
			if isTrue(f) {
				add(f, platform.MCPScopeUser)
			}
		case platform.MCPScopeProject, platform.MCPScopeLocal:
			roots[strings.TrimSuffix(canonicalPath(f.ScopePath), "/")] = true
		}
	}
	for root := range roots {
		if !trusted(scan.Trust, root) {
			continue // the project's own files are not honored there
		}
		var local, project []platform.ClaudeSettingsFile
		for _, f := range scan.Files {
			if f.Status != platform.MCPSourceRead || strings.TrimSuffix(canonicalPath(f.ScopePath), "/") != root {
				continue
			}
			switch f.Scope {
			case platform.MCPScopeLocal:
				local = append(local, f)
			case platform.MCPScopeProject:
				project = append(project, f)
			}
		}
		for _, f := range concat(policy.docs, local, project, user) {
			if f.EnableAllProjectMcpServers == nil {
				continue
			}
			// The highest-precedence file that sets the key decides; a managed
			// or user one is already listed.
			if isTrue(f) && (f.Scope == platform.MCPScopeLocal || f.Scope == platform.MCPScopeProject) {
				add(f, f.Scope)
			}
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ConfigPath < out[j].ConfigPath })
	return out
}
