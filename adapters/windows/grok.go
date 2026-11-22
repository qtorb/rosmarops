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

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/jsonkeys"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
	"github.com/qtorb/rosmarops/internal/tomlsubset"
)

// grokCompatVendors and grokCompatSurfaces are the [compat.<vendor>] cells:
// Grok Build reads the harness folders of two vendors, Cursor and Claude, on
// five surfaces, all on by default (docs.x.ai/build/settings/reference, read
// 22-11-2026: [compat.cursor] and [compat.claude] with skills, rules,
// agents, mcps and hooks).
var (
	grokCompatVendors  = []string{"cursor", "claude"}
	grokCompatSurfaces = []string{"skills", "rules", "agents", "mcps", "hooks"}
)

// grokSystemDir is where Grok Build reads the system's managed config and
// requirements on macOS and Linux (docs.x.ai/build/enterprise, read
// 22-11-2026). Grok does not read it on Windows.
const grokSystemDir = "/etc/grok"

// ListGrok reads Grok Build's configuration (BV-140, CTO-16 Grok §2), read
// only: its TOML layers in precedence order (the system's and the user's
// managed config, the user config.toml, the user's and the system's
// requirements, docs.x.ai/build/enterprise), each repo's .grok/config.toml
// (in force only when the folder is trusted), trusted_folders.toml, its own
// hooks files and sandbox.toml. The system's files in /etc/grok only on
// macOS and Linux; on Windows Grok reads neither /etc/grok nor Claude Code's
// managed-settings.json (paths.rs:22-41), and this reader does not either.
func (w *WindowsPlatform) ListGrok(ctx context.Context, repoRoots []string) (platform.GrokScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.GrokScan{}, fmt.Errorf("resolve home dir: %w", err)
	}
	system := ""
	if w.layout == agentdirs.MacOSLayout || w.layout == agentdirs.LinuxLayout {
		system = grokSystemDir
	}
	return ReadGrokWith(home, system, agentdirs.Env(os.LookupEnv), repoRoots), nil
}

// ReadGrok is ListGrok with the home folder, environment and repo roots given,
// and no system folder, for tests.
func ReadGrok(home string, env agentdirs.Env, repoRoots []string) platform.GrokScan {
	return ReadGrokWith(home, "", env, repoRoots)
}

// ReadGrokWith is ReadGrok with the system folder (/etc/grok, or "" for
// none) given.
func ReadGrokWith(home, system string, env agentdirs.Env, repoRoots []string) platform.GrokScan {
	dir := agentdirs.GrokHome(home, env)
	scan := platform.GrokScan{ConfigDir: dir}
	for _, name := range []string{"config.toml", "auth.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			scan.Exists = true
			break
		}
	}

	// 1. The layers, lowest precedence first (docs.x.ai/build/enterprise):
	// the system's managed config, the user's managed config, the user
	// config.toml (with the session credential files beside it), the user's
	// requirements and the system's requirements, which no lower layer can
	// override.
	layer := func(path, kind string, sys bool) platform.GrokLayer {
		l := readGrokLayer(path, kind, "", false)
		l.System = sys
		return l
	}
	var sysManaged, sysReq platform.GrokLayer
	if system != "" {
		sysManaged = layer(filepath.Join(system, "managed_config.toml"), platform.GrokLayerManaged, true)
		scan.Layers = append(scan.Layers, sysManaged)
	}
	managed := layer(filepath.Join(dir, "managed_config.toml"), platform.GrokLayerManaged, false)
	scan.Layers = append(scan.Layers, managed)
	user := layer(filepath.Join(dir, "config.toml"), platform.GrokLayerUser, false)
	user.SessionCreds = grokSessionCreds(dir)
	scan.Layers = append(scan.Layers, user)
	scan.Layers = append(scan.Layers, layer(filepath.Join(dir, "requirements.toml"), platform.GrokLayerRequirements, false))
	if system != "" {
		sysReq = layer(filepath.Join(system, "requirements.toml"), platform.GrokLayerRequirements, true)
		scan.Layers = append(scan.Layers, sysReq)
	}

	// Folder-trust control: on unless an exception turns it off (CTO-16 Grok §2).
	off := func(l platform.GrokLayer) bool { return l.FolderTrustEnabled != nil && !*l.FolderTrustEnabled }
	scan.FolderTrustOff = off(user) || off(managed) || off(sysManaged) || envFlagOff(env, "GROK_FOLDER_TRUST")
	scan.Trusted = readGrokTrustedFolders(dir)

	// 2. Project .grok/config.toml, one per repo root (only [mcp_servers],
	// [plugins], [permission] and [mcp]; in force only when trusted).
	trusted := map[string]bool{}
	for _, root := range repoRoots {
		l := readGrokLayer(filepath.Join(root, ".grok", "config.toml"), platform.GrokLayerProject, root, true)
		l.Trusted = grokRootTrusted(root, scan.Trusted, scan.FolderTrustOff, l.Status)
		scan.Layers = append(scan.Layers, l)
		// A project's hooks and sandbox.toml need an explicit trust like its
		// config.toml: a repository with no config.toml is not trusted for
		// them unless trusted_folders.toml or the control says so.
		trusted[root] = grokRootTrusted(root, scan.Trusted, scan.FolderTrustOff, platform.MCPSourceRead)
	}

	// 3. Grok's own hooks (docs.x.ai/build/features/hooks): the user's
	// hooks/*.json and each repository's .grok/hooks/*.json; a project's run
	// only once the folder is trusted.
	scan.Hooks = grokHookFiles(filepath.Join(dir, "hooks"), "", true)
	for _, root := range repoRoots {
		scan.Hooks = append(scan.Hooks, grokHookFiles(filepath.Join(root, ".grok", "hooks"), root, trusted[root])...)
	}

	// 4. sandbox.toml: the user's and each repository's profiles.
	scan.Sandboxes = append(scan.Sandboxes, readGrokSandbox(filepath.Join(dir, "sandbox.toml"), ""))
	for _, root := range repoRoots {
		scan.Sandboxes = append(scan.Sandboxes, readGrokSandbox(filepath.Join(root, ".grok", "sandbox.toml"), root))
	}
	return scan
}

// maxGrokHookFiles bounds the hook files read from one folder.
const maxGrokHookFiles = 100

// grokHookFiles reads the *.json files of one hooks folder: each must be
// JSON; RosmarOps keeps no key of it (a hook is an executable resource of
// Grok), and follows no link.
func grokHookFiles(dir, root string, trusted bool) []platform.GrokHookFile {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []platform.GrokHookFile
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.EqualFold(filepath.Ext(e.Name()), ".json") || len(out) >= maxGrokHookFiles {
			continue
		}
		path := filepath.Join(dir, e.Name())
		f := platform.GrokHookFile{Path: path, Root: root, Trusted: trusted}
		raw, status, reason := readBounded(path)
		f.Status, f.Reason = status, reason
		if status == platform.MCPSourceRead {
			var doc struct{}
			if _, err := jsonkeys.Decode(raw, &doc); err != nil {
				f.Status, f.Reason = platform.MCPSourceFailed, graph.Reason(graph.ReasonParseError, err.Error())
			}
		}
		slog.Info("grok: hooks file", "path", path, "status", f.Status)
		out = append(out, f)
	}
	return out
}

// readGrokSandbox reads a sandbox.toml: its [profiles.<name>] tables with
// extends, restrict_network, read_write and deny
// (docs.x.ai/build/features/sandbox, read 22-11-2026).
func readGrokSandbox(path, root string) platform.GrokSandboxFile {
	f := platform.GrokSandboxFile{Path: path, Root: root}
	raw, status, reason := readBounded(path)
	f.Status, f.Reason = status, reason
	if status != platform.MCPSourceRead {
		return f
	}
	m, err := tomlsubset.Parse(raw)
	if err != nil {
		f.Status, f.Reason = platform.MCPSourceFailed, graph.Reason(graph.ReasonParseError, err.Error())
		return f
	}
	profiles, _ := m["profiles"].(tomlsubset.Map)
	for name, v := range profiles {
		pm, ok := v.(tomlsubset.Map)
		if !ok {
			continue
		}
		p := platform.GrokSandboxProfile{Extends: "workspace", ReadWrite: tomlStringArray(pm, "read_write"), Deny: tomlStringArray(pm, "deny")}
		if s, ok := tomlString(pm, "extends"); ok && strings.TrimSpace(s) != "" {
			p.Extends = strings.TrimSpace(s)
		}
		if b, ok := tomlBool(pm, "restrict_network"); ok {
			p.RestrictNetwork = &b
		}
		if f.Profiles == nil {
			f.Profiles = map[string]platform.GrokSandboxProfile{}
		}
		f.Profiles[name] = p
	}
	return f
}

// grokRootTrusted reports whether Grok trusts a repo root (CTO-16 Grok §2): an
// explicit trusted_folders entry, the folder-trust control switched off, or a
// repo with no project configuration (read as missing) — the last is trusted
// because there is nothing to load.
func grokRootTrusted(root string, trusted []string, trustOff bool, status platform.MCPSourceStatus) bool {
	if trustOff || status == platform.MCPSourceMissing {
		return true
	}
	key := graph.CanonicalPath(root)
	for _, t := range trusted {
		if t == key {
			return true
		}
	}
	return false
}

// readGrokLayer reads one config layer, bounded and strict (invariant 6). A
// project layer keeps only [mcp_servers], [plugins], [permission] and [mcp]
// (CTO-16 Grok §2, layer 4); the user and managed layers keep every posture key.
func readGrokLayer(path, kind, root string, project bool) platform.GrokLayer {
	l := platform.GrokLayer{Kind: kind, Path: path, Root: root}
	raw, status, reason := readBounded(path)
	l.Status, l.Reason = status, reason
	if status != platform.MCPSourceRead {
		return l
	}
	m, err := tomlsubset.Parse(raw)
	if err != nil {
		l.Status, l.Reason = platform.MCPSourceFailed, graph.Reason(graph.ReasonParseError, err.Error())
		return l
	}
	l.MCP = grokMCPServers(m)
	if perm, ok := m["permission"].(tomlsubset.Map); ok {
		l.Allow = tomlStringArray(perm, "allow")
		l.Deny = tomlStringArray(perm, "deny")
		l.Ask = tomlStringArray(perm, "ask")
		l.Rules = grokRules(perm)
	}
	if project {
		// The project layer contributes nothing else (CTO-16 Grok §2); [ui],
		// [compat], credentials and folder-trust are user/managed only.
		return l
	}
	if ui, ok := m["ui"].(tomlsubset.Map); ok {
		if s, ok := tomlString(ui, "permission_mode"); ok {
			l.PermissionMode, l.HasPermissionMode = s, true
		}
		if b, ok := tomlBool(ui, "yolo"); ok {
			l.Yolo = b
		}
		if b, ok := tomlBool(ui, "disable_bypass_permissions_mode"); ok {
			l.DisableBypass = b
		}
	}
	// BV-232 §1: [sandbox] profile (docs.x.ai/build/features/sandbox).
	if sb, ok := m["sandbox"].(tomlsubset.Map); ok {
		if s, ok := tomlString(sb, "profile"); ok {
			l.SandboxProfile = strings.TrimSpace(s)
		}
	}
	l.Compat = grokCompatCells(m)
	if cc, ok := m["claude_compat"].(tomlsubset.Map); ok {
		if b, ok := tomlBool(cc, "imported"); ok {
			l.ClaudeImported = b
		}
	}
	if ft, ok := m["folder_trust"].(tomlsubset.Map); ok {
		if b, ok := tomlBool(ft, "enabled"); ok {
			l.FolderTrustEnabled = &b
		}
	}
	l.APIKeys = grokAPIKeys(m)
	return l
}

// grokCompatCells reads the [compat.<vendor>] cells that the file sets, as
// "<vendor>.<surface>" → enabled. Absent cells are decided elsewhere
// (compat_cell_remote_unknown).
func grokCompatCells(m tomlsubset.Map) map[string]bool {
	compat, ok := m["compat"].(tomlsubset.Map)
	if !ok {
		return nil
	}
	out := map[string]bool{}
	for _, vendor := range grokCompatVendors {
		vm, ok := compat[vendor].(tomlsubset.Map)
		if !ok {
			continue
		}
		for _, surface := range grokCompatSurfaces {
			if b, ok := tomlBool(vm, surface); ok {
				out[vendor+"."+surface] = b
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// grokMCPServers reads [mcp_servers.<id>] with its command, args and the secret
// env and headers (CTO-16 Grok §3, by fingerprint only).
func grokMCPServers(m tomlsubset.Map) []platform.GrokMCPServer {
	servers, ok := m["mcp_servers"].(tomlsubset.Map)
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(servers))
	for id := range servers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []platform.GrokMCPServer
	for _, id := range ids {
		sm, ok := servers[id].(tomlsubset.Map)
		if !ok {
			continue
		}
		s := platform.GrokMCPServer{ID: id}
		s.Command, _ = tomlString(sm, "command")
		s.Args = tomlStringArray(sm, "args")
		s.EnvKeys, s.Env = tomlSecretTable(sm, "env")
		s.HeaderKeys, s.Headers = tomlSecretTable(sm, "headers")
		out = append(out, s)
	}
	return out
}

// grokAPIKeys reads [model.<id>].api_key and [telemetry].events_api_key values
// (CTO-16 Grok §3), secret, by fingerprint only.
func grokAPIKeys(m tomlsubset.Map) []platform.EnvVar {
	var out []platform.EnvVar
	if models, ok := m["model"].(tomlsubset.Map); ok {
		ids := make([]string, 0, len(models))
		for id := range models {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			mm, ok := models[id].(tomlsubset.Map)
			if !ok {
				continue
			}
			if k, ok := tomlString(mm, "api_key"); ok {
				out = append(out, platform.EnvVar{Key: id, Value: secrets.NewSecretValue(k)})
			}
		}
	}
	if tel, ok := m["telemetry"].(tomlsubset.Map); ok {
		if k, ok := tomlString(tel, "events_api_key"); ok {
			out = append(out, platform.EnvVar{Key: "telemetry", Value: secrets.NewSecretValue(k)})
		}
	}
	return out
}

// grokSessionCreds reads the plaintext session credential files beside the user
// config (auth.json, mcp_credentials.json), by fingerprint only: every string
// leaf value is a secret (CTO-16 Grok §3). Nothing is read from sessions/.
func grokSessionCreds(dir string) []platform.EnvVar {
	var out []platform.EnvVar
	for _, name := range []string{"auth.json", "mcp_credentials.json"} {
		raw, status, _ := readBounded(filepath.Join(dir, name))
		if status != platform.MCPSourceRead {
			continue
		}
		var doc any
		if json.Unmarshal(raw, &doc) != nil {
			continue
		}
		keys := map[string]string{}
		grokWalkJSON("", doc, keys)
		names := make([]string, 0, len(keys))
		for k := range keys {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			out = append(out, platform.EnvVar{Key: name + ":" + k, Value: secrets.NewSecretValue(keys[k])})
		}
	}
	return out
}

// grokWalkJSON collects the non-empty string leaf values of a JSON document,
// keyed by their dotted path, so a session token is fingerprinted once.
func grokWalkJSON(prefix string, v any, out map[string]string) {
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
			grokWalkJSON(p, val, out)
		}
	}
}

// readGrokTrustedFolders reads ~/.grok/trusted_folders.toml: the canonical repo
// roots with [folders."<root>"].trusted = true (CTO-16 Grok §2).
func readGrokTrustedFolders(dir string) []string {
	raw, status, _ := readBounded(filepath.Join(dir, "trusted_folders.toml"))
	if status != platform.MCPSourceRead {
		return nil
	}
	m, err := tomlsubset.Parse(raw)
	if err != nil {
		return nil
	}
	folders, ok := m["folders"].(tomlsubset.Map)
	if !ok {
		return nil
	}
	var out []string
	for path, v := range folders {
		if fm, ok := v.(tomlsubset.Map); ok {
			if b, ok := tomlBool(fm, "trusted"); ok && b {
				out = append(out, graph.CanonicalPath(path))
			}
		}
	}
	sort.Strings(out)
	return out
}

// envFlagOff reports an environment variable set to a false value ("0",
// "false", empty): GROK_FOLDER_TRUST=0 turns the control off (CTO-16 Grok §2).
func envFlagOff(env agentdirs.Env, name string) bool {
	v, ok := env(name)
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "0", "false", "":
		return true
	}
	return false
}

// grokRules reads [permission] rules, the verbose form (BV-237): an array of
// { action, tool, pattern? } tables. An entry with an unknown action or tool,
// or that is not a table, is skipped: it grants nothing the engine would read.
func grokRules(perm tomlsubset.Map) []platform.GrokRule {
	arr, ok := perm["rules"].(tomlsubset.Array)
	if !ok {
		return nil
	}
	var out []platform.GrokRule
	for _, e := range arr {
		t, ok := e.(tomlsubset.Map)
		if !ok {
			continue
		}
		action, _ := tomlString(t, "action")
		tool, _ := tomlString(t, "tool")
		pattern, _ := tomlString(t, "pattern")
		action, tool = strings.ToLower(strings.TrimSpace(action)), strings.ToLower(strings.TrimSpace(tool))
		switch action {
		case "allow", "deny", "ask":
		default:
			continue
		}
		switch tool {
		case "any", "bash", "edit", "read", "grep", "mcp", "webfetch":
		default:
			continue
		}
		out = append(out, platform.GrokRule{Action: action, Tool: tool, Pattern: strings.TrimSpace(pattern)})
	}
	return out
}
