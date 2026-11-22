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
	"github.com/qtorb/rosmarops/internal/jsonc"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// The OpenCode family (BV-276). Sources, read at the latest release:
// anomalyco/opencode v1.19.6 (f4e363c829, 2026-11) and Kilo-Org/kilocode
// v7.8.32 (2aa002cc0a), packages/opencode/src/config/config.ts and
// config/managed.ts. Precedence, lowest first: the user's folder, the
// project (its root files, then its dot folder), the managed folder. Only
// permission and mcp are read. The data folder (<xdg data>/opencode or
// /kilo), with the session database and auth.json, is never opened.

// openCodeFamily is what tells OpenCode and Kilo Code apart.
type openCodeFamily struct {
	product     string
	name        string   // the managed folder's name: opencode or kilo
	userFiles   []string // in the user's folder, lowest first
	rootFiles   []string // at a project's root, lowest first
	dotDirs     []string // a project's dot folders, lowest first
	dotFiles    []string // inside a dot folder, lowest first
	legacyMCP   []string // Kilo's legacy {"mcpServers": …} files in a dot folder
	vscodeMCPID string   // the VS Code extension whose global mcp_settings.json Kilo reads
}

var (
	openCodeDef = openCodeFamily{
		product:   agentdirs.OpenCode,
		name:      "opencode",
		userFiles: []string{"config.json", "opencode.json", "opencode.jsonc"}, // config.ts:272-274
		rootFiles: []string{"opencode.json", "opencode.jsonc"},                // config.ts:420-424
		dotDirs:   []string{".opencode"},                                      // config.ts:438-443
		dotFiles:  []string{"opencode.json", "opencode.jsonc"},
	}
	kiloDef = openCodeFamily{
		product:     agentdirs.KiloCode,
		name:        "kilo",
		userFiles:   []string{"config.json", "kilo.json", "kilo.jsonc", "opencode.json", "opencode.jsonc"}, // config.ts:462-480
		rootFiles:   []string{"opencode.json", "opencode.jsonc", "kilo.json", "kilo.jsonc"},                // config.ts:786-799
		dotDirs:     []string{".kilocode", ".kilo"},                                                        // config/paths.ts:23-41
		dotFiles:    []string{"opencode.json", "opencode.jsonc", "kilo.json", "kilo.jsonc"},                // kilocode/config/config.ts:679-681
		legacyMCP:   []string{"mcp.json"},                                                                  // kilocode/mcp-migrator.ts:114-118
		vscodeMCPID: "kilocode.kilo-code",                                                                  // kilocode/paths.ts:14-29
	}
)

// ListOpenCode implements platform.Platform.
func (w *WindowsPlatform) ListOpenCode(_ context.Context, repoRoots []string) ([]platform.OpenCodeScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir: %w", err)
	}
	return []platform.OpenCodeScan{
		ReadOpenCode(w.layout, home, nil, repoRoots, agentdirs.OpenCode),
		ReadOpenCode(w.layout, home, nil, repoRoots, agentdirs.KiloCode),
	}, nil
}

// ReadOpenCode reads one product of the family on layout l. env is nil
// outside tests.
func ReadOpenCode(l agentdirs.Layout, home string, env agentdirs.Env, repoRoots []string, product string) platform.OpenCodeScan {
	def, dir := openCodeDef, agentdirs.OpenCodeDir(home, env)
	if product == agentdirs.KiloCode {
		def, dir = kiloDef, agentdirs.KiloDir(home, env)
	}
	scan := platform.OpenCodeScan{Product: product, ConfigDir: dir}
	servers := map[string]platform.MCPServer{}
	var order []string
	read := func(kind, path, root string) {
		layer, mcp, ok := readOpenCodeLayer(kind, path, root)
		if !ok {
			return
		}
		scan.Layers = append(scan.Layers, layer)
		scan.Present = true
		applyOpenCodeMCP(def.product, dir, layer, mcp, servers, &order)
	}
	for _, f := range def.userFiles {
		read(platform.OpenCodeLayerUser, filepath.Join(dir, f), "")
	}
	for _, r := range repoRoots {
		for _, f := range def.rootFiles {
			read(platform.OpenCodeLayerProject, filepath.Join(r, f), r)
		}
		for _, d := range def.dotDirs {
			for _, f := range def.dotFiles {
				read(platform.OpenCodeLayerProject, filepath.Join(r, d, f), r)
			}
		}
	}
	managed := l.OpenCodeManagedDir(def.name, env)
	for _, f := range def.dotFiles {
		read(platform.OpenCodeLayerManaged, filepath.Join(managed, f), "")
	}
	// Kilo Code's legacy MCP files: {"mcpServers": …} in each project's dot
	// folders, and the VS Code extension's global mcp_settings.json.
	if def.vscodeMCPID != "" {
		for _, r := range repoRoots {
			for _, d := range def.dotDirs {
				for _, f := range def.legacyMCP {
					readLegacyMCP(def.product, dir, platform.MCPScopeProject, filepath.Join(r, d, f), r, servers, &order, &scan)
				}
			}
		}
		global := filepath.Join(l.VSCodeUserDir(home, env), "globalStorage", def.vscodeMCPID, "settings", "mcp_settings.json")
		readLegacyMCP(def.product, dir, platform.MCPScopeUser, global, "", servers, &order, &scan)
	}
	if !scan.Present {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			scan.Present = true
		}
	}
	for _, name := range order {
		if s, ok := servers[name]; ok {
			scan.Servers = append(scan.Servers, s)
		}
	}
	return scan
}

// openCodeMCP is one mcp.<name> entry: local (command array, environment)
// or remote (url, headers); a bare {"enabled": false} turns off a server a
// lower layer defined (v1/config/mcp.ts:6-60, v1/config/config.ts:113-115).
type openCodeMCP struct {
	Type        string            `json:"type"`
	Command     []string          `json:"command"`
	Environment map[string]string `json:"environment"`
	Env         map[string]string `json:"env"` // Kilo's alias of environment
	URL         string            `json:"url"`
	Headers     map[string]string `json:"headers"`
	Enabled     *bool             `json:"enabled"`
}

// readOpenCodeLayer reads one file; ok is false when it does not exist.
func readOpenCodeLayer(kind, path, root string) (platform.OpenCodeLayer, map[string]openCodeMCP, bool) {
	layer := platform.OpenCodeLayer{Kind: kind, Path: path, Root: root}
	raw, status, reason := readBounded(path)
	if status == platform.MCPSourceMissing {
		return layer, nil, false
	}
	layer.Status, layer.Reason = status, reason
	if status != platform.MCPSourceRead {
		return layer, nil, true
	}
	var doc struct {
		Permission json.RawMessage        `json:"permission"`
		MCP        map[string]openCodeMCP `json:"mcp"`
	}
	if err := json.Unmarshal(jsonc.Strip(raw), &doc); err != nil {
		layer.Status, layer.Reason = platform.MCPSourceFailed, fmt.Sprintf("parse: %v", err)
		return layer, nil, true
	}
	perm, err := parseOpenCodePermission(doc.Permission)
	if err != nil {
		layer.Status, layer.Reason = platform.MCPSourceFailed, fmt.Sprintf("permission: %v", err)
		return layer, nil, true
	}
	layer.Permission = perm
	return layer, doc.MCP, true
}

// parseOpenCodePermission reads permission: one action ("allow" is
// {"*": "allow"}) or an object of tool keys, each an action or a map of
// patterns to actions (v1/config/permission.ts:5-48).
func parseOpenCodePermission(raw json.RawMessage) (map[string]platform.OpenCodeRule, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var single string
	if json.Unmarshal(raw, &single) == nil {
		if !openCodeAction(single) {
			return nil, fmt.Errorf("action %q", single)
		}
		return map[string]platform.OpenCodeRule{"*": {Action: single}}, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	out := map[string]platform.OpenCodeRule{}
	for key, v := range obj {
		var action string
		if json.Unmarshal(v, &action) == nil {
			if !openCodeAction(action) {
				return nil, fmt.Errorf("%s: action %q", key, action)
			}
			out[key] = platform.OpenCodeRule{Action: action}
			continue
		}
		var patterns map[string]string
		if err := json.Unmarshal(v, &patterns); err != nil {
			return nil, fmt.Errorf("%s: %w", key, err)
		}
		for p, a := range patterns {
			if !openCodeAction(a) {
				return nil, fmt.Errorf("%s %s: action %q", key, p, a)
			}
		}
		out[key] = platform.OpenCodeRule{Patterns: patterns}
	}
	return out, nil
}

func openCodeAction(a string) bool { return a == "allow" || a == "ask" || a == "deny" }

// isOpenCodeRef is a {env:…} or {file:…} substitution: not a secret at rest
// (config/variable.ts:33-61).
func isOpenCodeRef(v string) bool {
	return strings.Contains(v, "{env:") || strings.Contains(v, "{file:")
}

func openCodeScope(kind string) string {
	switch kind {
	case platform.OpenCodeLayerProject:
		return platform.MCPScopeProject
	case platform.OpenCodeLayerManaged:
		return platform.MCPScopeManaged
	}
	return platform.MCPScopeUser
}

// applyOpenCodeMCP merges a layer's mcp entries into servers: a later layer
// replaces a server of the same name, and {"enabled": false} removes it.
func applyOpenCodeMCP(product, dir string, layer platform.OpenCodeLayer, mcp map[string]openCodeMCP, servers map[string]platform.MCPServer, order *[]string) {
	names := make([]string, 0, len(mcp))
	for n := range mcp {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		e := mcp[name]
		if e.Enabled != nil && !*e.Enabled {
			delete(servers, name)
			continue
		}
		if e.Type == "" && len(e.Command) == 0 && e.URL == "" {
			continue // only {"enabled": true}: keeps a lower layer's server
		}
		env := e.Environment
		if len(env) == 0 {
			env = e.Env
		}
		srv := platform.MCPServer{
			Client: product, Name: name, ConfigPath: layer.Path, ConfigDir: dir,
			Pointer: "/mcp/" + name, LoadsMCP: true, Scope: openCodeScope(layer.Kind), ScopePath: layer.Root,
		}
		if e.Type == "remote" || (e.Type == "" && e.URL != "") {
			srv.URL, srv.Transport = e.URL, "http"
		} else {
			srv.Transport = "stdio"
			if len(e.Command) > 0 {
				srv.Command, srv.Args = e.Command[0], append([]string{}, e.Command[1:]...)
			}
		}
		srv.EnvKeys, srv.Env = secretEntries(env)
		_, srv.Headers = secretEntries(e.Headers)
		if _, seen := servers[name]; !seen {
			*order = append(*order, name)
		}
		servers[name] = srv
	}
}

// secretEntries are a map's keys and its values wrapped as SecretValues,
// sorted, without the {env:…}/{file:…} references.
func secretEntries(m map[string]string) ([]string, []platform.EnvVar) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var vars []platform.EnvVar
	for _, k := range keys {
		if v := m[k]; !isOpenCodeRef(v) {
			vars = append(vars, platform.EnvVar{Key: k, Value: secrets.NewSecretValue(v)})
		}
	}
	return keys, vars
}

// readLegacyMCP reads Kilo Code's legacy {"mcpServers": …} files
// (kilocode/mcp-migrator.ts:100-118): command and args, env, url, headers,
// disabled.
func readLegacyMCP(product, dir, scope, path, root string, servers map[string]platform.MCPServer, order *[]string, scan *platform.OpenCodeScan) {
	raw, status, reason := readBounded(path)
	if status == platform.MCPSourceMissing {
		return
	}
	scan.Present = true
	layer := platform.OpenCodeLayer{Kind: platform.OpenCodeLayerProject, Path: path, Root: root, Status: status, Reason: reason}
	if scope == platform.MCPScopeUser {
		layer.Kind = platform.OpenCodeLayerUser
	}
	var doc struct {
		MCPServers map[string]struct {
			Command  string            `json:"command"`
			Args     []string          `json:"args"`
			Env      map[string]string `json:"env"`
			URL      string            `json:"url"`
			Headers  map[string]string `json:"headers"`
			Disabled bool              `json:"disabled"`
		} `json:"mcpServers"`
	}
	if status == platform.MCPSourceRead {
		if err := json.Unmarshal(jsonc.Strip(raw), &doc); err != nil {
			layer.Status, layer.Reason = platform.MCPSourceFailed, fmt.Sprintf("parse: %v", err)
		}
	}
	scan.Layers = append(scan.Layers, layer)
	if layer.Status != platform.MCPSourceRead {
		return
	}
	names := make([]string, 0, len(doc.MCPServers))
	for n := range doc.MCPServers {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		e := doc.MCPServers[name]
		if e.Disabled {
			continue
		}
		if _, defined := servers[name]; defined {
			continue // the kilo.jsonc entry of the same name wins
		}
		srv := platform.MCPServer{
			Client: product, Name: name, Command: e.Command, Args: e.Args, URL: e.URL, ConfigPath: path, ConfigDir: dir,
			Pointer: "/mcpServers/" + name, LoadsMCP: true, Scope: scope, ScopePath: root, Transport: "stdio",
		}
		if e.URL != "" && e.Command == "" {
			srv.Transport = "http"
		}
		srv.EnvKeys, srv.Env = secretEntries(e.Env)
		_, srv.Headers = secretEntries(e.Headers)
		*order = append(*order, name)
		servers[name] = srv
	}
}
