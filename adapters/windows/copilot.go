package windows

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/jsonc"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// copilotPolicyKey is VS Code's enterprise policy key (CTO-16 Copilot §2,
// decision 9): HKLM wins over HKCU. Its values are the managed layer of Copilot.
const copilotPolicyKey = `Software\Policies\Microsoft\VSCode`

// ListCopilot reads GitHub Copilot's VS Code configuration (BV-140, CTO-16
// Copilot §2), read only: the user and profile settings.json, each repo's
// .vscode/settings.json, the Software\Policies\Microsoft\VSCode registry policy
// (HKLM over HKCU), the MCP files and the Copilot CLI credentials.
func (w *WindowsPlatform) ListCopilot(ctx context.Context, repoRoots []string) (platform.CopilotScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.CopilotScan{}, fmt.Errorf("resolve home dir: %w", err)
	}
	env := agentdirs.Env(os.LookupEnv)
	userDir := copilotVSCodeUserDir(w.layout, home, env)
	var reg RegistryReader
	if w.managed != nil {
		reg = w.managed.registry
	}
	return ReadCopilot(ctx, userDir, home, env, repoRoots, reg), nil
}

// copilotVSCodeUserDir is VS Code's user folder, as claude_settings resolves
// it: %APPDATA%\Code\User on Windows, the layout's path on macOS.
func copilotVSCodeUserDir(layout agentdirs.Layout, home string, env agentdirs.Env) string {
	if layout == agentdirs.MacOSLayout {
		return layout.VSCodeUserDir(home, nil)
	}
	appData, ok := env("APPDATA")
	if !ok || appData == "" {
		appData = filepath.Join(home, "AppData", "Roaming")
	}
	return filepath.Join(appData, "Code", "User")
}

// ReadCopilot is ListCopilot with the VS Code user folder, home, environment,
// repo roots and registry reader given, for tests. It is a pure function (the
// registry reader may be nil off Windows: the managed layer reads as absent).
func ReadCopilot(ctx context.Context, userDir, home string, env agentdirs.Env, repoRoots []string, reg RegistryReader) platform.CopilotScan {
	scan := platform.CopilotScan{ConfigDir: userDir}

	// 1. User and profile settings.json (JSONC).
	scan.Layers = append(scan.Layers, readCopilotSettings(filepath.Join(userDir, "settings.json"), platform.CopilotLayerUser, ""))
	for _, prof := range copilotProfiles(userDir) {
		scan.Layers = append(scan.Layers, readCopilotSettings(prof, platform.CopilotLayerProfile, ""))
	}

	// 2. Project .vscode/settings.json, one per repo root.
	for _, root := range repoRoots {
		scan.Layers = append(scan.Layers, readCopilotSettings(filepath.Join(root, ".vscode", "settings.json"), platform.CopilotLayerProject, root))
	}

	// 3. Managed policy layer: Software\Policies\Microsoft\VSCode, HKLM over
	// HKCU (CTO-16 Copilot §2). Absent off Windows (reg nil).
	if reg != nil {
		if l, ok := readCopilotPolicy(ctx, reg); ok {
			scan.Layers = append(scan.Layers, l)
		}
	}

	// 4. MCP files: the VS Code `servers` format (user-profile mcp.json and
	// <repo>\.vscode\mcp.json) and the portable Copilot `mcpServers` format
	// (~/.copilot/mcp-config.json, or COPILOT_HOME).
	scan.MCP = append(scan.MCP, readCopilotMCP(filepath.Join(userDir, "mcp.json"), "", copilotFormatServers))
	for _, root := range repoRoots {
		scan.MCP = append(scan.MCP, readCopilotMCP(filepath.Join(root, ".vscode", "mcp.json"), root, copilotFormatServers))
	}
	scan.MCP = append(scan.MCP, readCopilotMCP(filepath.Join(copilotHome(home, env), "mcp-config.json"), "", copilotFormatMCPServers))

	// 5. Copilot CLI / harness credentials: ~/.copilot/config.json, by
	// fingerprint only (CTO-16 Copilot §3).
	scan.Creds = copilotCreds(copilotHome(home, env))

	scan.Present = copilotPresent(scan, home, env)
	return scan
}

// copilotFormat is the JSON shape of an MCP file.
const (
	copilotFormatServers    = "servers"
	copilotFormatMCPServers = "mcpServers"
)

// copilotHome is $COPILOT_HOME, else ~/.copilot (CTO-16 Copilot §2: COPILOT_HOME
// replaces the folder, the two are not both read).
func copilotHome(home string, env agentdirs.Env) string {
	if v, ok := env("COPILOT_HOME"); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return filepath.Join(home, ".copilot")
}

// copilotProfiles lists <userDir>/profiles/<id>/settings.json for each profile
// folder (CTO-16 Copilot §2; a profile folder appears only once the profile
// changes something). Sorted for a stable order.
func copilotProfiles(userDir string) []string {
	entries, err := os.ReadDir(filepath.Join(userDir, "profiles"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, filepath.Join(userDir, "profiles", e.Name(), "settings.json"))
		}
	}
	sort.Strings(out)
	return out
}

// readCopilotSettings reads one settings.json layer as JSONC and extracts the
// chat.* keys that bear on posture (CTO-16 Copilot §4). VS Code stores settings
// as flat dotted keys, so each is a top-level entry.
func readCopilotSettings(path, kind, root string) platform.CopilotLayer {
	l := platform.CopilotLayer{Kind: kind, Path: path, Root: root}
	raw, status, reason := readBounded(path)
	l.Status, l.Reason = status, reason
	if status != platform.MCPSourceRead {
		return l
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(jsonc.Strip(raw), &keys); err != nil {
		l.Status, l.Reason = platform.MCPSourceFailed, graph.Reason(graph.ReasonParseError, err.Error())
		return l
	}
	l.GlobalAutoApprove = copilotBoolKey(keys, "chat.tools.global.autoApprove")
	l.TerminalEnableAutoApprove = copilotBoolKey(keys, "chat.tools.terminal.enableAutoApprove")
	l.TerminalIgnoreDefaults = copilotBoolKey(keys, "chat.tools.terminal.ignoreDefaultAutoApproveRules")
	l.AgentEnabled = copilotBoolKey(keys, "chat.agent.enabled")
	if all, ok := copilotTerminalCatchAll(keys["chat.tools.terminal.autoApprove"]); ok {
		l.TerminalAutoApproveAll = &all
	}
	if protect, ok := copilotEditsProtect(keys["chat.tools.edits.autoApprove"]); ok {
		l.EditsProtect = &protect
	}
	if s, ok := copilotStringKey(keys, "chat.permissions.default"); ok {
		l.PermissionsDefault = s
	}
	if dc, ok := keys["chat.defaultConfiguration"]; ok {
		var cfg struct {
			Mode      string `json:"mode"`
			Approvals string `json:"approvals"`
		}
		if json.Unmarshal(dc, &cfg) == nil {
			l.Mode, l.Approvals = cfg.Mode, cfg.Approvals
		}
	}
	return l
}

// copilotBoolKey returns a pointer to the bool value of key, or nil if absent
// or not a bool.
func copilotBoolKey(keys map[string]json.RawMessage, key string) *bool {
	raw, ok := keys[key]
	if !ok {
		return nil
	}
	var b bool
	if json.Unmarshal(raw, &b) != nil {
		return nil
	}
	return &b
}

// copilotStringKey returns the string value of key.
func copilotStringKey(keys map[string]json.RawMessage, key string) (string, bool) {
	raw, ok := keys[key]
	if !ok {
		return "", false
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// copilotTerminalCatchAll reports whether chat.tools.terminal.autoApprove has a
// catch-all rule set to true: a regex that matches everything (/.*/ or /.+/) or
// the literal "*" (CTO-16 Copilot §7 case 3). A map value of true auto-approves;
// an object {approve:true} does too.
func copilotTerminalCatchAll(raw json.RawMessage) (bool, bool) {
	if len(raw) == 0 {
		return false, false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false, false
	}
	for k, v := range m {
		if !copilotRuleCatchAll(k) {
			continue
		}
		if copilotRuleApproves(v) {
			return true, true
		}
	}
	return false, true
}

// copilotRuleCatchAll reports a terminal-autoApprove key that matches every
// command: a /regex/ whose body is .* or .+, or the literal "*".
func copilotRuleCatchAll(key string) bool {
	if key == "*" {
		return true
	}
	if len(key) >= 2 && strings.HasPrefix(key, "/") {
		body := key[1:]
		if i := strings.LastIndex(body, "/"); i >= 0 {
			body = body[:i] // drop trailing flags
		}
		return body == ".*" || body == ".+"
	}
	return false
}

// copilotRuleApproves reports a terminal-autoApprove value that approves: the
// boolean true, or an object {"approve": true}.
func copilotRuleApproves(raw json.RawMessage) bool {
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b
	}
	var obj struct {
		Approve bool `json:"approve"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Approve
	}
	return false
}

// copilotEditsProtect reports whether chat.tools.edits.autoApprove is a
// non-empty map: the default {} protects nothing (edits apply without asking);
// a non-empty map lists files that require approval (CTO-16 Copilot §4).
func copilotEditsProtect(raw json.RawMessage) (bool, bool) {
	if len(raw) == 0 {
		return false, false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil {
		return false, false
	}
	return len(m) > 0, true
}

// readCopilotPolicy reads the Software\Policies\Microsoft\VSCode registry
// policy, HKLM over HKCU (machine wins), into one managed layer. ok is false
// when no policy value is set in either hive.
func readCopilotPolicy(ctx context.Context, reg RegistryReader) (platform.CopilotLayer, bool) {
	l := platform.CopilotLayer{Kind: platform.CopilotLayerManaged, Path: "registry:" + copilotPolicyKey}
	set := false
	// HKCU first, then HKLM overrides (machine wins).
	for _, hive := range []string{HiveHKCU, HiveHKLM} {
		if v, ok, err := reg.Uint32Value(ctx, hive, copilotPolicyKey, "ChatToolsAutoApprove"); err == nil && ok {
			b := v != 0
			l.GlobalAutoApprove, set = &b, true
		}
		if v, ok, err := reg.Uint32Value(ctx, hive, copilotPolicyKey, "ChatAgentMode"); err == nil && ok {
			b := v != 0
			l.AgentEnabled, set = &b, true
		}
		if v, ok, err := reg.Uint32Value(ctx, hive, copilotPolicyKey, "ChatToolsTerminalEnableAutoApprove"); err == nil && ok {
			b := v != 0
			l.TerminalEnableAutoApprove, set = &b, true
		}
		if s, ok, err := reg.StringValue(ctx, hive, copilotPolicyKey, "ChatDefaultConfiguration"); err == nil && ok {
			var cfg struct {
				Mode      string `json:"mode"`
				Approvals string `json:"approvals"`
			}
			if json.Unmarshal([]byte(s), &cfg) == nil {
				l.Mode, l.Approvals, set = cfg.Mode, cfg.Approvals, true
			}
		}
	}
	return l, set
}

// readCopilotMCP reads one MCP file in the given format (servers or mcpServers),
// with the secret env and headers by fingerprint only.
func readCopilotMCP(path, root, format string) platform.CopilotMCPSource {
	src := platform.CopilotMCPSource{Path: path, Root: root, Format: format}
	raw, status, reason := readBounded(path)
	src.Status, src.Reason = status, reason
	if status != platform.MCPSourceRead {
		return src
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(jsonc.Strip(raw), &doc); err != nil {
		src.Status, src.Reason = platform.MCPSourceFailed, graph.Reason(graph.ReasonParseError, err.Error())
		return src
	}
	servers := map[string]json.RawMessage{}
	if err := json.Unmarshal(doc[format], &servers); err != nil {
		return src
	}
	ids := make([]string, 0, len(servers))
	for id := range servers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		var s struct {
			Command   string            `json:"command"`
			Args      []string          `json:"args"`
			URL       string            `json:"url"`
			ServerURL string            `json:"serverUrl"`
			Env       map[string]string `json:"env"`
			Headers   map[string]string `json:"headers"`
		}
		if json.Unmarshal(servers[id], &s) != nil {
			continue
		}
		url := s.URL
		if url == "" {
			url = s.ServerURL
		}
		server := platform.CopilotMCPServer{ID: id, Command: s.Command, Args: s.Args, URL: url}
		server.EnvKeys, server.Env = copilotSecretMap(s.Env)
		server.HeaderKeys, server.Headers = copilotSecretMap(s.Headers)
		src.Servers = append(src.Servers, server)
	}
	return src
}

// copilotSecretMap turns an env/headers object into sorted key names and their
// secret values (never the raw value).
func copilotSecretMap(m map[string]string) ([]string, []platform.EnvVar) {
	if len(m) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	vars := make([]platform.EnvVar, 0, len(keys))
	for _, k := range keys {
		vars = append(vars, platform.EnvVar{Key: k, Value: secrets.NewSecretValue(m[k])})
	}
	return keys, vars
}

// copilotCreds reads ~/.copilot/config.json by fingerprint only: every non-empty
// string leaf value is a credential (CTO-16 Copilot §3, Copilot CLI token kept
// in clear only when there is no OS keychain).
func copilotCreds(copilotDir string) []platform.EnvVar {
	raw, status, _ := readBounded(filepath.Join(copilotDir, "config.json"))
	if status != platform.MCPSourceRead {
		return nil
	}
	var doc any
	if json.Unmarshal(raw, &doc) != nil {
		return nil
	}
	keys := map[string]string{}
	copilotWalkJSON("", doc, keys)
	names := make([]string, 0, len(keys))
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	out := make([]platform.EnvVar, 0, len(names))
	for _, k := range names {
		out = append(out, platform.EnvVar{Key: "config.json:" + k, Value: secrets.NewSecretValue(keys[k])})
	}
	return out
}

// copilotWalkJSON collects non-empty string leaf values keyed by dotted path.
func copilotWalkJSON(prefix string, v any, out map[string]string) {
	switch t := v.(type) {
	case string:
		if strings.TrimSpace(t) != "" {
			out[prefix] = t
		}
	case map[string]any:
		for k, val := range t {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			copilotWalkJSON(p, val, out)
		}
	}
}

// copilotPresent reports Copilot is configured on the machine: a chat.* key set
// in any layer, an MCP file read, a policy value, or the Copilot CLI folder
// exists. The VS Code extension, read separately, is the other presence signal
// (handled by the graph builder).
func copilotPresent(scan platform.CopilotScan, home string, env agentdirs.Env) bool {
	for _, l := range scan.Layers {
		if copilotLayerHasKey(l) {
			return true
		}
	}
	for _, s := range scan.MCP {
		if s.Status == platform.MCPSourceRead {
			return true
		}
	}
	if len(scan.Creds) > 0 {
		return true
	}
	if _, err := os.Stat(copilotHome(home, env)); err == nil {
		return true
	}
	// BV-203 A1 (CTO-25): the bundled Copilot Chat extension, even with default
	// settings and no ~/.copilot, leaves a per-extension folder under VS Code's
	// globalStorage once it has run — <VS Code User>\globalStorage\
	// github.copilot-chat (and github.copilot). Its presence is a detection
	// signal. Only the folder's existence is checked; its state.vscdb and chat
	// sessions are a VS Code database this does not read (that would be a new
	// source). The VS Code User dir resolves per OS through the layout
	// (%APPDATA%\Code\User, ~/Library/Application Support/Code/User,
	// ~/.config/Code/User).
	if copilotGlobalStoragePresent(scan.ConfigDir) {
		return true
	}
	return false
}

// copilotGlobalStoragePresent reports a Copilot Chat globalStorage folder under
// the VS Code user dir (BV-203 A1). Existence only — never a read of its
// contents.
func copilotGlobalStoragePresent(vscodeUserDir string) bool {
	if vscodeUserDir == "" {
		return false
	}
	for _, ext := range []string{"github.copilot-chat", "github.copilot"} {
		if fi, err := os.Stat(filepath.Join(vscodeUserDir, "globalStorage", ext)); err == nil && fi.IsDir() {
			return true
		}
	}
	return false
}

// copilotLayerHasKey reports a layer that sets at least one posture key.
func copilotLayerHasKey(l platform.CopilotLayer) bool {
	return l.GlobalAutoApprove != nil || l.TerminalEnableAutoApprove != nil ||
		l.TerminalIgnoreDefaults != nil || l.TerminalAutoApproveAll != nil ||
		l.EditsProtect != nil || l.AgentEnabled != nil ||
		l.PermissionsDefault != "" || l.Mode != "" || l.Approvals != ""
}
