package platform

// Claude Code subagents (CTO-76 §5; code.claude.com/docs/en/sub-agents, read
// 2026-11-20): Markdown files with a YAML header, in <claude config
// folder>/agents/ (the user's) and <repository>/.claude/agents/ (a
// project's). RosmarOps reads only the header, and of it only the fields
// that give tools or permissions; the body (the subagent's prompt) is never
// kept.

// Where a subagent file lives.
const (
	// SubagentUser is <claude config folder>/agents/: its hooks and inline
	// MCP servers run without the folder-trust question.
	SubagentUser = "user"
	// SubagentProject is <repository>/.claude/agents/: its hooks and inline
	// MCP servers wait for the person to trust the folder.
	SubagentProject = "project"
)

// ClaudeSubagent is one subagent file's header.
type ClaudeSubagent struct {
	// Path is the .md file; Dir is the agents folder; Repo the repository
	// root for a project subagent, empty for the user's.
	Path, Dir, Repo string
	Scope           string // SubagentUser or SubagentProject
	Status          MCPSourceStatus
	Reason          string
	// Name is the header's name, as written; empty when there is none.
	Name string
	// ToolsDeclared is false when the header has no tools field: the
	// subagent then inherits every tool of the conversation.
	ToolsDeclared bool
	// PermissionMode is the header's permissionMode, as written.
	PermissionMode string
	// HookCommands counts the hooks of type command in the header.
	HookCommands int
	// InlineMCPServers are the names of the MCP servers defined in the header
	// itself (not references to servers already configured).
	InlineMCPServers []string
}

// PermissiveSubagentMode reports the permissionMode values CTO-76 §5 names
// as permissive: they let the subagent act without asking for some or all
// actions.
func PermissiveSubagentMode(mode string) bool {
	switch mode {
	case "acceptEdits", "auto", "dontAsk", "bypassPermissions":
		return true
	}
	return false
}
