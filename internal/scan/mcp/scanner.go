// Package mcp implements the MCP server command-location scanner.
// ScanMCPServers is the sole entry point; it is a pure function (no I/O)
// so it can be exercised in unit tests without any OS-specific adapters.
package mcp

import (
	"fmt"
	"strings"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/platform"
)

// Finding text approved by human owner (2026-05-22).
const (
	whyMCPServerWritable = "The command field points to a location under your user profile that " +
		"is not in RosmarOps's list of approved toolchain directories. Any process running as your " +
		"user account can silently replace the binary at that path before the next agent " +
		"invocation, achieving arbitrary code execution inside your AI agent without any " +
		"visible config change."

	actionMCPServerWritable = "Check whether the path is a recognized toolchain bin directory. " +
		"If it is a legitimate tool not yet " +
		"in the list, open an issue to add it. Otherwise, move the binary to a trusted managed " +
		"location (e.g., install via Volta, nvm, Cargo, or Go toolchain) and update the MCP " +
		"server config to point to the new path."
)

// safeSubpaths is the hardcoded list of approved toolchain subdirectories under the
// user's home directory that are legitimate install targets for MCP server binaries.
// Not user-configurable — this is RosmarOps's security policy.
//
// Paths are lowercased and use forward slashes for cross-platform string matching.
//
// Note: ".nvm/" is included for Linux/macOS portability. On Windows, nvm-windows
// typically installs to %APPDATA%\nvm or C:\nvm, so this entry is effectively a
// no-op on Windows but is kept for future cross-platform support.
//
// "%USERPROFILE%\.claude\" is intentionally NOT in this list: it is an app state
// directory (configs, plugins, cache), not a toolchain bin dir. A plugin binary
// dropped under .claude\plugins\<x>\bin\ must not be auto-trusted — detecting such
// drift is exactly RosmarOps's purpose.
//
// Owner-approved 2026-05-21.
var safeSubpaths = []string{
	".volta/bin/",
	".nvm/",
	"appdata/roaming/npm/",
	"appdata/local/programs/",
	"go/bin/",
	".cargo/bin/",
	".local/bin/",
}

// ScanResult bundles all outputs from a single ScanMCPServers call.
type ScanResult struct {
	Findings         []finding.Finding
	AllowListedCount int // servers whose findings were all suppressed by the allow-list
}

// ScanMCPServers returns the findings of the MCP servers a client loads
// (LoadsMCP; decision 2026-10-26, agentes y MCP nuevos, A4) whose command is
// in a user-writable location. It is a pure function with no filesystem or OS
// access.
//
//   - mcp_server_user_writable_command (RED): command is an absolute path under homeDir
//     not in the approved safe subpath list. Fires on every run — binary
//     replacement is a continuous threat, not a one-time event.
//
// A new server is the v2 memory's (A1) and a modified one is retired (A4):
// there is no reference.
//
// homeDir is the resolved user home directory (os.UserHomeDir() result). Passing it as
// a parameter keeps this function testable without OS calls.
func ScanMCPServers(
	current []platform.MCPServer,
	allowList []config.AllowEntryID,
	homeDir string,
) ScanResult {
	// Build allow-list set: lowercase "client:name" → present.
	allowSet := make(map[string]struct{}, len(allowList))
	for _, a := range allowList {
		allowSet[strings.ToLower(a.ID)] = struct{}{}
	}

	var (
		findings         []finding.Finding
		allowListedCount int
	)

	for _, srv := range current {
		if !srv.LoadsMCP {
			continue // a file the client does not load MCP servers from (A4)
		}
		nameKey := strings.ToLower(srv.Client) + ":" + strings.ToLower(srv.Name)

		// Allow-list check first — suppresses ALL findings for this server.
		if _, allowed := allowSet[nameKey]; allowed {
			allowListedCount++
			continue
		}

		// mcp_server_user_writable_command fires on every run,
		// because the threat (silent binary replacement) is continuous.
		if isUserWritableCommand(srv.Command, homeDir) {
			f := finding.NewFinding(
				findingID("mcp_server_user_writable_command", srv.Client, srv.Name),
				finding.SeverityRed,
				"mcp_server_user_writable_command",
				fmt.Sprintf("MCP server uses a user-writable command path: %s/%s", srv.Client, srv.Name),
			)
			f.WhyItMatters = whyMCPServerWritable
			f.SuggestedAction = actionMCPServerWritable
			f.Confidence = "high"
			f.Evidence = finding.Evidence{
				MCPClient: srv.Client,
				MCPServer: srv.Name,
				Command:   srv.Command,
				File:      srv.ConfigPath,
			}
			findings = append(findings, f)
		}
	}

	return ScanResult{
		Findings:         findings,
		AllowListedCount: allowListedCount,
	}
}

// findingID returns a deterministic finding ID for an MCP server event.
// Format: <category>:<sha256[:16]> — hash input is lowercase(client)+":"+lowercase(name).
// The ID is stable across runs and independent of command contents, so that
// mcp_server_new, mcp_server_modified, and mcp_server_user_writable_command each
// produce distinct full IDs for the same server (different category prefix).
func findingID(category, client, name string) string {
	h := hash.Bytes([]byte(strings.ToLower(client) + ":" + strings.ToLower(name)))
	return category + ":" + h[:16]
}

// isUserWritableCommand reports whether command is an absolute path under homeDir
// that is not in the approved safe subpaths list.
//
// Returns false for bare command names (e.g., "npx", "uvx") and relative paths —
// those resolve via PATH lookup, which is a different threat surface.
//
// Comparison is case-insensitive and normalised to forward slashes, so it works
// correctly regardless of whether the config stores backslashes or forward slashes.
func isUserWritableCommand(command, homeDir string) bool {
	if !isAbsPath(command) {
		return false
	}

	cmdLow := strings.ToLower(toSlash(command))
	homeLow := strings.ToLower(toSlash(strings.TrimRight(homeDir, "/\\"))) + "/"

	if !strings.HasPrefix(cmdLow, homeLow) {
		return false // not under home dir
	}

	suffix := cmdLow[len(homeLow):]
	for _, safe := range safeSubpaths {
		if strings.HasPrefix(suffix, safe) {
			return false // approved toolchain location
		}
	}

	return true // user-writable, not in approved list
}

// toSlash converts backslashes to forward slashes for cross-platform path comparison.
func toSlash(s string) string {
	return strings.ReplaceAll(s, "\\", "/")
}

// isAbsPath reports whether p is an absolute filesystem path.
// Handles Windows drive-letter paths (C:\, C:/), UNC paths (\\...), and Unix paths (/).
func isAbsPath(p string) bool {
	if len(p) == 0 {
		return false
	}
	if p[0] == '/' || p[0] == '\\' {
		return true
	}
	// Windows drive letter: e.g., "C:\" or "C:/".
	if len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		return true
	}
	return false
}
