package windows

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

// settingsFile is the only part of a Claude Code settings file RosmarOps
// decodes (https://code.claude.com/docs/en/settings-reference): permission
// keys, .mcp.json approvals, env, the commands Claude Code runs (hooks,
// statusLine, apiKeyHelper) and two managed-only keys. Any other key is
// discarded by json.Unmarshal. Command texts are read only to see that a
// command exists; they are never kept.
type settingsFile struct {
	Permissions struct {
		Allow                               []string `json:"allow"`
		Ask                                 []string `json:"ask"`
		Deny                                []string `json:"deny"`
		DefaultMode                         string   `json:"defaultMode"`
		DisableBypassPermissionsMode        string   `json:"disableBypassPermissionsMode"`
		DisableAutoMode                     string   `json:"disableAutoMode"`
		AdditionalDirectories               []string `json:"additionalDirectories"`
		BlockReadsOutsideWorkingDirectories bool     `json:"blockReadsOutsideWorkingDirectories"`
	} `json:"permissions"`
	DisableAutoMode string `json:"disableAutoMode"`

	EnabledMcpjsonServers      []string `json:"enabledMcpjsonServers"`
	DisabledMcpjsonServers     []string `json:"disabledMcpjsonServers"`
	EnableAllProjectMcpServers *bool    `json:"enableAllProjectMcpServers"`

	Env map[string]string `json:"env"`

	Hooks map[string][]struct {
		Hooks []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
		} `json:"hooks"`
	} `json:"hooks"`
	StatusLine *struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	} `json:"statusLine"`
	APIKeyHelper string `json:"apiKeyHelper"`

	AllowManagedPermissionRulesOnly bool   `json:"allowManagedPermissionRulesOnly"`
	ManagedSourcesBehavior          string `json:"managedSourcesBehavior"`
}

// controlKeys are the managed settings keys that are not policy keys
// (https://code.claude.com/docs/en/managed-settings#how-claude-code-combines-managed-sources).
var controlKeys = map[string]bool{"managedSourcesBehavior": true, "wslInheritsWindowsSettings": true}

// ListClaudeSettings implements platform.Platform.
//
// Files read (https://code.claude.com/docs/en/settings):
//   - user: <configuration folder>/settings.json, where the folder is
//     CLAUDE_CONFIG_DIR or ~/.claude (CLAUDE_CONFIG_DIR moves user settings,
//     https://code.claude.com/docs/en/env-vars);
//   - project: <repo>/.claude/settings.json;
//   - local: <repo>/.claude/settings.local.json;
//   - managed, on Windows only (see readManaged).
//
// Trust comes from ~/.claude.json, which stays in the home directory: the
// documentation does not say CLAUDE_CONFIG_DIR moves it.
func (w *WindowsPlatform) ListClaudeSettings(ctx context.Context, repoRoots []string) (platform.ClaudeSettingsScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.ClaudeSettingsScan{}, fmt.Errorf("resolve home dir: %w", err)
	}
	dir := claudeConfigDir(home)

	var scan platform.ClaudeSettingsScan
	scan.Files = append(scan.Files, ReadClaudeSettings(filepath.Join(dir, "settings.json"), platform.MCPScopeUser, "", dir))
	for _, root := range repoRoots {
		scan.Files = append(scan.Files,
			ReadClaudeSettings(filepath.Join(root, ".claude", "settings.json"), platform.MCPScopeProject, root, dir),
			ReadClaudeSettings(filepath.Join(root, ".claude", "settings.local.json"), platform.MCPScopeLocal, root, dir),
		)
	}
	scan.Trust = readProjectTrust(filepath.Join(home, ".claude.json"))
	appData := os.Getenv("APPDATA")
	if appData == "" {
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	scan.VSCode = readVSCodeMode(filepath.Join(appData, "Code", "User", "settings.json"), dir)
	if w.managed != nil {
		w.managed.read(ctx, dir, &scan)
	}
	return scan, nil
}

// read fills the managed sources of scan, highest priority first
// (https://code.claude.com/docs/en/managed-settings#how-claude-code-combines-managed-sources):
// the remote-settings cache, HKLM, the managed settings files, HKCU.
// Nothing is written.
func (m *managedLocations) read(ctx context.Context, configDir string, scan *platform.ClaudeSettingsScan) {
	managed := func(path string) platform.ClaudeSettingsFile {
		return ReadClaudeSettings(path, platform.MCPScopeManaged, "", configDir)
	}

	// 1. Cache of server-managed settings
	// (https://code.claude.com/docs/en/claude-directory: remote-settings.json).
	scan.Managed = append(scan.Managed, platform.ManagedSource{
		Kind: platform.ManagedRemote, Files: []platform.ClaudeSettingsFile{managed(filepath.Join(configDir, "remote-settings.json"))},
	})
	// 2. HKLM registry.
	scan.Managed = append(scan.Managed, platform.ManagedSource{
		Kind: platform.ManagedHKLM, Files: []platform.ClaudeSettingsFile{m.readRegistry(ctx, HiveHKLM, configDir)},
	})
	// 3. managed-settings.json, then managed-settings.d/*.json in
	// alphabetical order, hidden files and non-.json files ignored
	// (managed-settings, "Split a file-based policy across teams").
	files := []platform.ClaudeSettingsFile{managed(filepath.Join(m.dir, "managed-settings.json"))}
	dropIn := filepath.Join(m.dir, "managed-settings.d")
	entries, err := os.ReadDir(dropIn)
	switch {
	case err == nil:
		var names []string
		for _, e := range entries {
			if !e.IsDir() && !strings.HasPrefix(e.Name(), ".") && strings.HasSuffix(e.Name(), ".json") {
				names = append(names, e.Name())
			}
		}
		sort.Strings(names)
		for _, n := range names {
			files = append(files, managed(filepath.Join(dropIn, n)))
		}
	case !os.IsNotExist(err):
		// A managed-settings.d that exists but cannot be read is a present
		// admin document (managed-settings, "When an admin document counts
		// as present").
		files = append(files, platform.ClaudeSettingsFile{
			Scope: platform.MCPScopeManaged, ConfigPath: dropIn, ConfigDir: configDir,
			Status: platform.MCPSourceFailed, Reason: graph.Reason(graph.ReasonReadError, err.Error()),
		})
	}
	scan.Managed = append(scan.Managed, platform.ManagedSource{Kind: platform.ManagedFiles, Files: files})
	// 4. HKCU registry (user-writable).
	scan.Managed = append(scan.Managed, platform.ManagedSource{
		Kind: platform.ManagedHKCU, Files: []platform.ClaudeSettingsFile{m.readRegistry(ctx, HiveHKCU, configDir)},
	})

	mcp := managed(filepath.Join(m.dir, "managed-mcp.json"))
	mcp = platform.ClaudeSettingsFile{Scope: mcp.Scope, ConfigPath: mcp.ConfigPath, ConfigDir: mcp.ConfigDir, Status: mcp.Status, Reason: mcp.Reason}
	scan.ManagedMCP = &mcp

	// The legacy path is not read by Claude Code: existence only, never opened.
	if _, err := os.Lstat(m.legacy); err == nil {
		scan.LegacyManagedPath = m.legacy
	}
}

// readRegistry reads one hive's Settings value as a managed settings
// document, with path "registry:<hive>\SOFTWARE\Policies\ClaudeCode\Settings".
func (m *managedLocations) readRegistry(ctx context.Context, hive, configDir string) platform.ClaudeSettingsFile {
	f := platform.ClaudeSettingsFile{Scope: platform.MCPScopeManaged, ConfigPath: registryPath(hive), ConfigDir: configDir}
	value, exists, err := m.registry.PolicySettings(ctx, hive)
	switch {
	case err != nil:
		slog.Warn("claude settings: could not read registry", "path", f.ConfigPath, "err", err)
		f.Status, f.Reason = platform.MCPSourceFailed, graph.Reason(graph.ReasonReadError, err.Error())
		return f
	case !exists:
		f.Status = platform.MCPSourceMissing
		return f
	case len(value) > MaxConfigBytes:
		f.Status = platform.MCPSourceFailed
		f.Reason = graph.Reason(graph.ReasonSizeLimitExceeded, fmt.Sprintf("%d bytes > %d (invariant 6)", len(value), MaxConfigBytes))
		return f
	}
	f.Status = platform.MCPSourceRead
	return parseClaudeSettings(value, f)
}

// ReadClaudeSettings reads one Claude Code settings file. The outcome follows
// readMCPFile: missing, failed (read_error, parse_error,
// size_limit_exceeded; files over 2 MB are not opened) or read.
func ReadClaudeSettings(path, scope, scopePath, configDir string) platform.ClaudeSettingsFile {
	f := platform.ClaudeSettingsFile{Scope: scope, ScopePath: scopePath, ConfigPath: path, ConfigDir: configDir}
	raw, status, reason := readBounded(path)
	f.Status, f.Reason = status, reason
	if status != platform.MCPSourceRead {
		return f
	}
	return parseClaudeSettings(raw, f)
}

// parseClaudeSettings decodes a settings document into f.
func parseClaudeSettings(raw []byte, f platform.ClaudeSettingsFile) platform.ClaudeSettingsFile {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return f
	}
	var s settingsFile
	if err := json.Unmarshal(raw, &s); err != nil {
		slog.Warn("claude settings: could not parse", "path", f.ConfigPath, "err", err)
		f.Status = platform.MCPSourceFailed
		f.Reason = graph.Reason(graph.ReasonParseError, err.Error())
		return f
	}
	f.HasPolicyKey = hasPolicyKey(raw)

	p := s.Permissions
	f.Permissions = platform.ClaudePermissions{
		Allow:                               p.Allow,
		Ask:                                 p.Ask,
		Deny:                                p.Deny,
		DefaultMode:                         p.DefaultMode,
		DisableBypassPermissionsMode:        p.DisableBypassPermissionsMode,
		AdditionalDirectories:               p.AdditionalDirectories,
		BlockReadsOutsideWorkingDirectories: p.BlockReadsOutsideWorkingDirectories,
	}
	switch {
	case s.DisableAutoMode != "":
		f.Permissions.DisableAutoMode, f.Permissions.DisableAutoModePointer = s.DisableAutoMode, "/disableAutoMode"
	case p.DisableAutoMode != "":
		f.Permissions.DisableAutoMode, f.Permissions.DisableAutoModePointer = p.DisableAutoMode, "/permissions/disableAutoMode"
	}

	f.EnabledMcpjsonServers = s.EnabledMcpjsonServers
	f.DisabledMcpjsonServers = s.DisabledMcpjsonServers
	f.EnableAllProjectMcpServers = s.EnableAllProjectMcpServers
	f.Env = secretVars(s.Env, sortedKeys(s.Env))
	f.Executables = executablePointers(s)
	f.AllowManagedPermissionRulesOnly = s.AllowManagedPermissionRulesOnly
	f.ManagedSourcesBehavior = s.ManagedSourcesBehavior
	return f
}

// executablePointers lists the JSON Pointers of the commands a settings
// document makes Claude Code run: every "command" hook handler
// (/hooks/<event>/<group>/hooks/<handler>/command), statusLine.command and
// apiKeyHelper (https://code.claude.com/docs/en/settings-reference#hooks).
// Handlers of other types (prompt, agent, http, mcp_tool) run no command.
func executablePointers(s settingsFile) []string {
	var out []string
	events := make([]string, 0, len(s.Hooks))
	for e := range s.Hooks {
		events = append(events, e)
	}
	sort.Strings(events)
	for _, event := range events {
		for g, group := range s.Hooks[event] {
			for h, handler := range group.Hooks {
				if handler.Type == "command" && strings.TrimSpace(handler.Command) != "" {
					out = append(out, "/hooks/"+pointerEscape(event)+"/"+strconv.Itoa(g)+"/hooks/"+strconv.Itoa(h)+"/command")
				}
			}
		}
	}
	if s.StatusLine != nil && s.StatusLine.Type == "command" && strings.TrimSpace(s.StatusLine.Command) != "" {
		out = append(out, "/statusLine/command")
	}
	if strings.TrimSpace(s.APIKeyHelper) != "" {
		out = append(out, "/apiKeyHelper")
	}
	return out
}

// hasPolicyKey reports whether a document sets a policy key: a key other
// than the control keys, to a value other than null. Values are inspected
// only for null and then dropped.
func hasPolicyKey(raw []byte) bool {
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) != nil {
		return false
	}
	for k, v := range keys {
		if !controlKeys[k] && strings.TrimSpace(string(v)) != "null" {
			return true
		}
	}
	return false
}

// readProjectTrust returns every project entry of ~/.claude.json with its
// hasTrustDialogAccepted value, sorted by path. A missing, oversize or
// unreadable file yields no entries.
func readProjectTrust(path string) []platform.ProjectTrust {
	raw, status, _ := readBounded(path)
	if status != platform.MCPSourceRead || len(strings.TrimSpace(string(raw))) == 0 {
		return nil
	}
	var cfg claudeJSONFile
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}
	trust := make([]platform.ProjectTrust, 0, len(cfg.Projects))
	for p, entry := range cfg.Projects {
		trust = append(trust, platform.ProjectTrust{Path: p, Accepted: entry.HasTrustDialogAccepted})
	}
	sort.Slice(trust, func(i, j int) bool { return trust[i].Path < trust[j].Path })
	return trust
}

// readBounded reads a file of at most MaxConfigBytes (invariant 6). It
// returns the content, or the missing/failed status with a coded reason.
func readBounded(path string) ([]byte, platform.MCPSourceStatus, string) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, platform.MCPSourceMissing, ""
		}
		slog.Warn("claude settings: could not read", "path", path, "err", err)
		return nil, platform.MCPSourceFailed, graph.Reason(graph.ReasonReadError, err.Error())
	}
	if info.Size() > MaxConfigBytes {
		slog.Warn("claude settings: over size limit, not opened", "path", path, "bytes", info.Size())
		return nil, platform.MCPSourceFailed, graph.Reason(graph.ReasonSizeLimitExceeded,
			fmt.Sprintf("%d bytes > %d (invariant 6)", info.Size(), MaxConfigBytes))
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("claude settings: could not read", "path", path, "err", err)
		return nil, platform.MCPSourceFailed, graph.Reason(graph.ReasonReadError, err.Error())
	}
	return raw, platform.MCPSourceRead, ""
}

// vscodeModeKey is the VS Code setting of the Claude Code extension's
// starting permission mode, read from user settings only
// (https://code.claude.com/docs/en/vs-code: "VS Code reads
// initialPermissionMode from your user settings and ignores workspace
// values").
const vscodeModeKey = "claudeCode.initialPermissionMode"

// readVSCodeMode reads only claudeCode.initialPermissionMode from VS Code's
// user settings.json (decision 2026-10-24, D7); every other key is
// discarded. The file is decoded as strict JSON: VS Code allows comments and
// trailing commas, and such a file goes to coverage as parse_error. nil when
// the file does not exist.
func readVSCodeMode(path, configDir string) *platform.VSCodeModeSetting {
	raw, status, reason := readBounded(path)
	if status == platform.MCPSourceMissing {
		return nil
	}
	v := &platform.VSCodeModeSetting{Path: path, ConfigDir: configDir, Status: status, Reason: reason}
	if status != platform.MCPSourceRead {
		return v
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(raw, &keys); err != nil {
		v.Status, v.Reason = platform.MCPSourceFailed, graph.Reason(graph.ReasonParseError, err.Error())
		return v
	}
	if m, ok := keys[vscodeModeKey]; ok {
		var mode string
		if json.Unmarshal(m, &mode) == nil {
			v.Mode = mode
		}
	}
	return v
}
