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
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
	"github.com/qtorb/rosmarops/internal/tomlsubset"
)

// grokCompatVendors and grokCompatSurfaces are the 18 [compat.<vendor>] cells
// (CTO-16 Grok §2): three vendors by six surfaces, all enabled by default.
var (
	grokCompatVendors  = []string{"cursor", "claude", "codex"}
	grokCompatSurfaces = []string{"skills", "rules", "agents", "mcps", "hooks", "sessions"}
)

// ListGrok reads Grok Build's TOML config layers (BV-140, CTO-16 Grok §2), read
// only: the user config.toml, each repo's .grok/config.toml (in force only when
// the folder is trusted), the managed config and the requirements in GROK_HOME,
// and trusted_folders.toml. On Windows Grok reads neither /etc/grok nor Claude
// Code's managed-settings.json (paths.rs:22-41); this reader does not either.
func (w *WindowsPlatform) ListGrok(ctx context.Context, repoRoots []string) (platform.GrokScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.GrokScan{}, fmt.Errorf("resolve home dir: %w", err)
	}
	return ReadGrok(home, agentdirs.Env(os.LookupEnv), repoRoots), nil
}

// ReadGrok is ListGrok with the home folder, environment and repo roots given,
// for tests. It is a pure function.
func ReadGrok(home string, env agentdirs.Env, repoRoots []string) platform.GrokScan {
	dir := agentdirs.GrokHome(home, env)
	scan := platform.GrokScan{ConfigDir: dir}
	for _, name := range []string{"config.toml", "auth.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			scan.Exists = true
			break
		}
	}

	// 1. User config.toml (full keys), plus the session credential files beside
	// it (auth.json, mcp_credentials.json).
	user := readGrokLayer(filepath.Join(dir, "config.toml"), platform.GrokLayerUser, "", false)
	user.SessionCreds = grokSessionCreds(dir)
	scan.Layers = append(scan.Layers, user)

	// 2. Managed config and requirements, from GROK_HOME (CTO-16 Grok §2; on
	// Windows there is no /etc/grok).
	managed := readGrokLayer(filepath.Join(dir, "managed_config.toml"), platform.GrokLayerManaged, "", false)
	scan.Layers = append(scan.Layers, managed)
	req := readGrokLayer(filepath.Join(dir, "requirements.toml"), platform.GrokLayerRequirements, "", false)
	scan.Layers = append(scan.Layers, req)

	// Folder-trust control: on unless an exception turns it off (CTO-16 Grok §2).
	scan.FolderTrustOff = user.FolderTrustEnabled != nil && !*user.FolderTrustEnabled ||
		managed.FolderTrustEnabled != nil && !*managed.FolderTrustEnabled ||
		envFlagOff(env, "GROK_FOLDER_TRUST")
	scan.Trusted = readGrokTrustedFolders(dir)

	// 3. Project .grok/config.toml, one per repo root (only [mcp_servers],
	// [plugins], [permission] and [mcp]; in force only when trusted).
	for _, root := range repoRoots {
		layer := readGrokLayer(filepath.Join(root, ".grok", "config.toml"), platform.GrokLayerProject, root, true)
		layer.Trusted = grokRootTrusted(root, scan.Trusted, scan.FolderTrustOff, layer.Status)
		scan.Layers = append(scan.Layers, layer)
	}
	return scan
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
