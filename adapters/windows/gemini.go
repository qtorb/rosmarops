package windows

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/jsonc"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
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

	scan.Present = geminiPresent(dir)
	return scan
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
	return l
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
