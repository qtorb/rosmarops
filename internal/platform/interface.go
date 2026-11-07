// Package platform defines the abstract interface over OS-specific operations.
// Each supported OS provides an adapter in adapters/<os>/ that implements
// this interface. Scanners depend only on this interface, never on adapters,
// so the scan logic is testable without OS-specific I/O.
package platform

import (
	"context"
	"time"

	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// VSCodeExtension describes a VS Code extension as discovered at runtime.
// This is the live view; the persisted baseline is state.VSCodeExtension.
type VSCodeExtension struct {
	// ID is the publisher.extensionname identifier, lowercased.
	// Example: "ms-python.python", "eamodio.gitlens".
	ID string

	// Version as reported by extensions.json or the VS Code CLI.
	Version string

	// Enabled is best-effort. When the VS Code CLI is unavailable,
	// all extensions are reported as Enabled=true (conservative: safer
	// to report a disabled extension than to silently miss it).
	Enabled bool

	// InstalledAt is the install date extensions.json records
	// (metadata.installedTimestamp, milliseconds), zero when it has none
	// (decision 2026-10-26, agentes y MCP nuevos, A5).
	InstalledAt time.Time
}

// MCPScan is the result of reading all known MCP client config files.
type MCPScan struct {
	// Servers are the MCP servers read, from every source that was read.
	Servers []MCPServer

	// Sources lists every client config file considered, in discovery order,
	// including missing ones.
	Sources []MCPSource

	// Present lists the agents detected by the existence of their
	// configuration, without reading it (decision 2026-10-27, agentes
	// nuevos, N1): each one is an ai_client, detected and not evaluated.
	Present []AgentPresence
}

// AgentPresence is an agent found on disk by the existence of an entry of
// its configuration; nothing of it is read.
type AgentPresence struct {
	// Platform is the product code ("codex", "antigravity", "hermes",
	// "claude-desktop").
	Platform string

	// ConfigDir is the agent's configuration folder: its ai_client identity.
	ConfigDir string

	// Marker is the file or folder that showed it.
	Marker string
}

// MCPSourceStatus is the outcome of reading one MCP client config file.
type MCPSourceStatus string

const (
	// MCPSourceRead means the file was read and parsed. It may declare no
	// servers at all: the client is still present.
	MCPSourceRead MCPSourceStatus = "read"

	// MCPSourceMissing means the file does not exist.
	MCPSourceMissing MCPSourceStatus = "missing"

	// MCPSourceFailed means the file exists but could not be read or parsed.
	MCPSourceFailed MCPSourceStatus = "failed"
)

// MCPSource is one MCP client config file and what happened when reading it.
type MCPSource struct {
	// Platform is the client label, as in MCPServer.Client.
	Platform string

	// ConfigPath is the absolute path of the config file.
	ConfigPath string

	// ConfigDir is the configuration folder of the client product this file
	// belongs to: the identity of its ai_client node in the v2 graph (one node
	// per product and folder). Claude Code: CLAUDE_CONFIG_DIR, or ~/.claude;
	// Cursor: ~/.cursor; Cline: its settings folder. Set by the adapter.
	ConfigDir string

	// Status is the read outcome.
	Status MCPSourceStatus

	// Reason explains a failed read. Empty unless Status is MCPSourceFailed.
	Reason string

	// LoadsMCP reports whether the client actually loads the MCP servers
	// declared in this file. Set by the adapter when it defines each source;
	// the graph builder relies on it and never guesses from the file name.
	// False for Claude Code's settings.json and settings.local.json: Claude
	// Code loads MCP servers only from ~/.claude.json and <repo>/.mcp.json
	// (https://code.claude.com/docs/en/mcp,
	// https://code.claude.com/docs/en/settings). Such a file is still read
	// for the v1 scanner, but the v2 graph ignores it.
	LoadsMCP bool

	// KeyConflicts are the JSON Pointers of the keys of this file that match
	// a key RosmarOps reads only without regard to case (decision
	// 2026-10-27, blindaje, B5): neither the agent nor RosmarOps reads them,
	// and each one is a config_key_case_conflict finding.
	KeyConflicts []string
}

// MCPServer describes one entry under "mcpServers" in an AI agent's MCP config file.
// This is the live view; the persisted baseline is state.MCPServerBaseline.
type MCPServer struct {
	// Client identifies which agent config file this server was read from.
	// One of: "claude-code", "claude-code-project", "cline", "cursor",
	// "claude-desktop", "antigravity".
	Client string

	// Name is the key under "mcpServers" in the config JSON.
	Name string

	// Command is the binary or bare command name to invoke.
	Command string

	// Args are the positional arguments passed to Command.
	Args []string

	// EnvKeys are the sorted key names from the "env" object.
	// Values are NEVER stored — only key names, to prevent secrets from
	// appearing in state files or reports (invariant §3).
	EnvKeys []string

	// Env holds the "env" entries with their values, sorted by Key. Values are
	// secrets.SecretValue: every print or encoding is redacted, and only the
	// credential fingerprint calls Reveal. Env is never persisted; the v1
	// scanner and baseline use EnvKeys only.
	Env []EnvVar

	// Headers holds the "headers" entries of a remote server (type http,
	// streamable-http, sse or ws), with SecretValue values, sorted by Key. Nil
	// for stdio servers. Never persisted, like Env.
	Headers []EnvVar

	// ConfigPath is the absolute path of the config file this server was read from.
	ConfigPath string

	// Pointer is the JSON Pointer (RFC 6901) of the server entry inside
	// ConfigPath, e.g. "/mcpServers/github" or
	// "/projects/~1home~1me~1repo/mcpServers/github". Empty means
	// "/mcpServers/<Name>".
	Pointer string

	// LoadsMCP is the LoadsMCP of the source this server was read from.
	LoadsMCP bool

	// ConfigDir is the ConfigDir of the source this server was read from.
	ConfigDir string

	// Scope is where the client loads the server: MCPScopeUser,
	// MCPScopeLocal (one project, from ~/.claude.json) or MCPScopeProject
	// (a repository's .mcp.json). Set by the adapter.
	Scope string

	// ScopePath is the project path of a local-scope server or the
	// repository root of a project-scope server, as read. Empty for user scope.
	ScopePath string

	// EnvFile is the resolved path of a Cursor server's envFile: its entries
	// go to the credential files collector, received by the server
	// (decision 2026-10-25, Cursor, K10). Empty otherwise.
	EnvFile string

	// Transport is how the client reaches the server: "stdio", "http", "sse"
	// or "ws" (BV-91, D1). Empty when the adapter could not tell.
	Transport string

	// URL is the remote server's URL as read from the config ("url", or
	// Antigravity's "serverUrl"), in clear, for the graph to parse into a
	// canonical origin and a salted destination_ref (BV-91, D1/D2). Empty for a
	// stdio server. Like Env, it is never persisted; only the origin (scheme,
	// host and non-default port) and a salted hash reach the graph.
	URL string

	// CommandFileDigest is the SHA-256 (lowercase hex) of the local program a
	// stdio server runs — its command when that is a file path, else the first
	// argument that is a file path — when the file exists and is at most 2 MB
	// (BV-91, E3). "unreadable" when the file exists but could not be read or is
	// too big; empty when there is no such file. It feeds destination_ref so an
	// edit to the program is seen as a change of destination.
	CommandFileDigest string
}

// ClaudeSettingsScan is what ListClaudeSettings read: Claude Code's
// permission settings files and the workspace trust recorded in
// ~/.claude.json.
type ClaudeSettingsScan struct {
	// Files reports every settings file considered, read or not.
	Files []ClaudeSettingsFile

	// Trust holds, for every project entry of ~/.claude.json, its path as
	// written there and its hasTrustDialogAccepted value. Empty when the file
	// is missing or could not be read (its read outcome is reported by
	// ListMCPServers).
	Trust []ProjectTrust

	// Managed lists the managed settings sources considered, highest
	// priority first (https://code.claude.com/docs/en/managed-settings#how-claude-code-combines-managed-sources).
	// Empty on platforms where they do not exist.
	Managed []ManagedSource

	// ManagedMCP is managed-mcp.json in the managed settings directory: a
	// managed MCP server file, not a settings source. Status only.
	ManagedMCP *ClaudeSettingsFile

	// LegacyManagedPath is the legacy Windows managed settings path when a
	// file exists there. Claude Code no longer reads it; it is never opened.
	LegacyManagedPath string

	// VSCode is claudeCode.initialPermissionMode from VS Code's user
	// settings; nil when the file does not exist.
	VSCode *VSCodeModeSetting
}

// Managed settings source kinds, highest priority first.
const (
	ManagedRemote = "remote" // <config folder>/remote-settings.json, the cache of server-managed settings
	ManagedHKLM   = "hklm"   // HKLM\SOFTWARE\Policies\ClaudeCode, value Settings
	ManagedFiles  = "files"  // managed-settings.json, then managed-settings.d/*.json
	ManagedHKCU   = "hkcu"   // HKCU\SOFTWARE\Policies\ClaudeCode, value Settings (user-writable)
)

// ManagedSource is one managed settings source. Files holds its documents in
// merge order: one for remote, HKLM and HKCU; for ManagedFiles,
// managed-settings.json first, then every managed-settings.d/*.json in
// alphabetical order.
type ManagedSource struct {
	Kind  string
	Files []ClaudeSettingsFile
}

// ClaudeSettingsFile is one Claude Code settings file: user
// (<config folder>/settings.json), project (<repo>/.claude/settings.json) or
// local (<repo>/.claude/settings.local.json).
type ClaudeSettingsFile struct {
	// Scope is MCPScopeUser, MCPScopeProject or MCPScopeLocal: the settings
	// layer, as Claude Code names it (https://code.claude.com/docs/en/settings).
	Scope string

	// ScopePath is the repository root of a project or local file; empty for
	// user.
	ScopePath string

	// ConfigPath is the path of the settings file.
	ConfigPath string

	// ConfigDir is Claude Code's configuration folder (see MCPSource.ConfigDir).
	ConfigDir string

	// Status is the read outcome; Reason explains a failed read.
	Status MCPSourceStatus
	Reason string

	// Permissions holds the permission keys read.
	Permissions ClaudePermissions

	// .mcp.json approvals (https://code.claude.com/docs/en/settings-reference#enabledmcpjsonservers).
	// EnableAllProjectMcpServers is nil when the key is unset.
	EnabledMcpjsonServers      []string
	DisabledMcpjsonServers     []string
	EnableAllProjectMcpServers *bool

	// Env holds the env block, values as SecretValue (redacted on any print
	// or encoding), sorted by Key. Never persisted.
	Env []EnvVar

	// Executables are the JSON Pointers of the commands this file makes
	// Claude Code run: every hooks command, statusLine.command and
	// apiKeyHelper. The command text itself is never kept.
	Executables []string

	// HookScripts, for a project settings file only (BV-107), are the
	// commands whose text named a regular file inside the project: the
	// command's JSON Pointer, the file's project-relative path and its size
	// in bytes. Resolved while the command text is in memory; the text itself
	// is never kept (it may hold secrets).
	HookScripts []HookScript

	// Managed-only keys (https://code.claude.com/docs/en/settings-reference).
	AllowManagedPermissionRulesOnly bool
	ManagedSourcesBehavior          string

	// HasPolicyKey reports that the document sets a policy key: any key other
	// than managedSourcesBehavior and wslInheritsWindowsSettings, to a value
	// other than null (managed-settings, "When an admin document counts as
	// present").
	HasPolicyKey bool

	// KeyConflicts are the JSON Pointers of the keys of this file that match
	// a key RosmarOps reads only without regard to case (decision
	// 2026-10-27, blindaje, B5): neither the agent nor RosmarOps reads them,
	// and each one is a config_key_case_conflict finding.
	KeyConflicts []string
}

// HookScript is one project command that runs a file inside the project
// (BV-107): the command's JSON Pointer, the file's project-relative path and
// its size in bytes. The command text is never kept.
type HookScript struct {
	Pointer string
	Path    string
	Size    int64
}

// ClaudePermissions is the subset of a Claude Code settings file that grants
// or restricts what the agent can do
// (https://code.claude.com/docs/en/settings-reference).
type ClaudePermissions struct {
	Allow []string
	Ask   []string
	Deny  []string

	DefaultMode                         string
	DisableBypassPermissionsMode        string
	AdditionalDirectories               []string
	BlockReadsOutsideWorkingDirectories bool

	// DisableAutoMode is read at the root or under permissions; the root wins
	// when both are set. DisableAutoModePointer is the JSON Pointer it was
	// read from ("/disableAutoMode" or "/permissions/disableAutoMode").
	DisableAutoMode        string
	DisableAutoModePointer string
}

// InstructionScan lists the instruction files agents load (decision
// 2026-10-23, Q7) that exist on disk. Only their existence is checked: no
// file is opened, so no content is ever read.
type InstructionScan struct {
	Files  []InstructionFile
	Failed []InstructionFailure
}

// InstructionFile is one existing instruction file and the agent that loads
// it, identified as its ai_client is: product and configuration folder.
type InstructionFile struct {
	Product   string // "claude-code", "cursor", "codex", "antigravity", "hermes", "cline" or "github-copilot"
	ConfigDir string
	Path      string

	// Shared marks a file several agents load (AGENTS.md): it is listed once
	// per agent, and each agent gets its own resource.
	Shared bool
}

// SkillScan lists the files of the agent skills on disk (decision
// 2026-10-28, skills y herramientas de agentes, K1): every <skill>/SKILL.md
// as an instruction file and every file under <skill>/scripts/ as an
// executable, one entry per agent that loads the folder, all Shared. Only
// their existence is checked: no skill file is opened (Hermes Agent's
// config.yaml is read for its trusted project roots, BV-99b).
type SkillScan struct {
	Instructions []InstructionFile
	Executables  []InstructionFile
	Failed       []InstructionFailure
}

// InstructionFailure is an instruction file or folder whose existence could
// not be established, or that is over the size limit (invariant 6). Reason is
// "<code>: <detail>" with a coverage reason code.
type InstructionFailure struct {
	Product   string
	ConfigDir string
	Path      string
	Reason    string
}

// CredentialFileScan is what the credential files collector read (decision
// 2026-10-24, D1): every entry that may hold a secret, with its file and
// line. Values are SecretValue: redacted on any print or encoding, never
// persisted.
type CredentialFileScan struct {
	Entries []CredentialEntry

	// Read are the files read.
	Read []string

	// Failed are files that exist but were not read, and repositories over
	// the file-count limit. Reason is "<code>: <detail>".
	Failed []CredentialFailure

	// KeyConflicts are the keys of the JSON credential files read with
	// exact keys (Codex's .credentials.json, Hermes Agent's auth.json) that
	// match a key RosmarOps reads only without regard to case (decision
	// 2026-10-27, blindaje, B5): not read, and each one a
	// config_key_case_conflict finding.
	KeyConflicts []KeyConflict
}

// KeyConflict is one such key: its file and its JSON Pointer.
type KeyConflict struct {
	Path, Pointer string
}

// CredentialEntry is one key and value read from a credential file.
type CredentialEntry struct {
	Path  string
	Line  int
	Key   string
	Value secrets.SecretValue

	// Pointer, for a value read from a JSON file, is its JSON Pointer
	// (RFC 6901); the location is then "<Path>#<Pointer>" instead of
	// "<Path>:<Line>".
	Pointer string

	// Kind, when set, is the credential kind the file's format states for
	// the key (BV-36): the value is a credential of that kind whatever its
	// text. Empty: the value is classified by the secret patterns.
	Kind string
}

// CredentialFailure is a credential source not read.
type CredentialFailure struct {
	Path   string
	Reason string
}

// VSCodeModeSetting is the claudeCode.initialPermissionMode key of VS Code's
// user settings.json (decision 2026-10-24, D7). Mode is empty when the key
// is not set; Status and Reason are the read outcome of the file. Env is
// its claudeCode.environmentVariables, values kept as secrets (decision
// 2026-10-25, cómo trabajan tus agentes, cálculo, T12).
type VSCodeModeSetting struct {
	Path      string
	ConfigDir string // Claude Code's configuration folder (its client)
	Mode      string
	Env       []EnvVar
	Status    MCPSourceStatus
	Reason    string

	// KeyConflicts are the JSON Pointers of the keys of this file that match
	// a key RosmarOps reads only without regard to case (decision
	// 2026-10-27, blindaje, B5): neither the agent nor RosmarOps reads them,
	// and each one is a config_key_case_conflict finding.
	KeyConflicts []string
}

// ProjectTrust is the workspace trust Claude Code recorded for one project
// entry of ~/.claude.json.
type ProjectTrust struct {
	Path     string
	Accepted bool
}

// MCP server scopes (https://code.claude.com/docs/en/mcp). The same names
// are the Claude Code settings layers user, project and local; managed
// settings are the layer MCPScopeManaged.
const (
	MCPScopeManaged = "managed"
	MCPScopeUser    = "user"
	MCPScopeLocal   = "local"
	MCPScopeProject = "project"
)

// EnvVar is one entry of an MCP server's "env" object.
type EnvVar struct {
	Key   string
	Value secrets.SecretValue
}

// ShellProfile describes one PowerShell $PROFILE scope with its content hash.
// Scopes that have no profile file on disk have an empty SHA256 — this is not
// an error; it means the user has not created a profile for that scope.
type ShellProfile struct {
	// Scope is the PowerShell profile scope name.
	// One of: "CurrentUserCurrentHost", "CurrentUserAllHosts",
	//         "AllUsersCurrentHost", "AllUsersAllHosts".
	Scope string

	// Path is the absolute path to the profile file for this scope.
	// The file may not exist on disk.
	Path string

	// SHA256 is the lowercase hex SHA-256 of the file content.
	// Empty string ("") means the file does not exist.
	SHA256 string
}

// ScheduledTask describes one Windows Task Scheduler entry.
type ScheduledTask struct {
	// Name is the task name as displayed in Task Scheduler (not the full path).
	// Example: "GoogleUpdateTaskMachineCore"
	Name string

	// Path is the task folder path, including trailing backslash.
	// Example: "\Google\Update\"
	Path string

	// SHA256 is the lowercase hex SHA-256 of the canonical task definition:
	// SHA-256( name + "\x00" + path + "\x00" + triggerTypes + "\x00" + actions ).
	SHA256 string

	// HasLogonTrigger is true if any of the task's triggers fires at user logon
	// (MSFT_TaskLogonTrigger). These are the highest-risk persistence triggers.
	HasLogonTrigger bool

	// Date is the task registration date (UTC). Zero value means unknown.
	Date time.Time

	// RosmarOps is the detail of a task with an action whose executable is
	// named rosmarops.exe (decision 2026-10-26, integridad y revisión
	// semanal, J5); nil for any other task.
	RosmarOps *RosmarOpsTask
}

// RosmarOpsTask is what is read of a task that runs rosmarops.exe (J5): its
// number of actions; of its first rosmarops.exe action, the executable as
// registered and normalized (environment variables expanded, without
// quotes), the arguments and the working directory; its principal; and its
// triggers.
type RosmarOpsTask struct {
	Actions          int
	Execute          string // as registered
	ExecuteExpanded  string // variables expanded, quotes removed
	Arguments        string
	WorkingDirectory string
	UserID           string
	LogonType        string // e.g. "Interactive"
	RunLevel         string // "Limited" or "Highest"
	Triggers         []TaskTrigger
}

// TaskTrigger is one trigger: its CIM class (MSFT_TaskWeeklyTrigger, …),
// DaysOfWeek (Sunday = 1 … Saturday = 64, weekly triggers) and
// StartBoundary as registered (its time of day is the run's time).
type TaskTrigger struct {
	Class      string
	DaysOfWeek int
	Start      string
}

// DefenderExclusion describes one Windows Defender exclusion entry.
type DefenderExclusion struct {
	// Kind is one of "path", "process", or "extension".
	Kind string

	// Value is the excluded path, executable name, or file extension.
	Value string
}

// Platform abstracts OS-specific read operations that RosmarOps requires.
// Methods are added milestone by milestone; stub adapters return nil, nil
// for unimplemented methods (no-op, not an error).
type Platform interface {
	// ListVSCodeExtensions returns all installed VS Code extensions,
	// both enabled and disabled. Returns nil, nil if VS Code is not
	// installed or the extensions directory does not exist.
	ListVSCodeExtensions(ctx context.Context) ([]VSCodeExtension, error)

	// ListMCPServers reads MCP server entries from all known AI agent config
	// files on this machine. repoRoots is used to locate per-project Claude
	// Code files: <repo>/.mcp.json, which Claude Code loads MCP servers from,
	// and .claude/settings.json and .claude/settings.local.json, which it does
	// not (https://code.claude.com/docs/en/mcp) — those are read for the v1
	// scanner only and carry LoadsMCP false.
	// The result also reports every config file considered and whether it was
	// read, missing or failed. If no agent config files exist, Servers is empty
	// and every source is reported as missing.
	ListMCPServers(ctx context.Context, repoRoots []string) (MCPScan, error)

	// ListClaudeSettings reads Claude Code's permission settings: the user
	// settings file in its configuration folder and, for every repo root,
	// .claude/settings.json and .claude/settings.local.json, plus the
	// workspace trust of each project entry of ~/.claude.json. Every file
	// considered is reported with its read outcome.
	ListClaudeSettings(ctx context.Context, repoRoots []string) (ClaudeSettingsScan, error)

	// ListAgentInstructions reports the instruction files Claude Code and
	// Cursor load, in their configuration folders and in every repo root,
	// that exist. Existence only: no file is opened.
	ListAgentInstructions(ctx context.Context, repoRoots []string) (InstructionScan, error)

	// ListSkills reports the agent skills (SKILL.md and scripts/) in the
	// user's and each repository's skills folders (decision 2026-10-28, K1).
	ListSkills(ctx context.Context, repoRoots []string) (SkillScan, error)

	// ListCursor reads Cursor's permission, sandbox, CLI and hooks files
	// (decision 2026-10-25, Cursor, K10). Every file considered is reported
	// with its read outcome.
	ListCursor(ctx context.Context, repoRoots []string) (CursorScan, error)

	// ListHermes reads Hermes Agent's config.yaml and every profile's
	// (decision 2026-10-27, agentes nuevos, N2a): its approval mode, its
	// protected instruction files and its MCP servers.
	ListHermes(ctx context.Context) (HermesScan, error)

	// ListCodex reads OpenAI Codex's TOML config layers (BV-140, CTO-16 §2):
	// the user config, the profiles, each repository's .codex/config.toml (in
	// force only when the user config trusts it) and the system requirements.
	ListCodex(ctx context.Context, repoRoots []string) (CodexScan, error)

	// ListGrok reads xAI Grok Build's TOML config layers (BV-140, CTO-16 Grok
	// §2): the user config, each repository's .grok/config.toml (in force only
	// when the folder is trusted), the managed config and the requirements,
	// plus trusted_folders.toml.
	ListGrok(ctx context.Context, repoRoots []string) (GrokScan, error)

	// ListCopilot reads GitHub Copilot's VS Code configuration (BV-140, CTO-16
	// Copilot §2): the user and profile settings.json, each repository's
	// .vscode/settings.json, the Software\Policies\Microsoft\VSCode registry
	// policy layer, the MCP files and the Copilot CLI credentials.
	ListCopilot(ctx context.Context, repoRoots []string) (CopilotScan, error)

	// ListAntigravity reads Google Antigravity's CLI configuration (BV-140,
	// CTO-16 Antigravity §4): ~/.gemini/antigravity-cli/settings.json, the
	// hooks, and the credential files by fingerprint. Its MCP servers come
	// through ListMCPServers (client antigravity-cli).
	ListAntigravity(ctx context.Context, repoRoots []string) (AntigravityScan, error)

	// ListShellProfiles returns SHA-256 hashes for all four PowerShell $PROFILE
	// scopes. Scopes without a profile file on disk have an empty SHA256.
	// Returns nil, nil if PowerShell is not available.
	ListShellProfiles(ctx context.Context) ([]ShellProfile, error)

	// ListScheduledTasks returns all Windows Task Scheduler tasks.
	// The caller filters by registration date; this method returns the full set.
	// Returns nil, nil if PowerShell / Task Scheduler is not available.
	ListScheduledTasks(ctx context.Context) ([]ScheduledTask, error)

	// ListDefenderExclusions returns the current Windows Defender exclusions
	// (path, process, extension) as reported by Get-MpPreference.
	// Returns nil, nil if Defender or PowerShell is not available.
	ListDefenderExclusions(ctx context.Context) ([]DefenderExclusion, error)

	// PythonSitePackages returns the site-packages directories to inspect for
	// installed Python packages (BV-100, F0a): the interpreters' and user site,
	// the Microsoft Store Python, pipx, uv tools, conda, and each repository's
	// .venv and venv. The collector lists these one level deep; it does not
	// recurse, so the forced exclusions are untouched. Non-existent paths may be
	// returned and are skipped by the collector. The layout is Windows-specific;
	// on systems where it does not apply (macOS) this returns ErrNotApplicable,
	// which is only logged — not a failure and not a coverage gap.
	PythonSitePackages(repoRoots []string) ([]string, error)
}

// CursorScan is what the Cursor collector read (decision 2026-10-25,
// Cursor, K10): Cursor's permission, sandbox, CLI and hooks files in its
// configuration folder, in every repo root and the enterprise hooks file.
// Its MCP files are read with the MCP sources.
type CursorScan struct {
	// ConfigDir is ~/.cursor; Exists reports that the folder exists: the
	// Cursor ai_client exists then (annex 2 point 6).
	ConfigDir string
	Exists    bool

	// Files are every file considered, with its read outcome.
	Files []CursorFile

	// WorkspaceTrusted are the repo roots with .cursor/.workspace-trusted:
	// only recorded, with no effect on the inference (K10).
	WorkspaceTrusted []string
}

// Cursor file kinds.
const (
	CursorPermissions = "permissions"
	CursorSandbox     = "sandbox"
	CursorCLI         = "cli"
	CursorHooks       = "hooks"
)

// Cursor file scopes: the user's folder, a repository, or the enterprise
// hooks file.
const (
	CursorScopeUser       = "user"
	CursorScopeProject    = "project"
	CursorScopeEnterprise = "enterprise"
)

// CursorFile is one Cursor file and what it says. Only the keys RosmarOps
// uses are kept; autoRun is prose and is not kept (K10).
type CursorFile struct {
	Path   string
	Kind   string
	Scope  string
	Root   string // the repo root of a project file
	Status MCPSourceStatus
	Reason string

	// permissions.json (https://cursor.com/docs/reference/permissions.md):
	// a key present replaces the IDE's list; present and empty is an empty
	// list.
	TerminalAllowlist    []string
	HasTerminalAllowlist bool
	MCPAllowlist         []string
	HasMCPAllowlist      bool

	// sandbox.json (https://cursor.com/docs/reference/sandbox.md).
	SandboxType              string
	ReadBoundary             string
	AdditionalReadwritePaths []string

	// cli-config.json and cli.json
	// (https://cursor.com/docs/cli/reference/permissions.md).
	CLIAllow []string
	CLIDeny  []string

	// KeyConflicts are the JSON Pointers of the keys of this file that match
	// a key RosmarOps reads only without regard to case (decision
	// 2026-10-27, blindaje, B5): neither the agent nor RosmarOps reads them,
	// and each one is a config_key_case_conflict finding.
	KeyConflicts []string
}

// HermesScan is what ListHermes read (N2a, BV-97). Hermes stays detected and
// not evaluated: its MCP servers reach the graph as every agent's do; its
// approvals are read and logged, not in the graph yet (N2b).
type HermesScan struct {
	// Configs are <HERMES_HOME>\config.yaml and every
	// <HERMES_HOME>\profiles\<name>\config.yaml that exists.
	Configs []HermesConfig

	// Sources are the same files as MCP sources (Platform "hermes",
	// LoadsMCP true): a file that exists and was not read is a failed
	// source, so the run is at least yellow (blindaje, B6).
	Sources []MCPSource

	// Servers are the mcp_servers of the files read, as MCPServer (Client
	// "hermes").
	Servers []MCPServer
}

// HermesConfig is one config.yaml and its security keys.
type HermesConfig struct {
	Path      string
	ConfigDir string // the Hermes folder (or the profile's) it belongs to
	Status    MCPSourceStatus
	Reason    string

	// ApprovalMode is approvals.mode as Hermes reads it: "manual" (also
	// when absent, true or unknown), "smart" or "off" (also the YAML 1.1
	// boolean false, «mode: off»). Empty when the file was not read.
	ApprovalMode string

	// ProtectedInstructionFiles is security.protected_instruction_files;
	// nil when absent or not a boolean.
	ProtectedInstructionFiles *bool

	// TerminalBackend is terminal.backend lowercased (BV-111): "local" when
	// absent; "docker", "ssh", "modal", "daytona" or "vercel_sandbox" for an
	// isolated backend; any other value is an unrecognized plugin, treated as
	// local with a note. Empty when the file was not read.
	TerminalBackend string
}

// CodexScan is what ListCodex read of OpenAI Codex's TOML config layers
// (BV-140, CTO-16 §2). Like CursorScan for Cursor, it only reports what each
// layer says; the graph builder and the overview cells resolve the posture.
type CodexScan struct {
	// ConfigDir is CODEX_HOME (or <home>/.codex); Exists reports the folder
	// exists, so the Codex ai_client exists (CTO-16 §1, as detection does).
	ConfigDir string
	Exists    bool

	// Layers are the config layers considered, each with its read outcome.
	Layers []CodexLayer
}

// Codex config layer kinds, in CTO-16 §2 precedence order after the CLI flags
// (which leave no trace at rest).
const (
	CodexLayerProject      = "project"      // <repo>/.codex/config.toml, only in force when trusted
	CodexLayerProfile      = "profile"      // CODEX_HOME/<name>.config.toml
	CodexLayerUser         = "user"         // CODEX_HOME/config.toml
	CodexLayerRequirements = "requirements" // %ProgramData%\OpenAI\Codex\requirements.toml or the HKLM RequirementsPath
)

// CodexLayer is one Codex config layer and the subset of keys RosmarOps reads
// from it (CTO-16 §4, §6). Only keys that bear on the posture are kept; a Has*
// flag or a pointer tells absent from a zero value.
type CodexLayer struct {
	Kind   string
	Path   string
	Root   string // the repo root, for a project layer
	Status MCPSourceStatus
	Reason string

	// Trusted is whether the user config trusts this project root
	// ([projects."<root>"].trust_level = "trusted"); only for a project layer.
	// A project layer that is not trusted is read but not in force: its .codex
	// settings, hooks and rules do not load (CTO-16 §2).
	Trusted bool

	// TrustLevel is the raw [projects."<root>"].trust_level value: "trusted",
	// "untrusted", or "" when there is no entry. The asymmetry of CTO-16 §5:
	// with no entry the project's AGENTS.md still loads, but an explicit
	// "untrusted" stops it and makes commands ask (CTO-16 §4).
	TrustLevel string

	ApprovalPolicy     string // on-request | never | on-failure (alias); "" for a granular table or absent
	HasApprovalPolicy  bool
	ApprovalsReviewer  string // user | auto_review
	SandboxMode        string // read-only | workspace-write | danger-full-access
	DefaultPermissions string // :read-only | :workspace | :danger-full-access
	NetworkAccess      *bool  // [sandbox_workspace_write].network_access
	WindowsSandbox     string // [windows].sandbox: mxc | elevated | unelevated; "" absent
	HasWindowsSandbox  bool

	// Projects is the user layer's [projects] trust map: repo path →
	// trust_level, used to gate the project layers.
	Projects map[string]string

	// IgnoreDefaultExcludes is shell_environment_policy.ignore_default_excludes
	// (reads_env, CTO-16 §3); nil when absent.
	IgnoreDefaultExcludes *bool

	// HasRulesFiles reports a .codex/rules/*.rules file beside this layer
	// (exec_rules_not_evaluated, CTO-16 §4).
	HasRulesFiles bool

	// MCP are the [mcp_servers] of this layer, with their secret env and
	// headers (CTO-16 §3, credentials by fingerprint only).
	MCP []CodexMCPServer

	// BearerTokens are model_providers.<id>.experimental_bearer_token values
	// (CTO-16 §3), secret, by fingerprint only.
	BearerTokens []EnvVar
}

// CodexMCPServer is one [mcp_servers.<id>] of a Codex config layer.
type CodexMCPServer struct {
	ID         string
	Command    string
	Args       []string
	EnvKeys    []string // names only
	Env        []EnvVar // values wrapped as SecretValue
	HeaderKeys []string // http_headers names only
	Headers    []EnvVar // http_headers values wrapped as SecretValue
}

// GrokScan is what ListGrok read of xAI Grok Build's config (BV-140, CTO-16
// Grok §2). Like CodexScan, it only reports what each layer says; the graph
// builder and the overview cells resolve the posture, including Grok's
// inheritance of Claude Code and Cursor configuration.
type GrokScan struct {
	// ConfigDir is GROK_HOME (or <home>/.grok); Exists reports the folder
	// exists with a config.toml or auth.json (CTO-16 Grok §6 detection signal).
	ConfigDir string
	Exists    bool

	// FolderTrustOff reports the folder-trust control is disabled (a §2
	// exception: GROK_FOLDER_TRUST=0, [folder_trust] enabled = false, or the
	// binary has no GROK_VERSION). When off, project layers load without a
	// trusted_folders entry.
	FolderTrustOff bool

	// Trusted are the repo roots the user trusts (trusted_folders.toml
	// [folders."<root>"].trusted = true), canonical.
	Trusted []string

	// Layers are the config layers considered, each with its read outcome.
	Layers []GrokLayer
}

// Grok config layer kinds (CTO-16 Grok §2 precedence order, after the CLI
// flags which leave no trace at rest).
const (
	GrokLayerProject      = "project"      // <repo>/.grok/config.toml, only [mcp_servers]/[plugins]/[permission]/[mcp]
	GrokLayerUser         = "user"         // GROK_HOME/config.toml
	GrokLayerManaged      = "managed"      // GROK_HOME/managed_config.toml
	GrokLayerRequirements = "requirements" // GROK_HOME/requirements.toml
)

// GrokLayer is one Grok config layer and the subset of keys RosmarOps reads
// from it (CTO-16 Grok §2–§4). Only keys that bear on the posture are kept.
type GrokLayer struct {
	Kind   string
	Path   string
	Root   string // the repo root, for a project layer
	Status MCPSourceStatus
	Reason string

	// Trusted is whether the user trusts this project root (set by the reader
	// from trusted_folders.toml and the §2 exceptions); only for a project
	// layer. A project layer that is not trusted is read but not in force.
	Trusted bool

	// PermissionMode is [ui] permission_mode (default | ask | acceptEdits |
	// auto | dontAsk | bypassPermissions | always-approve); "" absent. Yolo is
	// [ui] yolo = true, equivalent to always-approve. Both only from the user
	// and managed layers (the project layer does not set [ui]).
	PermissionMode    string
	HasPermissionMode bool
	Yolo              bool

	// Allow, Deny, Ask are the [permission] rules of this layer (merged across
	// layers by the graph; deny > ask > allow). A "*" is the wildcard.
	Allow []string
	Deny  []string
	Ask   []string

	// Compat are the [compat.<vendor>] cells set in this layer: "<vendor>.<surface>"
	// → enabled. ClaudeImported is [claude_compat] imported = true. Absent
	// cells are decided elsewhere (compat_cell_remote_unknown).
	Compat         map[string]bool
	ClaudeImported bool

	// DisableBypass is [ui] disable_bypass_permissions_mode = true (requirements
	// layer), which blocks always-approve (mode_not_allowed_in_scope).
	DisableBypass bool

	// FolderTrustEnabled is [folder_trust] enabled; nil when absent. false turns
	// the folder-trust control off (a §2 exception).
	FolderTrustEnabled *bool

	// MCP are the [mcp_servers] of this layer, with their secret env and
	// headers (CTO-16 Grok §3, credentials by fingerprint only).
	MCP []GrokMCPServer

	// APIKeys are [model.<id>].api_key and [telemetry].events_api_key values
	// (CTO-16 Grok §3), secret, by fingerprint only, keyed by the model id or
	// "telemetry".
	APIKeys []EnvVar

	// SessionCreds are the plaintext credential files beside this layer
	// (auth.json, mcp_credentials.json), by fingerprint only (CTO-16 Grok §3).
	SessionCreds []EnvVar
}

// GrokMCPServer is one [mcp_servers.<id>] of a Grok config layer.
type GrokMCPServer struct {
	ID         string
	Command    string
	Args       []string
	EnvKeys    []string // names only
	Env        []EnvVar // values wrapped as SecretValue
	HeaderKeys []string // headers names only
	Headers    []EnvVar // headers values wrapped as SecretValue
}

// Copilot layer kinds (BV-140, CTO-16 Copilot §2): the VS Code settings layers
// in precedence order, user < profile < project < managed (policy, registry).
const (
	CopilotLayerUser    = "user"
	CopilotLayerProfile = "profile"
	CopilotLayerProject = "project"
	CopilotLayerManaged = "managed"
)

// CopilotScan is GitHub Copilot's at-rest configuration in VS Code (BV-140,
// CTO-16 Copilot §2–§5), read only. The ai_client exists when Present; the VS
// Code extension stays as evidence of presence (knowledge.AgentExtensions).
type CopilotScan struct {
	// ConfigDir is the VS Code user folder (%APPDATA%\Code\User); it is the
	// Copilot ai_client's identity. Present reports Copilot is on the machine.
	ConfigDir string
	Present   bool

	// Layers are the settings layers considered, each with its read outcome,
	// in precedence order (user < profile < project < managed policy).
	Layers []CopilotLayer

	// MCP are the MCP configuration files read: the VS Code `servers` format
	// (user-profile mcp.json and <repo>\.vscode\mcp.json) and the portable
	// Copilot `mcpServers` format (~/.copilot/mcp-config.json). Credentials by
	// fingerprint only (CTO-16 Copilot §3).
	MCP []CopilotMCPSource

	// Creds are plaintext credentials beside Copilot CLI / harness
	// (~/.copilot/config.json), by fingerprint only (CTO-16 Copilot §3).
	Creds []EnvVar
}

// CopilotLayer is one VS Code settings layer, with the chat.* keys that bear on
// posture (CTO-16 Copilot §4). A nil pointer means the key is absent in this
// layer; the graph merges the layers by precedence.
type CopilotLayer struct {
	Kind   string // CopilotLayerUser | Profile | Project | Managed
	Path   string // settings.json path, or "registry:<hive>\<key>" for the policy layer
	Root   string // repo root, for a project layer
	Status MCPSourceStatus
	Reason string

	// GlobalAutoApprove is chat.tools.global.autoApprove (or the policy
	// ChatToolsAutoApprove): auto-approve all tools ("Allow all"). Honored at
	// user/managed scope (CTO-16 Copilot §4: "auto-approve tools across all
	// workspaces"); a project layer that sets it is not in force.
	GlobalAutoApprove *bool

	// TerminalEnableAutoApprove is chat.tools.terminal.enableAutoApprove and
	// TerminalIgnoreDefaults is chat.tools.terminal.ignoreDefaultAutoApproveRules.
	// TerminalAutoApproveAll reports a catch-all true entry in
	// chat.tools.terminal.autoApprove (e.g. "/.*/": true), which auto-approves
	// every command (CTO-16 Copilot §7 case 3).
	TerminalEnableAutoApprove *bool
	TerminalIgnoreDefaults    *bool
	TerminalAutoApproveAll    *bool

	// EditsProtect reports chat.tools.edits.autoApprove set to a non-empty map:
	// some workspace files then require approval before edits (CTO-16 Copilot
	// §4; the default {} protects nothing, so edits apply without asking).
	EditsProtect *bool

	// AgentEnabled is chat.agent.enabled (or the policy ChatAgentMode): false
	// turns agent mode off.
	AgentEnabled *bool

	// PermissionsDefault is chat.permissions.default: "default" (Manual),
	// "autoApprove" (Allow all) or "autopilot"; "" absent. Approvals and Mode
	// are the policy ChatDefaultConfiguration's "approvals"
	// (manual/assisted/allowAll) and "mode" (interactive/plan/autopilot); ""
	// absent (CTO-16 Copilot §2, §4).
	PermissionsDefault string
	Approvals          string
	Mode               string
}

// CopilotMCPSource is one MCP configuration file Copilot reads (CTO-16 Copilot
// §2): the VS Code `servers` format or the portable `mcpServers` format.
type CopilotMCPSource struct {
	Path    string
	Root    string // repo root for a project file; "" for a user file
	Format  string // "servers" (VS Code) | "mcpServers" (portable Copilot)
	Status  MCPSourceStatus
	Reason  string
	Servers []CopilotMCPServer
}

// CopilotMCPServer is one MCP server of a Copilot MCP file, with its secret env
// and headers by fingerprint only (CTO-16 Copilot §3).
type CopilotMCPServer struct {
	ID         string
	Command    string
	Args       []string
	URL        string   // remote server url (VS Code `servers`) — in clear for the graph to parse
	EnvKeys    []string // names only
	Env        []EnvVar // values wrapped as SecretValue
	HeaderKeys []string // headers names only
	Headers    []EnvVar // headers values wrapped as SecretValue
}

// AntigravityScan is Google Antigravity's CLI at-rest configuration (BV-140,
// CTO-16 Antigravity §4), read only. Its MCP servers come through the shared
// MCP pipeline (client antigravity-cli); this scan carries its settings.json,
// its hooks and its credentials by fingerprint. The ai_client exists when
// Present. ConfigDir is ~/.gemini (shared with the App and IDE).
type AntigravityScan struct {
	ConfigDir string
	Present   bool
	Settings  AntigravitySettings
	Hooks     []AntigravityHook
	Creds     []EnvVar
}

// AntigravitySettings is the CLI's ~/.gemini/antigravity-cli/settings.json
// (CTO-16 Antigravity §4). Empty strings mean the key is absent (the default
// applies). Allow/Ask/Deny are the permissions rules, each "action(target)".
type AntigravitySettings struct {
	Path                    string
	Status                  MCPSourceStatus
	Reason                  string
	ToolPermission          string // request-review | proceed-in-sandbox | strict | always-proceed
	AgentMode               string // default | accept-edits | auto | plan
	ArtifactReviewPolicy    string // asks-for-review | agent-decides | always-proceed
	AllowNonWorkspaceAccess bool
	Allow                   []string
	Ask                     []string
	Deny                    []string
}

// AntigravityHook is one PreToolUse/other hook (CTO-16 Antigravity §4): an
// agent_executable resource. Root is the repo root for a project hook, "" for
// a user hook.
type AntigravityHook struct {
	Path string
	Root string
}
