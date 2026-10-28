package windows

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/yamlsubset"
)

// ListHermes implements platform.Platform (decision 2026-10-27, agentes
// nuevos, N2a; LECTURA-100 §4, steps 2, 3 and 5).
func (w *WindowsPlatform) ListHermes(_ context.Context) (platform.HermesScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.HermesScan{}, fmt.Errorf("resolve home dir: %w", err)
	}
	return ReadHermes(home, nil), nil
}

// ReadHermes reads <HERMES_HOME>\config.yaml (HERMES_HOME, or
// %LOCALAPPDATA%\hermes: hermes_constants.py:57-64, 194-201 @ 8acb7f6) and
// every <HERMES_HOME>\profiles\<name>\config.yaml, with the YAML subset
// reader (internal/yamlsubset). A file outside the subset is not read
// (parse_error): never guessed. env is nil outside tests.
func ReadHermes(home string, env agentdirs.Env) platform.HermesScan {
	hermes := agentdirs.HermesHome(home, env)
	var scan platform.HermesScan
	dirs := []string{hermes}
	if entries, err := os.ReadDir(filepath.Join(hermes, "profiles")); err == nil {
		for _, e := range entries {
			if e.IsDir() && e.Type()&os.ModeSymlink == 0 {
				dirs = append(dirs, filepath.Join(hermes, "profiles", e.Name()))
			}
		}
	}
	for _, dir := range dirs {
		path := filepath.Join(dir, "config.yaml")
		cfg, servers := readHermesConfig(path, dir)
		if cfg.Status == platform.MCPSourceMissing {
			continue
		}
		scan.Configs = append(scan.Configs, cfg)
		scan.Sources = append(scan.Sources, platform.MCPSource{
			Platform: agentdirs.Hermes, ConfigPath: path, ConfigDir: dir,
			Status: cfg.Status, Reason: cfg.Reason, LoadsMCP: true,
		})
		scan.Servers = append(scan.Servers, servers...)
		if cfg.Status == platform.MCPSourceRead {
			protected := "absent"
			if cfg.ProtectedInstructionFiles != nil {
				protected = fmt.Sprint(*cfg.ProtectedInstructionFiles)
			}
			slog.Info("hermes: config", "path", path, "approvals_mode", cfg.ApprovalMode,
				"protected_instruction_files", protected, "mcp_servers", len(servers))
		}
	}
	return scan
}

// hermesModes are approvals.mode's values (tools/approval_context.py:263).
var hermesModes = map[string]bool{"manual": true, "smart": true, "off": true}

// readHermesConfig reads one config.yaml: its outcome, its security keys and
// its MCP servers.
func readHermesConfig(path, dir string) (platform.HermesConfig, []platform.MCPServer) {
	cfg := platform.HermesConfig{Path: path, ConfigDir: dir}
	raw, status, reason := readBounded(path)
	cfg.Status, cfg.Reason = status, reason
	if status != platform.MCPSourceRead {
		return cfg, nil
	}
	doc, err := yamlsubset.Parse(raw)
	if err != nil {
		slog.Warn("hermes: config not read: outside the YAML subset", "path", path, "err", err)
		cfg.Status, cfg.Reason = platform.MCPSourceFailed, graph.Reason(graph.ReasonParseError, err.Error())
		return cfg, nil
	}
	root, _ := doc.(yamlsubset.Map)

	// approvals.mode (tools/approval_context.py:266-280): absent → manual;
	// the YAML 1.1 boolean false («off») → off, true → manual; a string is
	// matched lowercased; anything else → manual, as Hermes does.
	cfg.ApprovalMode = "manual"
	if s, ok := mapOf(root, "approvals")["mode"].(yamlsubset.Scalar); ok {
		if b, isBool := s.Bool(); isBool {
			if !b {
				cfg.ApprovalMode = "off"
			}
		} else if m := strings.ToLower(strings.TrimSpace(s.Text)); hermesModes[m] {
			cfg.ApprovalMode = m
		}
	}
	if s, ok := mapOf(root, "security")["protected_instruction_files"].(yamlsubset.Scalar); ok {
		if b, isBool := s.Bool(); isBool {
			cfg.ProtectedInstructionFiles = &b
		}
	}
	return cfg, hermesServers(mapOf(root, "mcp_servers"), path, dir)
}

// hermesServers are the mcp_servers entries as MCPServer
// (cli-config.yaml.example:1621-1657; tools/mcp_tool_transport.py:117-134;
// mcp_tool_registration.py:498): url makes a remote server (it wins over
// command, mcp_tool_server_run.py:277-279), transport: sse forces SSE;
// otherwise command and args run a stdio server. env and headers are
// credentials, read as every agent's.
func hermesServers(servers yamlsubset.Map, path, dir string) []platform.MCPServer {
	names := make([]string, 0, len(servers))
	for n := range servers {
		names = append(names, n)
	}
	sort.Strings(names)
	var out []platform.MCPServer
	for _, name := range names {
		entry, ok := servers[name].(yamlsubset.Map)
		if !ok {
			continue // not a server entry
		}
		envMap, headerMap := stringMap(entry["env"]), stringMap(entry["headers"])
		envKeys := sortedKeys(envMap)
		args := stringList(entry["args"])
		command, url := text(entry["command"]), strings.TrimSpace(text(entry["url"]))
		transport := ""
		switch {
		case url != "" && strings.EqualFold(text(entry["transport"]), "sse"):
			transport = "sse"
		case url != "" && (strings.HasPrefix(strings.ToLower(url), "ws://") || strings.HasPrefix(strings.ToLower(url), "wss://")):
			transport = "ws"
		case url != "":
			transport = "http"
		case strings.TrimSpace(command) != "":
			transport = "stdio"
		}
		srv := platform.MCPServer{
			Client: agentdirs.Hermes, Name: name, Command: command, Args: args,
			EnvKeys: envKeys, Env: secretVars(envMap, envKeys),
			Headers:    secretVars(headerMap, sortedKeys(headerMap)),
			ConfigPath: path, Pointer: "/mcp_servers/" + pointerEscape(name), LoadsMCP: true,
			ConfigDir: dir, Scope: platform.MCPScopeUser, Transport: transport, URL: url,
		}
		if url != "" {
			srv.Command = "" // the URL wins: Hermes does not run the command
		} else {
			srv.CommandFileDigest = programFileDigest(command, args, "")
		}
		out = append(out, srv)
	}
	return out
}

func mapOf(m yamlsubset.Map, key string) yamlsubset.Map {
	v, _ := m[key].(yamlsubset.Map)
	return v
}

// text is a scalar's text; "" for anything else or a null.
func text(v any) string {
	s, ok := v.(yamlsubset.Scalar)
	if !ok || s.IsNull() {
		return ""
	}
	return s.Text
}

// stringMap is a mapping of scalars as strings; other values are skipped.
func stringMap(v any) map[string]string {
	m, _ := v.(yamlsubset.Map)
	out := make(map[string]string, len(m))
	for k, val := range m {
		if s, ok := val.(yamlsubset.Scalar); ok {
			out[k] = s.Text
		}
	}
	return out
}

// stringList is a sequence of scalars as strings.
func stringList(v any) []string {
	seq, _ := v.(yamlsubset.Seq)
	out := []string{}
	for _, item := range seq {
		if s, ok := item.(yamlsubset.Scalar); ok {
			out = append(out, s.Text)
		}
	}
	return out
}
