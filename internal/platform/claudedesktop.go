package platform

// Claude Desktop (BV-277): its claude_desktop_config.json is read by the
// shared MCP reader; this scan adds what bears on its score and is a file
// or a policy value. Never opened: the app's databases (IndexedDB, Local
// Storage), where its «Allow always» approvals, connectors and Cowork's
// approval mode live, and the extension settings files (Claude Extensions
// Settings/<id>.json), which can hold an extension's secrets; of these
// only the file names are listed.

// ClaudeDesktopScan is what the reader read of Claude Desktop.
type ClaudeDesktopScan struct {
	Present   bool
	ConfigDir string
	// Extensions are the ids of the installed desktop extensions (.mcpb),
	// from the file names in Claude Extensions Settings, sorted. Whether
	// each is enabled is in the file, which is never opened.
	Extensions []string
	// Managed is the administrator's policy, when one was found.
	Managed ClaudeDesktopManaged
}

// ClaudeDesktopManaged is Claude Desktop's managed configuration
// (support.claude.com/en/articles/12622667): HKLM, else HKCU,
// \SOFTWARE\Policies\Claude on Windows; the managed preferences of
// com.anthropic.claudefordesktop on macOS; /etc/claude-desktop/
// managed-settings.json on Linux. The files pass the administrator
// ownership check before they are read. Each setting is nil when the
// policy does not set it.
type ClaudeDesktopManaged struct {
	Path   string // the file, or "registry:<hive>\SOFTWARE\Policies\Claude"
	Status MCPSourceStatus
	Reason string
	// LocalMCP is isLocalDevMcpEnabled: false stops the app loading the MCP
	// servers of claude_desktop_config.json.
	LocalMCP *bool
	// Extensions is isDesktopExtensionEnabled: false stops desktop
	// extensions.
	Extensions *bool
	// PersistentAlwaysAllow is mcpPersistentAlwaysAllowEnabled: false means
	// an «Allow always» approval does not persist, so every MCP tool call
	// asks.
	PersistentAlwaysAllow *bool
}
