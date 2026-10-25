package windows

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// MaxConfigBytes is the content-scan size limit (invariant 6): config files
// larger than this are never opened. Same value as repos.MaxFileBytes.
const MaxConfigBytes = 2 * 1024 * 1024

// mcpSettingsFile is the minimal schema shared by all AI agent MCP config files.
// Claude Code, Cline, and Cursor all use the same top-level "mcpServers" key.
// Unknown fields (e.g., Claude Code's "autoUpdatesChannel", "theme") are ignored
// by json.Unmarshal because the struct has no corresponding fields.
type mcpSettingsFile struct {
	MCPServers map[string]mcpServerEntry `json:"mcpServers"`
}

// claudeJSONFile is the only part of ~/.claude.json RosmarOps reads: user-scope
// servers (top-level mcpServers), local-scope servers
// (projects.<absolute path>.mcpServers) and each project's workspace trust
// (projects.<absolute path>.hasTrustDialogAccepted,
// https://code.claude.com/docs/en/permissions#project-allow-rules-and-workspace-trust).
// The file also holds account details and history; no struct declares those
// fields, so json.Unmarshal discards them and none of it reaches the graph,
// the report or the log.
type claudeJSONFile struct {
	MCPServers map[string]mcpServerEntry `json:"mcpServers"`
	Projects   map[string]struct {
		MCPServers             map[string]mcpServerEntry `json:"mcpServers"`
		HasTrustDialogAccepted bool                      `json:"hasTrustDialogAccepted"`
	} `json:"projects"`
}

type mcpServerEntry struct {
	Type    string            `json:"type"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
	Headers map[string]string `json:"headers"`

	// EnvFile is Cursor's envFile (https://cursor.com/docs/context/mcp).
	EnvFile string `json:"envFile"`
}

// fileFormat selects how ReadMCPFile parses a config file.
type fileFormat int

const (
	// formatMCPServers: a file with a top-level "mcpServers" object.
	formatMCPServers fileFormat = iota
	// formatClaudeJSON: Claude Code's ~/.claude.json (user and local scopes).
	formatClaudeJSON
)

// remoteTypes are the Claude Code entry types for remote servers, per
// https://code.claude.com/docs/en/mcp ("streamable-http" is an alias of
// "http"). An entry with any other type, or none, is stdio — even if it has a
// url: Claude Code reads an entry with no type as a stdio server.
var remoteTypes = map[string]bool{"http": true, "streamable-http": true, "sse": true, "ws": true}

// ListMCPServers implements platform.Platform.
//
// Sources scanned:
//  1. Claude Code user and local scopes: %USERPROFILE%\.claude.json
//     (top-level mcpServers; projects.<path>.mcpServers)
//  2. Claude Code project scope: <repo>\.mcp.json (per repo in repoRoots)
//  3. Cline:  %APPDATA%\Code\User\globalStorage\saoudrizwan.claude-dev\settings\cline_mcp_settings.json
//  4. Cursor: %USERPROFILE%\.cursor\mcp.json
//
// Claude Code loads MCP servers only from ~/.claude.json and <repo>/.mcp.json
// (https://code.claude.com/docs/en/mcp, https://code.claude.com/docs/en/settings).
// It does NOT load them from %USERPROFILE%\.claude\settings.json or
// <repo>\.claude\settings(.local).json. Those files are still read, as in
// v0.1, so the v1 scanner keeps its input, but their sources and servers carry
// LoadsMCP false and the v2 graph ignores them.
//
// Plugin marketplace templates under %USERPROFILE%\.claude\plugins\ are NOT scanned
// in v0.1 — templates ≠ active MCP servers. v0.2+ should consider emitting YELLOW
// informational findings for new templates as defense-in-depth against pre-positioned
// malicious templates.
//
// Missing files are skipped (not an error). Malformed JSON emits a slog.Warn
// and is skipped so that one broken config does not abort the whole scan.
// Files over 2 MB are not opened (invariant 6). Every file considered is
// reported in MCPScan.Sources with its outcome, so callers can tell a client
// that is absent from one that could not be read.
func (w *WindowsPlatform) ListMCPServers(_ context.Context, repoRoots []string) (platform.MCPScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.MCPScan{}, fmt.Errorf("resolve home dir: %w", err)
	}

	// %APPDATA% is normally set on Windows. If unset, derive from home as fallback.
	appData := os.Getenv("APPDATA")
	if appData == "" {
		appData = filepath.Join(home, "AppData", "Roaming")
	}

	var scan platform.MCPScan
	// add reads one source. Its spec fixes, per source, whether the client
	// really loads the MCP servers of that file, the client's configuration
	// folder and the scope; all are copied to every server read from it.
	add := func(spec sourceSpec) {
		servers, src := readMCPFile(spec)
		scan.Servers = append(scan.Servers, servers...)
		scan.Sources = append(scan.Sources, src)
	}
	claudeDir := claudeConfigDir(home)

	// 1. Claude Code global: settings.json (not an MCP source; v1 only) and
	// ~/.claude.json (user and local scopes).
	add(sourceSpec{
		path: filepath.Join(home, ".claude", "settings.json"), client: "claude-code",
		format: formatMCPServers, configDir: claudeDir, scope: platform.MCPScopeUser,
	})
	add(sourceSpec{
		path: filepath.Join(home, ".claude.json"), client: "claude-code",
		format: formatClaudeJSON, loadsMCP: true, configDir: claudeDir, scope: platform.MCPScopeUser,
	})

	// 2. Claude Code per project: settings.json and the gitignored
	// settings.local.json (not MCP sources; v1 only), and the project-scope
	// .mcp.json.
	for _, root := range repoRoots {
		project := func(path string, loadsMCP bool) sourceSpec {
			return sourceSpec{
				path: path, client: "claude-code-project", format: formatMCPServers, loadsMCP: loadsMCP,
				configDir: claudeDir, scope: platform.MCPScopeProject, scopePath: root,
			}
		}
		add(project(filepath.Join(root, ".claude", "settings.json"), false))
		add(project(filepath.Join(root, ".claude", "settings.local.json"), false))
		add(project(filepath.Join(root, ".mcp.json"), true))
	}

	// 3. Cline (VS Code extension saoudrizwan.claude-dev).
	clineDir := filepath.Join(appData, "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings")
	add(sourceSpec{
		path: filepath.Join(clineDir, "cline_mcp_settings.json"), client: "cline",
		format: formatMCPServers, loadsMCP: true, configDir: clineDir, scope: platform.MCPScopeUser,
	})

	// 4. Cursor (adopted standard MCP format; top-level mcpServers key):
	// ~/.cursor/mcp.json and <repo>/.cursor/mcp.json (decision 2026-10-25,
	// Cursor, K10).
	cursorDir := filepath.Join(home, ".cursor")
	add(sourceSpec{
		path: filepath.Join(cursorDir, "mcp.json"), client: "cursor",
		format: formatMCPServers, loadsMCP: true, configDir: cursorDir, scope: platform.MCPScopeUser,
		home: home,
	})
	for _, root := range repoRoots {
		add(sourceSpec{
			path: filepath.Join(root, ".cursor", "mcp.json"), client: "cursor",
			format: formatMCPServers, loadsMCP: true, configDir: cursorDir, scope: platform.MCPScopeProject, scopePath: root,
			home: home,
		})
	}

	return scan, nil
}

// sourceSpec defines one MCP source: the file, the scanner's client label,
// its format, whether the client loads MCP servers from it (see
// platform.MCPSource.LoadsMCP), the client's configuration folder and the
// scope of its servers. Local-scope entries of ~/.claude.json override the
// scope per server.
type sourceSpec struct {
	path      string
	client    string
	format    fileFormat
	loadsMCP  bool
	configDir string
	scope     string
	scopePath string

	// home resolves "~/" in a Cursor envFile.
	home string
}

// claudeConfigDir is Claude Code's configuration folder: CLAUDE_CONFIG_DIR
// when set, otherwise <home>/.claude (decision 2026-10-23, Q1).
func claudeConfigDir(home string) string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return dir
	}
	return filepath.Join(home, ".claude")
}

// specFor is the spec of a single file read outside ListMCPServers: the
// configuration folder and scope follow from the client label. A
// "claude-code-project" file is taken to sit at its repository root.
func specFor(path, client string, format fileFormat, loadsMCP bool) sourceSpec {
	spec := sourceSpec{
		path: path, client: client, format: format, loadsMCP: loadsMCP,
		configDir: filepath.Dir(path), scope: platform.MCPScopeUser,
	}
	if strings.HasPrefix(client, "claude-code") {
		home, _ := os.UserHomeDir()
		spec.configDir = claudeConfigDir(home)
	}
	if client == "claude-code-project" {
		spec.scope, spec.scopePath = platform.MCPScopeProject, filepath.Dir(path)
	}
	return spec
}

// ReadMCPFile reads a single file with a top-level "mcpServers" object and
// returns its servers and the read outcome. loadsMCP states whether the client
// really loads the MCP servers of this file (see platform.MCPSource.LoadsMCP).
// See readMCPFile.
func ReadMCPFile(path, client string, loadsMCP bool) ([]platform.MCPServer, platform.MCPSource) {
	return readMCPFile(specFor(path, client, formatMCPServers, loadsMCP))
}

// ReadClaudeJSON reads Claude Code's ~/.claude.json: user-scope and
// local-scope servers only. See readMCPFile.
func ReadClaudeJSON(path string) ([]platform.MCPServer, platform.MCPSource) {
	return readMCPFile(specFor(path, "claude-code", formatClaudeJSON, true))
}

// readMCPFile reads one MCP config file and returns its servers and the read
// outcome for coverage reporting:
//   - missing file: no servers, status missing;
//   - file over 2 MB: not opened, status failed (size_limit_exceeded);
//   - unreadable or malformed file: no servers, status failed (logged);
//   - file read, including an empty file or one with no "mcpServers" block:
//     its servers (possibly none), status read — the client is present.
//
// Failure reasons use graph's "<code>: <detail>" form.
func readMCPFile(spec sourceSpec) ([]platform.MCPServer, platform.MCPSource) {
	path, client, format, loadsMCP := spec.path, spec.client, spec.format, spec.loadsMCP
	src := platform.MCPSource{Platform: client, ConfigPath: path, ConfigDir: spec.configDir, LoadsMCP: loadsMCP}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			src.Status = platform.MCPSourceMissing
			return nil, src
		}
		slog.Warn("mcp: could not read config", "path", path, "err", err)
		src.Status = platform.MCPSourceFailed
		src.Reason = graph.Reason(graph.ReasonReadError, err.Error())
		return nil, src
	}
	if info.Size() > MaxConfigBytes {
		slog.Warn("mcp: config over size limit, not opened", "path", path, "bytes", info.Size())
		src.Status = platform.MCPSourceFailed
		src.Reason = graph.Reason(graph.ReasonSizeLimitExceeded,
			fmt.Sprintf("%d bytes > %d (invariant 6)", info.Size(), MaxConfigBytes))
		return nil, src
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("mcp: could not read config", "path", path, "err", err)
		src.Status = platform.MCPSourceFailed
		src.Reason = graph.Reason(graph.ReasonReadError, err.Error())
		return nil, src
	}

	// An empty file declares nothing; it is not malformed.
	if len(strings.TrimSpace(string(raw))) == 0 {
		src.Status = platform.MCPSourceRead
		return nil, src
	}

	// pointer → entry, for every server in the file.
	entries := map[string]namedEntry{}
	switch format {
	case formatClaudeJSON:
		var cfg claudeJSONFile
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, parseFailed(src, path, err)
		}
		for name, e := range cfg.MCPServers {
			entries["/mcpServers/"+pointerEscape(name)] = namedEntry{name: name, entry: e}
		}
		for project, p := range cfg.Projects {
			for name, e := range p.MCPServers {
				entries["/projects/"+pointerEscape(project)+"/mcpServers/"+pointerEscape(name)] = namedEntry{name: name, entry: e, project: project}
			}
		}
	default:
		var cfg mcpSettingsFile
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, parseFailed(src, path, err)
		}
		for name, e := range cfg.MCPServers {
			entries["/mcpServers/"+pointerEscape(name)] = namedEntry{name: name, entry: e}
		}
	}
	src.Status = platform.MCPSourceRead

	// Claude Code entries are classified as remote or stdio (Cursor and Cline
	// keep their v0.1 handling).
	classify := strings.HasPrefix(client, "claude-code")

	servers := make([]platform.MCPServer, 0, len(entries))
	for pointer, ne := range entries {
		entry := ne.entry

		// EnvKeys: key names only, for the v1 scanner and baseline.
		envKeys := sortedKeys(entry.Env)

		// Env (and Headers): values wrapped as SecretValue (redacted on any
		// print or encoding) for the credential fingerprint. Never stored
		// (invariant §3).
		env := secretVars(entry.Env, envKeys)

		var headers []platform.EnvVar
		if classify && remoteTypes[entry.Type] {
			headers = secretVars(entry.Headers, sortedKeys(entry.Headers))
		}

		args := entry.Args
		if args == nil {
			args = []string{}
		}

		scope, scopePath := spec.scope, spec.scopePath
		if ne.project != "" {
			scope, scopePath = platform.MCPScopeLocal, ne.project
		}

		servers = append(servers, platform.MCPServer{
			Client:     client,
			Name:       ne.name,
			Command:    entry.Command,
			Args:       args,
			EnvKeys:    envKeys,
			Env:        env,
			Headers:    headers,
			ConfigPath: path,
			Pointer:    pointer,
			LoadsMCP:   loadsMCP,
			ConfigDir:  spec.configDir,
			Scope:      scope,
			ScopePath:  scopePath,
			EnvFile:    cursorEnvFile(client, entry.EnvFile, spec),
		})
	}
	sort.Slice(servers, func(i, j int) bool { return servers[i].Pointer < servers[j].Pointer })

	return servers, src
}

// cursorEnvFile resolves a Cursor server's envFile: an absolute path, a
// "~/" path, or "${workspaceFolder}/" in a repository's mcp.json. Any other
// form is not resolved (logged) and not read. Empty for other clients.
func cursorEnvFile(client, envFile string, spec sourceSpec) string {
	if client != "cursor" || strings.TrimSpace(envFile) == "" {
		return ""
	}
	const ws = "${workspaceFolder}"
	switch {
	case filepath.IsAbs(envFile):
		return filepath.Clean(envFile)
	case (strings.HasPrefix(envFile, "~/") || strings.HasPrefix(envFile, `~\`)) && spec.home != "":
		return filepath.Join(spec.home, envFile[2:])
	case strings.HasPrefix(envFile, ws) && spec.scopePath != "":
		return filepath.Join(spec.scopePath, strings.TrimLeft(envFile[len(ws):], `/\`))
	}
	slog.Info("mcp: cursor envFile not resolved", "path", spec.path, "env_file", envFile)
	return ""
}

// namedEntry is one server entry of a config file. project is the
// ~/.claude.json project path of a local-scope entry, empty otherwise.
type namedEntry struct {
	name    string
	entry   mcpServerEntry
	project string
}

func parseFailed(src platform.MCPSource, path string, err error) platform.MCPSource {
	slog.Warn("mcp: could not parse config", "path", path, "err", err)
	src.Status = platform.MCPSourceFailed
	src.Reason = graph.Reason(graph.ReasonParseError, err.Error())
	return src
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func secretVars(m map[string]string, keys []string) []platform.EnvVar {
	out := make([]platform.EnvVar, 0, len(keys))
	for _, k := range keys {
		out = append(out, platform.EnvVar{Key: k, Value: secrets.NewSecretValue(m[k])})
	}
	return out
}

var pointerEscaper = strings.NewReplacer("~", "~0", "/", "~1")

// pointerEscape escapes a JSON Pointer reference token (RFC 6901).
func pointerEscape(s string) string {
	return pointerEscaper.Replace(s)
}
