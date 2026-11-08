package windows

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/jsonc"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
	"github.com/qtorb/rosmarops/internal/tomlsubset"
)

// ListGemini reads Google Gemini CLI's settings layers (BV-193, CTO-22 Gemini
// §2), read only: the system-defaults, user, project and system settings.json
// (JSONC), ~/.gemini/trustedFolders.json, and each layer's mcpServers. Its
// credentials by fingerprint come through credfiles, not here.
func (w *WindowsPlatform) ListGemini(_ context.Context, repoRoots []string) (platform.GeminiScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.GeminiScan{}, fmt.Errorf("resolve home dir: %w", err)
	}
	env := agentdirs.Env(os.LookupEnv)
	sysSettings, sysDefaults := geminiSystemPaths(w.layout, env)
	return ReadGemini(home, env, repoRoots, sysSettings, sysDefaults), nil
}

// geminiSystemPaths resolves the system settings and system-defaults paths
// (CTO-22 Gemini §2): the GEMINI_CLI_SYSTEM_SETTINGS_PATH /
// GEMINI_CLI_SYSTEM_DEFAULTS_PATH overrides, else the per-OS folder. Off
// Windows those folders do not exist, so the layers read as missing.
func geminiSystemPaths(layout agentdirs.Layout, env agentdirs.Env) (settings, defaults string) {
	dir := `C:\ProgramData\gemini-cli`
	switch layout {
	case agentdirs.MacOSLayout:
		dir = "/Library/Application Support/GeminiCli"
	case agentdirs.LinuxLayout:
		dir = "/etc/gemini-cli"
	default:
		if v, ok := env("ProgramData"); ok && v != "" {
			dir = filepath.Join(v, "gemini-cli")
		}
	}
	settings = filepath.Join(dir, "settings.json")
	defaults = filepath.Join(dir, "system-defaults.json")
	if v, ok := env("GEMINI_CLI_SYSTEM_SETTINGS_PATH"); ok && v != "" {
		settings = v
	}
	if v, ok := env("GEMINI_CLI_SYSTEM_DEFAULTS_PATH"); ok && v != "" {
		defaults = v
	}
	return settings, defaults
}

// ReadGemini is ListGemini with the home folder, environment, repo roots and
// the system file paths given, for tests. It is a pure function. GEMINI_CLI_HOME
// overrides where <home>/.gemini is read from (paths.ts:22-27).
func ReadGemini(home string, env agentdirs.Env, repoRoots []string, systemSettings, systemDefaults string) platform.GeminiScan {
	base := home
	if v, ok := env("GEMINI_CLI_HOME"); ok && v != "" {
		base = v
	}
	dir := agentdirs.AntigravityDir(base) // ~/.gemini, shared with Antigravity
	scan := platform.GeminiScan{ConfigDir: dir, FolderTrustEnabled: true}

	// Layers, in precedence order (system-defaults < user < project < system).
	scan.Layers = append(scan.Layers, readGeminiLayer(systemDefaults, platform.GeminiLayerSystemDefaults, ""))
	scan.Layers = append(scan.Layers, readGeminiLayer(filepath.Join(dir, "settings.json"), platform.GeminiLayerUser, ""))
	for _, root := range repoRoots {
		scan.Layers = append(scan.Layers, readGeminiLayer(filepath.Join(root, ".gemini", "settings.json"), platform.GeminiLayerProject, root))
	}
	scan.Layers = append(scan.Layers, readGeminiLayer(systemSettings, platform.GeminiLayerSystem, ""))

	// security.folderTrust.enabled: the winning layer sets it (default true).
	for _, kind := range []string{platform.GeminiLayerSystem, platform.GeminiLayerProject, platform.GeminiLayerUser, platform.GeminiLayerSystemDefaults} {
		if l, ok := geminiLayerByKind(scan.Layers, kind); ok && l.FolderTrust != nil {
			scan.FolderTrustEnabled = *l.FolderTrust
			break
		}
	}

	// trustedFolders.json (GEMINI_CLI_TRUSTED_FOLDERS_PATH overrides).
	tfPath := filepath.Join(dir, "trustedFolders.json")
	if v, ok := env("GEMINI_CLI_TRUSTED_FOLDERS_PATH"); ok && v != "" {
		tfPath = v
	}
	scan.TrustedFolders, scan.TrustedFoldersStatus = readGeminiTrustedFolders(tfPath)

	// Extensions (CTO-22 Gemini §5) and the admin/user policy tiers (§6).
	scan.Extensions = readGeminiExtensions(filepath.Join(dir, "extensions"))
	scan.Policies = readGeminiPolicies(systemSettings, dir)

	// The .env credential locations Gemini does not load (CTO-22 Gemini §3).
	scan.EnvDenied = geminiEnvDenied(repoRoots, scan)

	scan.Present = geminiPresent(dir)
	return scan
}

// geminiEnvAllowedVars are the .env variables Gemini loads even in a folder
// that is not trusted (CTO-22 Gemini §3).
var geminiEnvAllowedVars = map[string]bool{
	"GEMINI_API_KEY": true, "GOOGLE_API_KEY": true,
	"GOOGLE_CLOUD_PROJECT": true, "GOOGLE_CLOUD_LOCATION": true,
}

// geminiEnvDenied returns the ".env" locations ("<path>:<line>") Gemini CLI
// does not load (CTO-22 Gemini §3): per repo root, the winner among
// <root>\.gemini\.env and <root>\.env is loaded (fully when the root is a
// trusted folder, else only the four allowed variables); any other .env of the
// chain at that root is not loaded. The home .env is never denied.
func geminiEnvDenied(repoRoots []string, scan platform.GeminiScan) []string {
	var denied []string
	for _, root := range repoRoots {
		candidates := []string{filepath.Join(root, ".gemini", ".env"), filepath.Join(root, ".env")}
		winner := ""
		for _, c := range candidates {
			if fileExists(c) {
				winner = c
				break
			}
		}
		if winner == "" {
			continue
		}
		trusted := graph.GeminiTrusted(scan, root)
		for _, c := range candidates {
			if !fileExists(c) {
				continue
			}
			for _, line := range geminiEnvVarLines(c) {
				switch {
				case c != winner: // not the winning .env of the chain: not loaded
					denied = append(denied, line.loc)
				case !trusted && !geminiEnvAllowedVars[line.key]: // untrusted root: only the four
					denied = append(denied, line.loc)
				}
			}
		}
	}
	sort.Strings(denied)
	return denied
}

type geminiEnvLine struct {
	key string
	loc string // "<path>:<line>"
}

// geminiEnvVarLines reads a .env file and returns its KEY=VALUE lines with the
// same 1-based physical line numbering credfiles uses, so the locations match
// the credential nodes' Locations.
func geminiEnvVarLines(path string) []geminiEnvLine {
	raw, status, _ := readBounded(path)
	if status != platform.MCPSourceRead {
		return nil
	}
	var out []geminiEnvLine
	n := 0
	for _, line := range strings.Split(string(raw), "\n") {
		n++
		t := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		eq := strings.IndexByte(t, '=')
		if eq <= 0 {
			continue
		}
		key := strings.TrimSpace(strings.TrimPrefix(t[:eq], "export "))
		out = append(out, geminiEnvLine{key: key, loc: path + ":" + strconv.Itoa(n)})
	}
	return out
}

// readGeminiExtensions reads ~/.gemini/extensions/<n>/gemini-extension.json
// (CTO-22 Gemini §5): each extension's mcpServers, contextFileName, version and
// hooks, whether it ships a skills/ directory, and its policies/*.toml rules.
func readGeminiExtensions(extDir string) []platform.GeminiExtension {
	entries, err := os.ReadDir(extDir)
	if err != nil {
		return nil
	}
	var out []platform.GeminiExtension
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(extDir, e.Name())
		cfg := filepath.Join(dir, "gemini-extension.json")
		ext := platform.GeminiExtension{Name: e.Name(), Dir: dir, ConfigPath: cfg}
		raw, status, reason := readBounded(cfg)
		ext.Status, ext.Reason = status, reason
		if status == platform.MCPSourceRead {
			var doc struct {
				Name       string                       `json:"name"`
				Version    string                       `json:"version"`
				MCPServers map[string]geminiServerEntry `json:"mcpServers"`
				Context    json.RawMessage              `json:"contextFileName"`
				Hooks      json.RawMessage              `json:"hooks"`
			}
			if json.Unmarshal(jsonc.Strip(raw), &doc) != nil {
				ext.Status, ext.Reason = platform.MCPSourceFailed, graph.Reason(graph.ReasonParseError, "gemini-extension.json")
			} else {
				if doc.Name != "" {
					ext.Name = doc.Name
				}
				ext.Version = doc.Version
				ext.MCP = geminiServers(doc.MCPServers)
				ext.ContextFileNames = geminiContextNames(doc.Context)
				ext.Hooks, _ = geminiHooks(doc.Hooks, cfg, "")
			}
		}
		ext.HasSkills = isDirExists(filepath.Join(dir, "skills"))
		ext.Policies = readGeminiPolicyDir(filepath.Join(dir, "policies"))
		out = append(out, ext)
	}
	return out
}

// readGeminiPolicies reads the admin (<system settings folder>/policies) and
// user (~/.gemini/policies) policy tiers (CTO-22 Gemini §6). Project policies
// are disabled in the CLI, so they are not read.
func readGeminiPolicies(systemSettings, userDir string) []platform.GeminiPolicyLayer {
	var out []platform.GeminiPolicyLayer
	adminDir := filepath.Join(filepath.Dir(systemSettings), "policies")
	for _, l := range []struct {
		kind, dir string
	}{
		{platform.GeminiPolicyAdmin, adminDir},
		{platform.GeminiPolicyUser, filepath.Join(userDir, "policies")},
	} {
		if !isDirExists(l.dir) {
			continue
		}
		out = append(out, platform.GeminiPolicyLayer{Kind: l.kind, Dir: l.dir, Status: platform.MCPSourceRead, Rules: readGeminiPolicyDir(l.dir)})
	}
	return out
}

// readGeminiPolicyDir reads every *.toml of a policies directory into its rules
// (toml-loader.ts): [[rules]] with toolName, decision and commandPrefix.
func readGeminiPolicyDir(dir string) []platform.GeminiPolicyRule {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".toml") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	var rules []platform.GeminiPolicyRule
	for _, name := range names {
		path := filepath.Join(dir, name)
		raw, status, _ := readBounded(path)
		if status != platform.MCPSourceRead {
			continue
		}
		m, err := tomlsubset.Parse(raw)
		if err != nil {
			continue
		}
		arr, _ := m["rules"].(tomlsubset.Array)
		for _, r := range arr {
			rm, ok := r.(tomlsubset.Map)
			if !ok {
				continue
			}
			rule := platform.GeminiPolicyRule{Path: path}
			rule.ToolNames = tomlStrings(rm["toolName"])
			rule.CommandPrefix = tomlStrings(rm["commandPrefix"])
			if d, ok := rm["decision"].(string); ok {
				rule.Decision = d
			}
			if len(rule.ToolNames) > 0 && rule.Decision != "" {
				rules = append(rules, rule)
			}
		}
	}
	return rules
}

// tomlStrings reads a TOML value that is a string or an array of strings.
func tomlStrings(v any) []string {
	switch t := v.(type) {
	case string:
		return []string{t}
	case tomlsubset.Array:
		var out []string
		for _, e := range t {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// geminiPresent reports Gemini CLI's own signals in ~/.gemini (CTO-22 Gemini
// §6): settings.json, oauth_creds.json, google_accounts.json,
// trustedFolders.json or the tmp folder. All are exclusive to the CLI.
func geminiPresent(dir string) bool {
	for _, f := range []string{"settings.json", "oauth_creds.json", "google_accounts.json", "trustedFolders.json"} {
		if fileExists(filepath.Join(dir, f)) {
			return true
		}
	}
	return isDirExists(filepath.Join(dir, "tmp"))
}

func geminiLayerByKind(layers []platform.GeminiLayer, kind string) (platform.GeminiLayer, bool) {
	for _, l := range layers {
		if l.Kind == kind {
			return l, true
		}
	}
	return platform.GeminiLayer{}, false
}

// geminiServerEntry is one mcpServers entry.
type geminiServerEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	URL     string            `json:"url"`
	HTTPURL string            `json:"httpUrl"`
	Trust   bool              `json:"trust"`
	Env     map[string]string `json:"env"`
	Headers map[string]string `json:"headers"`
}

// readGeminiLayer reads one settings.json layer (JSONC), bounded and lenient
// (unknown keys ignored). A parse error marks the layer failed.
func readGeminiLayer(path, kind, root string) platform.GeminiLayer {
	l := platform.GeminiLayer{Kind: kind, Path: path, Root: root}
	raw, status, reason := readBounded(path)
	l.Status, l.Reason = status, reason
	if status != platform.MCPSourceRead {
		return l
	}
	var doc struct {
		General struct {
			DefaultApprovalMode string `json:"defaultApprovalMode"`
		} `json:"general"`
		Tools struct {
			Allowed []string `json:"allowed"`
		} `json:"tools"`
		Security struct {
			DisableYoloMode bool `json:"disableYoloMode"`
			FolderTrust     struct {
				Enabled *bool `json:"enabled"`
			} `json:"folderTrust"`
		} `json:"security"`
		Context struct {
			FileName json.RawMessage `json:"fileName"`
		} `json:"context"`
		MCPServers map[string]geminiServerEntry `json:"mcpServers"`
		Hooks      json.RawMessage              `json:"hooks"`
	}
	if err := json.Unmarshal(jsonc.Strip(raw), &doc); err != nil {
		l.Status, l.Reason = platform.MCPSourceFailed, graph.Reason(graph.ReasonParseError, err.Error())
		return l
	}
	l.DefaultApprovalMode = doc.General.DefaultApprovalMode
	l.ToolsAllowed = doc.Tools.Allowed
	l.DisableYoloMode = doc.Security.DisableYoloMode
	l.ContextFileNames = geminiContextNames(doc.Context.FileName)
	l.MCP = geminiServers(doc.MCPServers)
	l.FolderTrust = doc.Security.FolderTrust.Enabled
	l.Hooks, l.HooksDisabled = geminiHooks(doc.Hooks, path, root)
	return l
}

// geminiHooksDoc is the hooks object of a settings.json or gemini-extension.json
// (core/hooks/types.ts): hooks.enabled, and one array of hook definitions per
// event name. HOOKS_CONFIG_FIELDS (enabled, disabled, notifications) are not
// event names.
var geminiHooksNonEvents = map[string]bool{"enabled": true, "disabled": true, "notifications": true}

// geminiHooks reads the hooks object: the command of each hooks.<Event>[].hooks[]
// of type "command", and whether hooks.enabled is false. path is the file that
// defines them, root the repo root for a project layer (CTO-22 Gemini §5/§6).
func geminiHooks(raw json.RawMessage, path, root string) (hooks []platform.GeminiHook, disabled bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return nil, false
	}
	if en, ok := obj["enabled"]; ok {
		var b bool
		if json.Unmarshal(en, &b) == nil && !b {
			disabled = true
		}
	}
	events := make([]string, 0, len(obj))
	for k := range obj {
		if !geminiHooksNonEvents[k] {
			events = append(events, k)
		}
	}
	sort.Strings(events)
	for _, ev := range events {
		var defs []struct {
			Hooks []struct {
				Type    string `json:"type"`
				Command string `json:"command"`
			} `json:"hooks"`
		}
		if json.Unmarshal(obj[ev], &defs) != nil {
			continue
		}
		for _, d := range defs {
			for _, h := range d.Hooks {
				if h.Type == "command" && h.Command != "" {
					gh := platform.GeminiHook{Event: ev, Command: h.Command, Path: path, Root: root}
					if root != "" { // a project hook that names a repo script (BV-107 §2)
						for _, tok := range splitCommand(h.Command) {
							if rel, size, ok := repoScriptCandidate(tok, root); ok {
								gh.ScriptPath, gh.ScriptSize = rel, size
								break
							}
						}
					}
					hooks = append(hooks, gh)
				}
			}
		}
	}
	return hooks, disabled
}

// geminiContextNames reads context.fileName, which is a single string or a
// list of strings (settingsSchema.ts:1413).
func geminiContextNames(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var one string
	if json.Unmarshal(raw, &one) == nil && one != "" {
		return []string{one}
	}
	var many []string
	if json.Unmarshal(raw, &many) == nil {
		return many
	}
	return nil
}

func geminiServers(m map[string]geminiServerEntry) []platform.GeminiMCPServer {
	if len(m) == 0 {
		return nil
	}
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []platform.GeminiMCPServer
	for _, id := range ids {
		e := m[id]
		s := platform.GeminiMCPServer{ID: id, Command: e.Command, Args: e.Args, Trust: e.Trust}
		s.URL = e.URL
		if s.URL == "" {
			s.URL = e.HTTPURL
		}
		s.EnvKeys, s.Env = geminiSecretMap(e.Env)
		s.HeaderKeys, s.Headers = geminiSecretMap(e.Headers)
		out = append(out, s)
	}
	return out
}

func geminiSecretMap(m map[string]string) (names []string, vars []platform.EnvVar) {
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		vars = append(vars, platform.EnvVar{Key: k, Value: secrets.NewSecretValue(m[k])})
	}
	return names, vars
}

// readGeminiTrustedFolders reads ~/.gemini/trustedFolders.json: an object of
// path → "TRUST_FOLDER" | "TRUST_PARENT" | "DO_NOT_TRUST" (trust.ts).
func readGeminiTrustedFolders(path string) (map[string]string, platform.MCPSourceStatus) {
	raw, status, _ := readBounded(path)
	if status != platform.MCPSourceRead {
		return nil, status
	}
	var m map[string]string
	if json.Unmarshal(jsonc.Strip(raw), &m) != nil {
		return nil, platform.MCPSourceFailed
	}
	return m, platform.MCPSourceRead
}
