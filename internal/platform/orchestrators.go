package platform

// The orchestrators of CTO-76 that BV-268 reads at rest: Paseo, Claude
// Squad and Orca, plus Claude Code's subagents. Every reader keeps only the
// fields a rule or the graph uses; values that may be secrets (Paseo's
// launchEnv and API keys, Claude Squad's diffs) are never kept.

// OrchestratorScan is what ListOrchestrators read.
type OrchestratorScan struct {
	Subagents   []ClaudeSubagent
	Paseo       *PaseoScan
	ClaudeSquad *ClaudeSquadScan
	Orca        *OrcaScan
	Sources     []MCPSource
}

// PaseoScan is PASEO_HOME, or ~/.paseo.
type PaseoScan struct {
	Home, ConfigPath string
	ConfigStatus     MCPSourceStatus
	Reason           string
	// Listen is config.json's daemon.listen, empty when absent (the default
	// 127.0.0.1:6767 applies).
	Listen string
	// APIKeys are the JSON pointers of config.json's apiKey fields that hold
	// a value. Only the pointer: the value is never kept.
	APIKeys []string
	// Agents are agents/<project>/<id>.json; Schedules schedules/<id>.json.
	Agents    []PaseoAgent
	Schedules []PaseoSchedule
	// Projects are the rootPath of projects/projects.json.
	Projects []string
}

// PaseoAgent is one stored agent: its provider (claude, codex…), its
// effective mode (lastModeId, else config.modeId: agent-manager.ts:241 at
// v0.16.2) and its working folder.
type PaseoAgent struct {
	Path, Provider, Mode, Cwd string
	Archived                  bool
	Status                    MCPSourceStatus
	Reason                    string
}

// PaseoSchedule is one schedule: whether it is active and, when it starts a
// new agent, that agent's provider and mode.
type PaseoSchedule struct {
	Path, ScheduleStatus, Provider, Mode string
	Status                               MCPSourceStatus
	Reason                               string
}

// ClaudeSquadScan is ~/.claude-squad (config/config.go:21-26: the home
// folder only, no variable).
type ClaudeSquadScan struct {
	Dir, ConfigPath string
	ConfigStatus    MCPSourceStatus
	Reason          string
	// From config.json: auto_yes, default_program and the profiles' programs.
	AutoYes        bool
	DefaultProgram string
	Profiles       []string
	// From state.json, of each instance only its program and auto_yes.
	StatePath  string
	Instances  []ClaudeSquadInstance
	StateError string
	// DaemonPID is true when daemon.pid exists: the auto-yes daemon was
	// launched when the interface closed (daemon/daemon.go:120-121); it may
	// be left over if the daemon died.
	DaemonPID     bool
	DaemonPIDPath string
}

// ClaudeSquadInstance is one session of state.json.
type ClaudeSquadInstance struct {
	Program string
	AutoYes bool
}

// OrcaScan is Orca's presence (its userData folder or ~/.orca) and the
// hooks it installed in other agents' configuration.
type OrcaScan struct {
	UserData, Home string
	Hooks          []OrcaHook
}

// OrcaHook is an agent configuration file that holds a hook command of
// Orca's (its ~/.orca/agent-hooks/ scripts).
type OrcaHook struct {
	Agent, ConfigPath string
}
