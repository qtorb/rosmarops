package windows

import (
	"encoding/json"
	"log/slog"
	"math"
	"os"
	"path/filepath"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-253 §B (CTO-66 §4.2, decision 2, signed 18-11): RosmarOps recognises its
// own Claude Code plugin. It reads only installed_plugins.json,
// known_marketplaces.json and, in the installed copy, hooks/hooks.json and
// .claude-plugin/plugin.json; of the copy it also checks, by metadata only,
// that it carries no bin/, .mcp.json or .lsp.json. Nothing else under the
// plugins folder is read: no other plugin, no data/. Only the recognition
// leaves this file.

// The plugin, fixed in the code.
const (
	ownPluginID          = "rosmarops@rosmarops"
	ownMarketplace       = "rosmarops"
	ownPluginName        = "rosmarops"
	ownMarketplaceSource = "github"
	ownMarketplaceRepo   = "qtorb/rosmarops-claude-code"
)

// pluginsDir is Claude Code's plugins folder: CLAUDE_CODE_PLUGIN_CACHE_DIR,
// else <configuration folder>/plugins (code.claude.com/docs/en/plugins,
// «Find plugins on disk»).
func pluginsDir(claudeDir string) string {
	if d := os.Getenv("CLAUDE_CODE_PLUGIN_CACHE_DIR"); d != "" {
		return d
	}
	return filepath.Join(claudeDir, "plugins")
}

// ownPluginEnabled is condition 1: enabledPlugins has
// "rosmarops@rosmarops": true in the user or managed settings.
func ownPluginEnabled(scan platform.ClaudeSettingsScan) bool {
	files := append([]platform.ClaudeSettingsFile{}, scan.Files...)
	for _, m := range scan.Managed {
		files = append(files, m.Files...)
	}
	for _, f := range files {
		if (f.Scope == platform.MCPScopeUser || f.Scope == platform.MCPScopeManaged) && f.OwnPluginEnabled {
			return true
		}
	}
	return false
}

// readJSON decodes a bounded file into v; false (with a warning, never an
// error that changes the run's status) when it is missing, unreadable or
// not JSON.
func readJSON(path string, v any) bool {
	raw, status, reason := readBounded(path)
	if status != platform.MCPSourceRead {
		if status == platform.MCPSourceFailed {
			slog.Warn("claude plugins: not read", "path", path, "reason", reason)
		}
		return false
	}
	if err := json.Unmarshal(raw, v); err != nil {
		slog.Warn("claude plugins: not JSON", "path", path)
		return false
	}
	return true
}

// ReadOwnPlugin reports conditions 2 to 6 of BV-253 §B.2 under dir, the
// plugins folder.
func ReadOwnPlugin(dir string) bool {
	// 2. The marketplace «rosmarops» comes from github qtorb/rosmarops-claude-code.
	var known map[string]struct {
		Source struct {
			Source string `json:"source"`
			Repo   string `json:"repo"`
		} `json:"source"`
	}
	if !readJSON(filepath.Join(dir, "known_marketplaces.json"), &known) {
		return false
	}
	m, ok := known[ownMarketplace]
	if !ok || m.Source.Source != ownMarketplaceSource || m.Source.Repo != ownMarketplaceRepo {
		return false
	}
	// 3. installed_plugins.json gives an installPath inside
	// cache/rosmarops/rosmarops/ (version 2: a list per plugin; version 1: one
	// entry).
	var installed struct {
		Plugins map[string]json.RawMessage `json:"plugins"`
	}
	if !readJSON(filepath.Join(dir, "installed_plugins.json"), &installed) {
		return false
	}
	type entry struct {
		InstallPath string `json:"installPath"`
	}
	var entries []entry
	if raw, ok := installed.Plugins[ownPluginID]; ok {
		if json.Unmarshal(raw, &entries) != nil {
			var one entry
			if json.Unmarshal(raw, &one) != nil {
				return false
			}
			entries = []entry{one}
		}
	}
	root := filepath.Join(dir, "cache", ownMarketplace, ownPluginName)
	for _, e := range entries {
		p := filepath.Clean(e.InstallPath)
		if e.InstallPath == "" || !filepath.IsAbs(p) || !graph.Within(root, p) || graph.CanonicalPath(root) == graph.CanonicalPath(p) {
			continue
		}
		if ownCopy(p) {
			return true
		}
	}
	return false
}

// ownCopy reports conditions 4 to 6 for the installed copy at p.
func ownCopy(p string) bool {
	// 5. No bin/, and no MCP or LSP servers file in the copy (metadata only).
	for _, name := range []string{"bin", ".mcp.json", ".lsp.json"} {
		if _, err := os.Lstat(filepath.Join(p, name)); err == nil {
			return false
		}
	}
	var manifest map[string]json.RawMessage
	if !readJSON(filepath.Join(p, ".claude-plugin", "plugin.json"), &manifest) {
		return false
	}
	for _, k := range []string{"mcpServers", "monitors", "lspServers", "hooks"} {
		if _, ok := manifest[k]; ok {
			return false
		}
	}
	// 4. hooks/hooks.json is exactly the one SessionStart hook of §A.3.
	var hooks map[string]json.RawMessage
	if !readJSON(filepath.Join(p, "hooks", "hooks.json"), &hooks) || !ownHooksFile(hooks) {
		return false
	}
	// 6. «rosmarops», resolved by the PATH, has the reference's SHA-256.
	return OwnBinary != nil && OwnBinary("rosmarops")
}

// ownHooksFile reports that a hooks.json document is exactly {"hooks":
// {"SessionStart": [{"hooks": [{"type": "command", "command": "rosmarops",
// "args": ["session-check"], "timeout": 1..10}]}]}}: no other key, event,
// group or handler, no matcher and no if.
func ownHooksFile(doc map[string]json.RawMessage) bool {
	if !onlyKeys(doc, "hooks") {
		return false
	}
	var events map[string]json.RawMessage
	if json.Unmarshal(doc["hooks"], &events) != nil || !onlyKeys(events, "SessionStart") {
		return false
	}
	var groups []map[string]json.RawMessage
	if json.Unmarshal(events["SessionStart"], &groups) != nil || len(groups) != 1 || !onlyKeys(groups[0], "hooks") {
		return false
	}
	var handlers []map[string]json.RawMessage
	if json.Unmarshal(groups[0]["hooks"], &handlers) != nil || len(handlers) != 1 {
		return false
	}
	h := handlers[0]
	if !onlyKeys(h, "type", "command", "args", "timeout") {
		return false
	}
	var typ, command string
	var args []string
	var timeout float64
	if json.Unmarshal(h["type"], &typ) != nil || json.Unmarshal(h["command"], &command) != nil ||
		json.Unmarshal(h["args"], &args) != nil || json.Unmarshal(h["timeout"], &timeout) != nil {
		return false
	}
	return typ == "command" && command == "rosmarops" && len(args) == 1 && args[0] == platform.OwnHookSessionCheck &&
		timeout >= 1 && timeout <= 10 && timeout == math.Trunc(timeout)
}

// onlyKeys reports that m has exactly the given keys.
func onlyKeys(m map[string]json.RawMessage, keys ...string) bool {
	if len(m) != len(keys) {
		return false
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return false
		}
	}
	return true
}
