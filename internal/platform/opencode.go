package platform

// The OpenCode family (BV-276): OpenCode and Kilo Code share one
// configuration model (Kilo Code is a fork of OpenCode's code). Each reads
// JSON or JSONC files from the user's folder, the project (its root and a
// dot folder) and an administrator's managed folder; later files override
// earlier ones, and the managed one overrides them all. RosmarOps reads
// only permission and mcp. Never opened: the session database
// (opencode.db, kilo.db) and the credential files (auth.json,
// mcp-auth.json), under the data folder.

// Layers of an OpenCode-family configuration, lowest precedence first.
const (
	OpenCodeLayerUser    = "user"
	OpenCodeLayerProject = "project"
	OpenCodeLayerManaged = "managed"
)

// OpenCodeScan is what the reader read of one product of the family.
type OpenCodeScan struct {
	Product   string // agentdirs.OpenCode or agentdirs.KiloCode
	Present   bool
	ConfigDir string
	// Layers are the files read, in precedence order (lowest first).
	Layers []OpenCodeLayer
	// Servers are the MCP servers of the layers (and, for Kilo Code, its
	// legacy mcp.json and mcp_settings.json files), for the shared MCP
	// pipeline. Env and header values are SecretValues; references
	// ({env:…}, {file:…}) are dropped.
	Servers []MCPServer
}

// OpenCodeLayer is one configuration file.
type OpenCodeLayer struct {
	Kind, Path, Root string // Root: the repository of a project layer
	Status           MCPSourceStatus
	Reason           string
	// Permission is the file's permission, by tool key ("*" for the whole);
	// nil when the file sets none.
	Permission map[string]OpenCodeRule
}

// OpenCodeRule is one permission key: a single action (allow, ask, deny)
// or a map of patterns to actions.
type OpenCodeRule struct {
	Action   string
	Patterns map[string]string
}

// Default is the rule's action for anything: its single action, or its "*"
// pattern; "" when it has neither.
func (r OpenCodeRule) Default() string {
	if r.Action != "" {
		return r.Action
	}
	return r.Patterns["*"]
}
