package windows

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
	"github.com/qtorb/rosmarops/internal/tomlsubset"
)

// codexPolicyKey is Codex's policy key; its RequirementsPath value overrides
// where the system requirements.toml is read from on Windows (CTO-16 §2,
// point 12, verified on openai/codex@67f0994 loader/mod.rs:820-833).
const codexPolicyKey = `SOFTWARE\Policies\OpenAI\Codex`

// ListCodex reads Codex's TOML config layers (BV-140, CTO-16 §2), read only:
// the user config.toml, each repo's .codex/config.toml (in force only when the
// user config trusts it), the CODEX_HOME profiles, and the system
// requirements.toml from %ProgramData%\OpenAI\Codex (or the HKLM
// RequirementsPath). Session transcripts (sessions\) are never read.
func (w *WindowsPlatform) ListCodex(ctx context.Context, repoRoots []string) (platform.CodexScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.CodexScan{}, fmt.Errorf("resolve home dir: %w", err)
	}
	env := agentdirs.Env(os.LookupEnv)
	var reg RegistryReader
	if w.managed != nil {
		reg = w.managed.registry
	}
	req := resolveCodexRequirements(ctx, env, reg)
	return ReadCodex(home, env, repoRoots, req), nil
}

// resolveCodexRequirements is where the system requirements.toml lives on this
// machine: the HKLM RequirementsPath value when set, otherwise
// %ProgramData%\OpenAI\Codex\requirements.toml (CTO-16 §2 targets Windows; off
// Windows the registry reader is nil and the ProgramData path simply does not
// exist, so the layer reads as missing).
func resolveCodexRequirements(ctx context.Context, env agentdirs.Env, reg RegistryReader) string {
	if reg != nil {
		if v, ok, err := reg.StringValue(ctx, HiveHKLM, codexPolicyKey, "RequirementsPath"); err == nil && ok {
			if p := strings.TrimSpace(v); p != "" {
				return p
			}
		}
	}
	programData := `C:\ProgramData`
	if v, ok := env("ProgramData"); ok && v != "" {
		programData = v
	}
	return filepath.Join(programData, "OpenAI", "Codex", "requirements.toml")
}

// ReadCodex is ListCodex with the home folder, environment, repo roots and the
// resolved requirements path given, for tests. It is a pure function.
func ReadCodex(home string, env agentdirs.Env, repoRoots []string, requirementsPath string) platform.CodexScan {
	dir := agentdirs.CodexHome(home, env)
	scan := platform.CodexScan{ConfigDir: dir}
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		scan.Exists = true
	}

	// 1. User config.toml, read first: its [projects] map gates the project
	// layers below.
	user := readCodexLayer(filepath.Join(dir, "config.toml"), platform.CodexLayerUser, "", dir)
	scan.Layers = append(scan.Layers, user)

	// 2. Profiles: CODEX_HOME/<name>.config.toml (which --profile is active is
	// not readable at rest).
	for _, p := range codexProfiles(dir) {
		scan.Layers = append(scan.Layers, readCodexLayer(p, platform.CodexLayerProfile, "", dir))
	}

	// 3. Project .codex/config.toml, only in force when the user config trusts
	// the repo root (CTO-16 §2).
	for _, root := range repoRoots {
		cdir := filepath.Join(root, ".codex")
		layer := readCodexLayer(filepath.Join(cdir, "config.toml"), platform.CodexLayerProject, root, cdir)
		layer.TrustLevel = user.Projects[codexTrustKey(root)]
		layer.Trusted = layer.TrustLevel == "trusted"
		scan.Layers = append(scan.Layers, layer)
	}

	// 4. System requirements (restricts, never adds defaults).
	if requirementsPath != "" {
		scan.Layers = append(scan.Layers, readCodexLayer(requirementsPath, platform.CodexLayerRequirements, "", filepath.Dir(requirementsPath)))
	}
	return scan
}

// codexProfiles lists CODEX_HOME/<name>.config.toml files (not config.toml),
// sorted, so the scan is deterministic.
func codexProfiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || n == "config.toml" || !strings.HasSuffix(n, ".config.toml") {
			continue
		}
		out = append(out, filepath.Join(dir, n))
	}
	sort.Strings(out)
	return out
}

// codexTrustKey normalises a repo root for comparison with a [projects] key:
// cleaned, and lowercased on Windows where paths are case-insensitive.
func codexTrustKey(p string) string {
	c := filepath.Clean(p)
	if runtime.GOOS == "windows" {
		return strings.ToLower(c)
	}
	return c
}

// readCodexLayer reads one config layer, bounded and strict (CTO-16 §6): the
// TOML subset reader refuses anything it cannot read whole, and the layer is
// marked failed. rulesDir is the folder whose rules/*.rules presence is checked.
func readCodexLayer(path, kind, root, rulesDir string) platform.CodexLayer {
	l := platform.CodexLayer{Kind: kind, Path: path, Root: root}
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
	l.HasRulesFiles = codexHasRules(rulesDir)
	if s, ok := tomlString(m, "approval_policy"); ok {
		l.ApprovalPolicy, l.HasApprovalPolicy = s, true
	} else if _, present := m["approval_policy"]; present {
		l.HasApprovalPolicy = true // a granular table form; not a plain mode
	}
	l.ApprovalsReviewer, _ = tomlString(m, "approvals_reviewer")
	l.SandboxMode, _ = tomlString(m, "sandbox_mode")
	l.DefaultPermissions, _ = tomlString(m, "default_permissions")
	if sw, ok := m["sandbox_workspace_write"].(tomlsubset.Map); ok {
		if b, ok := tomlBool(sw, "network_access"); ok {
			l.NetworkAccess = &b
		}
	}
	if win, ok := m["windows"].(tomlsubset.Map); ok {
		if s, ok := tomlString(win, "sandbox"); ok {
			l.WindowsSandbox, l.HasWindowsSandbox = s, true
		}
	}
	if sep, ok := m["shell_environment_policy"].(tomlsubset.Map); ok {
		if b, ok := tomlBool(sep, "ignore_default_excludes"); ok {
			l.IgnoreDefaultExcludes = &b
		}
	}
	if projects, ok := m["projects"].(tomlsubset.Map); ok {
		l.Projects = map[string]string{}
		for p, v := range projects {
			if pm, ok := v.(tomlsubset.Map); ok {
				if tl, ok := tomlString(pm, "trust_level"); ok {
					l.Projects[codexTrustKey(p)] = tl
				}
			}
		}
	}
	l.MCP = codexMCPServers(m)
	l.BearerTokens = codexBearerTokens(m)
	return l
}

// codexHasRules reports a rules/*.rules file under dir (CTO-16 §4,
// exec_rules_not_evaluated). It opens nothing; only the names.
func codexHasRules(dir string) bool {
	entries, err := os.ReadDir(filepath.Join(dir, "rules"))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".rules") {
			return true
		}
	}
	return false
}

// codexMCPServers reads [mcp_servers.<id>] with its command, args and the
// secret env and http_headers (CTO-16 §3, by fingerprint only).
func codexMCPServers(m tomlsubset.Map) []platform.CodexMCPServer {
	servers, ok := m["mcp_servers"].(tomlsubset.Map)
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(servers))
	for id := range servers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []platform.CodexMCPServer
	for _, id := range ids {
		sm, ok := servers[id].(tomlsubset.Map)
		if !ok {
			continue
		}
		s := platform.CodexMCPServer{ID: id}
		s.Command, _ = tomlString(sm, "command")
		s.Args = tomlStringArray(sm, "args")
		s.EnvKeys, s.Env = tomlSecretTable(sm, "env")
		s.HeaderKeys, s.Headers = tomlSecretTable(sm, "http_headers")
		out = append(out, s)
	}
	return out
}

// codexBearerTokens reads model_providers.<id>.experimental_bearer_token
// values (CTO-16 §3), secret, by fingerprint only.
func codexBearerTokens(m tomlsubset.Map) []platform.EnvVar {
	providers, ok := m["model_providers"].(tomlsubset.Map)
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(providers))
	for id := range providers {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var out []platform.EnvVar
	for _, id := range ids {
		pm, ok := providers[id].(tomlsubset.Map)
		if !ok {
			continue
		}
		if tok, ok := tomlString(pm, "experimental_bearer_token"); ok {
			out = append(out, platform.EnvVar{Key: id, Value: secrets.NewSecretValue(tok)})
		}
	}
	return out
}

// tomlString returns a string value of a TOML table.
func tomlString(m tomlsubset.Map, key string) (string, bool) {
	s, ok := m[key].(string)
	return s, ok
}

// tomlBool returns a bool value of a TOML table.
func tomlBool(m tomlsubset.Map, key string) (bool, bool) {
	b, ok := m[key].(bool)
	return b, ok
}

// tomlStringArray returns the string elements of a single-line array value.
func tomlStringArray(m tomlsubset.Map, key string) []string {
	arr, ok := m[key].(tomlsubset.Array)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, v := range arr {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// tomlSecretTable reads an inline table of string values (env, http_headers)
// as sorted key names and secret-wrapped values (CTO-16 §3).
func tomlSecretTable(m tomlsubset.Map, key string) (names []string, vars []platform.EnvVar) {
	tbl, ok := m[key].(tomlsubset.Map)
	if !ok {
		return nil, nil
	}
	for k, v := range tbl {
		if s, ok := v.(string); ok {
			names = append(names, k)
			_ = s
		}
	}
	sort.Strings(names)
	for _, k := range names {
		s, _ := tbl[k].(string)
		vars = append(vars, platform.EnvVar{Key: k, Value: secrets.NewSecretValue(s)})
	}
	return names, vars
}
