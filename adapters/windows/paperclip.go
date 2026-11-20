package windows

import (
	"bufio"
	"bytes"
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
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/yamlsubset"
)

// Paperclip (CTO-75) keeps everything under PAPERCLIP_HOME, or ~/.paperclip
// (%USERPROFILE%\.paperclip on Windows, from os.homedir()): one folder per
// instance under instances/<id>/. RosmarOps reads, at rest, only what tells
// how the instance is exposed and which version runs: config.json,
// cli/install.json, a Paperclip service unit, npm's cache and the
// .paperclip.yaml manifests of the repositories. Its database (db/), data/,
// secrets/master.key, .env, run logs and MCP configs are never opened: they
// hold secrets, so only their existence, permissions and owner are seen.

// paperclipServiceVars are the PAPERCLIP_* variables a service unit may set
// that change what config.json says (CTO-75 §1). Nothing else of the unit is
// kept.
var paperclipServiceVars = map[string]bool{
	"PAPERCLIP_DEPLOYMENT_MODE":         true,
	"PAPERCLIP_BIND":                    true,
	"PAPERCLIP_DEPLOYMENT_EXPOSURE":     true,
	"PAPERCLIP_AUTH_DISABLE_SIGN_UP":    true,
	"PAPERCLIP_TELEMETRY_DISABLED":      true,
	"PAPERCLIP_HOME":                    true,
	"PAPERCLIP_INSTANCE_ID":             true,
	"PAPERCLIP_SECRETS_MASTER_KEY_FILE": true,
}

// ListPaperclip implements platform.Platform.
func (w *WindowsPlatform) ListPaperclip(_ context.Context, repoRoots []string) (platform.PaperclipScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.PaperclipScan{}, fmt.Errorf("resolve home dir: %w", err)
	}
	return ReadPaperclip(w.layout, home, nil, repoRoots), nil
}

// PaperclipHome is PAPERCLIP_HOME when set to a folder, else <home>/.paperclip
// (packages/shared/src/home-paths.ts:16-20: no XDG, no %APPDATA%).
func PaperclipHome(home string, env agentdirs.Env) string {
	if env == nil {
		env = os.LookupEnv
	}
	if v, ok := env("PAPERCLIP_HOME"); ok && strings.TrimSpace(v) != "" {
		if info, err := os.Stat(v); err == nil && info.IsDir() {
			return filepath.Clean(v)
		}
	}
	return filepath.Join(home, ".paperclip")
}

// ReadPaperclip reads Paperclip on layout l. env is nil outside tests.
func ReadPaperclip(l agentdirs.Layout, home string, env agentdirs.Env, repoRoots []string) platform.PaperclipScan {
	var scan platform.PaperclipScan
	base := PaperclipHome(home, env)
	if info, err := os.Stat(base); err != nil || !info.IsDir() {
		// No Paperclip home: manifests alone are inert (Paperclip reads them
		// only when it imports them), and npm's cache alone is not a
		// Paperclip on this computer.
		return scan
	}
	scan.Base = base
	instances := filepath.Join(base, "instances")
	entries, _ := os.ReadDir(instances)
	for _, e := range entries {
		if !e.IsDir() || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		in := readPaperclipInstance(e.Name(), filepath.Join(instances, e.Name()))
		scan.Instances = append(scan.Instances, in)
		if in.ConfigStatus != platform.MCPSourceMissing {
			scan.Sources = append(scan.Sources, platform.MCPSource{Platform: "paperclip", ConfigPath: in.ConfigPath, ConfigDir: base, Status: in.ConfigStatus, Reason: in.Reason})
		}
	}
	install := filepath.Join(base, "cli", "install.json")
	if v, status, reason := readPaperclipVersionFile(install); status != platform.MCPSourceMissing {
		scan.Sources = append(scan.Sources, platform.MCPSource{Platform: "paperclip", ConfigPath: install, ConfigDir: base, Status: status, Reason: reason})
		if status == platform.MCPSourceRead && v != "" {
			scan.Installed, scan.InstallPath = v, install
		}
	}
	scan.NPMCache = paperclipNPMCache(l, home, env)
	scan.Service = paperclipService(l, home, env)
	scan.Manifests = paperclipManifests(repoRoots)
	if t := telemetryState(scan); t != "" {
		// CTO-76 §4: Paperclip sends anonymous telemetry unless turned off.
		// A fact for the run log, not a finding.
		slog.Info("paperclip: telemetry", "state", t)
	}
	return scan
}

// readPaperclipInstance reads instances/<id>/config.json and lstats the
// instance's protected files.
func readPaperclipInstance(id, dir string) platform.PaperclipInstance {
	in := platform.PaperclipInstance{ID: id, Dir: dir, ConfigPath: filepath.Join(dir, "config.json")}
	raw, status, reason := readBounded(in.ConfigPath)
	in.ConfigStatus, in.Reason = status, reason
	if status == platform.MCPSourceRead {
		if err := decodePaperclipConfig(raw, &in); err != nil {
			in.ConfigStatus, in.Reason = platform.MCPSourceFailed, graph.Reason(graph.ReasonParseError, err.Error())
		}
	}
	for _, f := range []struct{ role, rel string }{
		{platform.PaperclipConfig, "config.json"},
		{platform.PaperclipMasterKey, filepath.Join("secrets", "master.key")},
		{platform.PaperclipEnv, ".env"},
		{platform.PaperclipDB, "db"},
		{platform.PaperclipData, "data"},
		{platform.PaperclipRunLogs, filepath.Join("data", "run-logs")},
	} {
		in.Files = append(in.Files, lstatPaperclip(f.role, filepath.Join(dir, f.rel)))
	}
	return in
}

// lstatPaperclip sees a protected file or folder from its metadata only:
// never opened, never followed.
func lstatPaperclip(role, path string) platform.PaperclipFile {
	f := platform.PaperclipFile{Role: role, Path: path, Owner: graph.OwnerUnknown}
	info, err := os.Lstat(path)
	if err != nil {
		return f
	}
	f.Exists = true
	f.Mode, f.Owner = fileModeOwner(info)
	return f
}

// decodePaperclipConfig keeps the fields that say how an instance is exposed
// (config-schema.ts:20-142). The two secret-bearing keys are decoded as raw
// JSON only to know whether they hold a value: the value is dropped here and
// never kept, logged or hashed.
func decodePaperclipConfig(raw []byte, in *platform.PaperclipInstance) error {
	var c struct {
		Server struct {
			DeploymentMode string `json:"deploymentMode"`
			Exposure       string `json:"exposure"`
			Bind           string `json:"bind"`
			Host           string `json:"host"`
			Port           *int   `json:"port"`
		} `json:"server"`
		Auth struct {
			DisableSignUp *bool `json:"disableSignUp"`
		} `json:"auth"`
		Database struct {
			Mode             string          `json:"mode"`
			ConnectionString json.RawMessage `json:"connectionString"`
		} `json:"database"`
		LLM struct {
			APIKey json.RawMessage `json:"apiKey"`
		} `json:"llm"`
		Telemetry struct {
			Enabled *bool `json:"enabled"`
		} `json:"telemetry"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return err
	}
	in.DeploymentMode, in.Exposure = c.Server.DeploymentMode, c.Server.Exposure
	in.Bind, in.Host = c.Server.Bind, c.Server.Host
	if c.Server.Port != nil {
		in.Port = *c.Server.Port
	}
	in.SignUpDisabled = c.Auth.DisableSignUp
	in.DatabaseMode = c.Database.Mode
	in.Telemetry = c.Telemetry.Enabled
	in.LLMAPIKeySet = holdsValue(c.LLM.APIKey)
	in.ConnectionStringSet = holdsValue(c.Database.ConnectionString)
	return nil
}

// holdsValue reports whether a raw JSON value is a non-empty string, without
// keeping it.
func holdsValue(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 2 && t[0] == '"'
}

// readPaperclipVersionFile reads the version field of an install.json or a
// package.json (both JSON with a top-level "version").
func readPaperclipVersionFile(path string) (string, platform.MCPSourceStatus, string) {
	raw, status, reason := readBounded(path)
	if status != platform.MCPSourceRead {
		return "", status, reason
	}
	var v struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", platform.MCPSourceFailed, graph.Reason(graph.ReasonParseError, err.Error())
	}
	return strings.TrimSpace(v.Version), platform.MCPSourceRead, ""
}

// paperclipNPMCache finds paperclipai in npm's npx cache
// (<cache>/_npx/<hash>/node_modules/paperclipai/package.json), where a
// Paperclip run with npx keeps its version. The cache is npm_config_cache, or
// ~/.npm (%LOCALAPPDATA%\npm-cache on Windows).
func paperclipNPMCache(l agentdirs.Layout, home string, env agentdirs.Env) []platform.PaperclipPackage {
	if env == nil {
		env = os.LookupEnv
	}
	cache := filepath.Join(home, ".npm")
	if l == agentdirs.WindowsLayout {
		local := filepath.Join(home, "AppData", "Local")
		if v, ok := env("LOCALAPPDATA"); ok && v != "" {
			local = v
		}
		cache = filepath.Join(local, "npm-cache")
	}
	if v, ok := env("npm_config_cache"); ok && strings.TrimSpace(v) != "" {
		cache = v
	}
	dirs, _ := os.ReadDir(filepath.Join(cache, "_npx"))
	var out []platform.PaperclipPackage
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		pkg := filepath.Join(cache, "_npx", d.Name(), "node_modules", "paperclipai", "package.json")
		if v, status, _ := readPaperclipVersionFile(pkg); status == platform.MCPSourceRead && v != "" {
			out = append(out, platform.PaperclipPackage{Version: v, Path: pkg})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// paperclipService reads a Paperclip service unit
// (cli/src/services/service-manager.ts): a systemd user unit on Linux, a
// launchd agent on macOS. Only its PAPERCLIP_* variables are kept.
func paperclipService(l agentdirs.Layout, home string, env agentdirs.Env) *platform.PaperclipService {
	var dir, ext string
	switch l {
	case agentdirs.LinuxLayout:
		if env == nil {
			env = os.LookupEnv
		}
		cfg := filepath.Join(home, ".config")
		if v, ok := env("XDG_CONFIG_HOME"); ok && filepath.IsAbs(v) {
			cfg = v
		}
		dir, ext = filepath.Join(cfg, "systemd", "user"), ".service"
	case agentdirs.MacOSLayout:
		dir, ext = filepath.Join(home, "Library", "LaunchAgents"), ".plist"
	default:
		return nil
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		if e.IsDir() || !strings.Contains(name, "paperclip") || !strings.HasSuffix(name, ext) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, status, _ := readBounded(path)
		if status != platform.MCPSourceRead {
			continue
		}
		vars := map[string]string{}
		if ext == ".service" {
			systemdEnv(raw, vars)
		} else {
			launchdEnv(raw, vars)
		}
		return &platform.PaperclipService{Path: path, Env: vars}
	}
	return nil
}

// systemdEnv keeps the PAPERCLIP_* pairs of Environment= lines.
func systemdEnv(raw []byte, vars map[string]string) {
	sc := bufio.NewScanner(bytes.NewReader(raw))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "Environment=") {
			continue
		}
		for _, f := range strings.Fields(strings.TrimPrefix(line, "Environment=")) {
			f = strings.Trim(f, `"'`)
			if k, v, ok := strings.Cut(f, "="); ok && paperclipServiceVars[k] {
				vars[k] = v
			}
		}
	}
}

// launchdEnv keeps the PAPERCLIP_* pairs of a plist's EnvironmentVariables
// dictionary (<key>NAME</key><string>value</string>).
func launchdEnv(raw []byte, vars map[string]string) {
	s := string(raw)
	i := strings.Index(s, "<key>EnvironmentVariables</key>")
	if i < 0 {
		return
	}
	s = s[i:]
	if end := strings.Index(s, "</dict>"); end >= 0 {
		s = s[:end]
	}
	for {
		k := strings.Index(s, "<key>")
		if k < 0 {
			return
		}
		s = s[k+len("<key>"):]
		ke := strings.Index(s, "</key>")
		if ke < 0 {
			return
		}
		name := strings.TrimSpace(s[:ke])
		s = s[ke:]
		v := strings.Index(s, "<string>")
		ve := strings.Index(s, "</string>")
		if v < 0 || ve < v {
			continue
		}
		if paperclipServiceVars[name] {
			vars[name] = strings.TrimSpace(s[v+len("<string>") : ve])
		}
		s = s[ve:]
	}
}

// paperclipManifests reads the .paperclip.yaml at each repository's root
// (docs/companies/companies-spec.md: schema paperclip/v1) and counts its
// agents that run an arbitrary command: adapter type process, or no type,
// which Paperclip imports as process (company-portability.ts:3283).
func paperclipManifests(repoRoots []string) []platform.PaperclipManifest {
	var out []platform.PaperclipManifest
	for _, repo := range repoRoots {
		path := filepath.Join(repo, ".paperclip.yaml")
		raw, status, _ := readBounded(path)
		if status != platform.MCPSourceRead {
			continue
		}
		doc, err := yamlsubset.ParseKeys(raw, "schema", "agents")
		if err != nil {
			continue
		}
		top, _ := doc.(yamlsubset.Map)
		if s, _ := top["schema"].(yamlsubset.Scalar); !strings.HasPrefix(s.Text, "paperclip/") {
			continue
		}
		m := platform.PaperclipManifest{Path: path, Repo: repo}
		agents, _ := top["agents"].(yamlsubset.Map)
		for _, a := range agents {
			agent, _ := a.(yamlsubset.Map)
			adapter, _ := agent["adapter"].(yamlsubset.Map)
			typ, _ := adapter["type"].(yamlsubset.Scalar)
			switch strings.TrimSpace(typ.Text) {
			case "":
				m.UntypedAgents++
			case "process":
				m.ProcessAgents++
			}
		}
		out = append(out, m)
	}
	return out
}

// telemetryState is how Paperclip's telemetry stands at rest: "off" when an
// instance or the service turns it off, "on" when config.json says so,
// "default_on" when nothing says (on by default, CTO-76 §4).
func telemetryState(scan platform.PaperclipScan) string {
	if len(scan.Instances) == 0 {
		return ""
	}
	if scan.Service != nil && scan.Service.Env["PAPERCLIP_TELEMETRY_DISABLED"] != "" {
		return "off"
	}
	state := "default_on"
	for _, in := range scan.Instances {
		if in.Telemetry != nil {
			if !*in.Telemetry {
				return "off"
			}
			state = "on"
		}
	}
	return state
}
