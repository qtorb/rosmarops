package windows

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/yamlsubset"
)

// BV-268 (CTO-76 §1-§3): Paseo, Claude Squad and Orca at rest, and Claude
// Code's subagents. Paseo and Claude Squad keep plain JSON files; RosmarOps
// reads only the fields the graph uses. Never opened: Paseo's
// daemon-keypair.json, local-credential and push-tokens.json; Orca's
// profile-state.db and the key files of ~/.orca. Values that may be secrets
// (Paseo's launchEnv and apiKey, Claude Squad's diff_stats) are decoded
// into nothing: the structs below have no field for them.

// ListOrchestrators implements platform.Platform.
func (w *WindowsPlatform) ListOrchestrators(_ context.Context, repoRoots []string) (platform.OrchestratorScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.OrchestratorScan{}, fmt.Errorf("resolve home dir: %w", err)
	}
	return ReadOrchestrators(w.layout, home, claudeConfigDir(home), nil, repoRoots), nil
}

// ReadOrchestrators reads the orchestrators on layout l. env is nil outside
// tests.
func ReadOrchestrators(l agentdirs.Layout, home, claudeDir string, env agentdirs.Env, repoRoots []string) platform.OrchestratorScan {
	var scan platform.OrchestratorScan
	scan.Subagents = ReadClaudeSubagents(claudeDir, repoRoots)
	for _, s := range scan.Subagents {
		scan.Sources = append(scan.Sources, platform.MCPSource{Platform: "claude-code", ConfigPath: s.Path, ConfigDir: claudeDir, Status: s.Status, Reason: s.Reason})
	}
	if p := ReadPaseo(home, env); p != nil {
		scan.Paseo = p
		scan.Sources = append(scan.Sources, platform.MCPSource{Platform: "paseo", ConfigPath: p.ConfigPath, ConfigDir: p.Home, Status: p.ConfigStatus, Reason: p.Reason})
	}
	if c := ReadClaudeSquad(home); c != nil {
		scan.ClaudeSquad = c
		scan.Sources = append(scan.Sources, platform.MCPSource{Platform: "claude-squad", ConfigPath: c.ConfigPath, ConfigDir: c.Dir, Status: c.ConfigStatus, Reason: c.Reason})
	}
	scan.Orca = ReadOrca(l, home, claudeDir, env)
	return scan
}

// readSmallJSON decodes path into v: a regular file of at most 2 MB
// (invariant 6), never a link.
func readSmallJSON(path string, v any) (platform.MCPSourceStatus, string) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return platform.MCPSourceMissing, ""
	}
	if err != nil {
		return platform.MCPSourceFailed, fmt.Sprintf("stat: %v", err)
	}
	if !info.Mode().IsRegular() {
		return platform.MCPSourceFailed, "not a regular file"
	}
	if info.Size() > yamlsubset.MaxBytes {
		return platform.MCPSourceFailed, fmt.Sprintf("larger than %d bytes", yamlsubset.MaxBytes)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return platform.MCPSourceFailed, fmt.Sprintf("read: %v", err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return platform.MCPSourceFailed, fmt.Sprintf("parse: %v", err)
	}
	return platform.MCPSourceRead, ""
}

// --- Paseo ---------------------------------------------------------------

// PaseoHome is PASEO_HOME when set to a folder, else <home>/.paseo
// (packages/server/src/server/paseo-home.ts:14-17 at v0.16.2).
func PaseoHome(home string, env agentdirs.Env) string {
	if env == nil {
		env = os.LookupEnv
	}
	if v, ok := env("PASEO_HOME"); ok && strings.TrimSpace(v) != "" {
		if info, err := os.Stat(v); err == nil && info.IsDir() {
			return filepath.Clean(v)
		}
	}
	return filepath.Join(home, ".paseo")
}

// filled records whether a JSON string holds a value, and drops the value.
type filled bool

func (f *filled) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return nil // not a string: not a key
	}
	*f = filled(strings.TrimSpace(s) != "")
	return nil
}

type paseoSpeech struct {
	APIKey filled `json:"apiKey"`
}

type paseoConfig struct {
	Daemon struct {
		Listen string `json:"listen"`
	} `json:"daemon"`
	Features struct {
		Dictation struct {
			STT paseoSpeech `json:"stt"`
		} `json:"dictation"`
		VoiceMode struct {
			STT paseoSpeech `json:"stt"`
			TTS paseoSpeech `json:"tts"`
		} `json:"voiceMode"`
	} `json:"features"`
}

type paseoAgentFile struct {
	Provider   string  `json:"provider"`
	Cwd        string  `json:"cwd"`
	LastModeID *string `json:"lastModeId"`
	ArchivedAt *string `json:"archivedAt"`
	Config     *struct {
		ModeID *string `json:"modeId"`
	} `json:"config"`
}

type paseoScheduleFile struct {
	Status string `json:"status"`
	Target struct {
		Type   string `json:"type"`
		Config struct {
			Provider string  `json:"provider"`
			ModeID   *string `json:"modeId"`
		} `json:"config"`
	} `json:"target"`
}

// ReadPaseo reads Paseo's folder; nil when there is none.
func ReadPaseo(home string, env agentdirs.Env) *platform.PaseoScan {
	base := PaseoHome(home, env)
	if info, err := os.Stat(base); err != nil || !info.IsDir() {
		return nil
	}
	p := &platform.PaseoScan{Home: base, ConfigPath: filepath.Join(base, "config.json")}
	var cfg paseoConfig
	p.ConfigStatus, p.Reason = readSmallJSON(p.ConfigPath, &cfg)
	if info, err := os.Lstat(p.ConfigPath); err == nil {
		p.ConfigMode, p.ConfigOwner = fileModeOwner(info)
	}
	if p.ConfigStatus == platform.MCPSourceRead {
		p.Listen = strings.TrimSpace(cfg.Daemon.Listen)
		for ptr, set := range map[string]filled{
			"/features/dictation/stt/apiKey": cfg.Features.Dictation.STT.APIKey,
			"/features/voiceMode/stt/apiKey": cfg.Features.VoiceMode.STT.APIKey,
			"/features/voiceMode/tts/apiKey": cfg.Features.VoiceMode.TTS.APIKey,
		} {
			if set {
				p.APIKeys = append(p.APIKeys, ptr)
			}
		}
		sort.Strings(p.APIKeys)
	}
	agents := filepath.Join(base, "agents")
	projects, _ := os.ReadDir(agents)
	for _, d := range projects {
		if !d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(agents, d.Name()))
		for _, e := range files {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			a := platform.PaseoAgent{Path: filepath.Join(agents, d.Name(), e.Name())}
			var f paseoAgentFile
			a.Status, a.Reason = readSmallJSON(a.Path, &f)
			if a.Status == platform.MCPSourceRead {
				a.Provider, a.Cwd, a.Archived = f.Provider, f.Cwd, f.ArchivedAt != nil && *f.ArchivedAt != ""
				switch {
				case f.LastModeID != nil && *f.LastModeID != "":
					a.Mode = *f.LastModeID
				case f.Config != nil && f.Config.ModeID != nil:
					a.Mode = *f.Config.ModeID
				}
			}
			p.Agents = append(p.Agents, a)
		}
	}
	scheds, _ := os.ReadDir(filepath.Join(base, "schedules"))
	for _, e := range scheds {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		s := platform.PaseoSchedule{Path: filepath.Join(base, "schedules", e.Name())}
		var f paseoScheduleFile
		s.Status, s.Reason = readSmallJSON(s.Path, &f)
		if s.Status == platform.MCPSourceRead {
			s.ScheduleStatus = f.Status
			if f.Target.Type == "new-agent" {
				s.Provider = f.Target.Config.Provider
				if f.Target.Config.ModeID != nil {
					s.Mode = *f.Target.Config.ModeID
				}
			}
		}
		p.Schedules = append(p.Schedules, s)
	}
	var projs []struct {
		RootPath   string  `json:"rootPath"`
		ArchivedAt *string `json:"archivedAt"`
	}
	if st, _ := readSmallJSON(filepath.Join(base, "projects", "projects.json"), &projs); st == platform.MCPSourceRead {
		for _, pr := range projs {
			if pr.RootPath != "" && (pr.ArchivedAt == nil || *pr.ArchivedAt == "") {
				p.Projects = append(p.Projects, pr.RootPath)
			}
		}
	}
	return p
}

// --- Claude Squad --------------------------------------------------------

// ReadClaudeSquad reads ~/.claude-squad; nil when there is none.
func ReadClaudeSquad(home string) *platform.ClaudeSquadScan {
	dir := filepath.Join(home, ".claude-squad")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil
	}
	c := &platform.ClaudeSquadScan{
		Dir: dir, ConfigPath: filepath.Join(dir, "config.json"),
		StatePath: filepath.Join(dir, "state.json"), DaemonPIDPath: filepath.Join(dir, "daemon.pid"),
	}
	var cfg struct {
		DefaultProgram string `json:"default_program"`
		AutoYes        bool   `json:"auto_yes"`
		Profiles       []struct {
			Program string `json:"program"`
		} `json:"profiles"`
	}
	c.ConfigStatus, c.Reason = readSmallJSON(c.ConfigPath, &cfg)
	if c.ConfigStatus == platform.MCPSourceRead {
		c.AutoYes, c.DefaultProgram = cfg.AutoYes, strings.TrimSpace(cfg.DefaultProgram)
		for _, p := range cfg.Profiles {
			if p.Program != "" {
				c.Profiles = append(c.Profiles, strings.TrimSpace(p.Program))
			}
		}
	}
	// Of state.json only each instance's program and auto_yes: never
	// diff_stats, the full diff that may carry what an agent wrote.
	var st struct {
		Instances []struct {
			Program string `json:"program"`
			AutoYes bool   `json:"auto_yes"`
		} `json:"instances"`
	}
	if s, reason := readSmallJSON(c.StatePath, &st); s == platform.MCPSourceRead {
		for _, in := range st.Instances {
			c.Instances = append(c.Instances, platform.ClaudeSquadInstance{Program: strings.TrimSpace(in.Program), AutoYes: in.AutoYes})
		}
	} else if s == platform.MCPSourceFailed {
		c.StateError = reason
	}
	if _, err := os.Lstat(c.DaemonPIDPath); err == nil {
		c.DaemonPID = true
	}
	return c
}

// --- Orca ----------------------------------------------------------------

// orcaHookFiles are the configuration files of agents RosmarOps knows in
// which Orca installs its hooks (managed-agent-hook-registry.ts at v1.4.266).
func orcaHookFiles(home, claudeDir string) map[string]string {
	return map[string]string{
		filepath.Join(claudeDir, "settings.json"):       "claude-code",
		filepath.Join(home, ".gemini", "settings.json"): agentdirs.GeminiCLI,
		filepath.Join(home, ".codex", "hooks.json"):     agentdirs.Codex,
		filepath.Join(home, ".cursor", "hooks.json"):    "cursor",
	}
}

// ReadOrca reports Orca when its userData folder or ~/.orca exists, and the
// agent configuration files that hold one of its hook commands. Neither
// profile-state.db nor anything in ~/.orca is opened.
func ReadOrca(l agentdirs.Layout, home, claudeDir string, env agentdirs.Env) *platform.OrcaScan {
	o := &platform.OrcaScan{}
	if d := l.OrcaUserData(home, env); isDirNoLink(d) {
		o.UserData = d
	}
	if d := filepath.Join(home, ".orca"); isDirNoLink(d) {
		o.Home = d
	}
	if o.UserData == "" && o.Home == "" {
		return nil
	}
	for path, agent := range orcaHookFiles(home, claudeDir) {
		var doc any
		if st, _ := readSmallJSON(path, &doc); st != platform.MCPSourceRead {
			continue
		}
		if hasOrcaCommand(doc) {
			o.Hooks = append(o.Hooks, platform.OrcaHook{Agent: agent, ConfigPath: path})
		}
	}
	sort.Slice(o.Hooks, func(i, j int) bool { return o.Hooks[i].ConfigPath < o.Hooks[j].ConfigPath })
	return o
}

func isDirNoLink(p string) bool {
	info, err := os.Lstat(p)
	return err == nil && info.IsDir()
}

// hasOrcaCommand reports a "command" string anywhere in doc that runs one of
// Orca's hook scripts (managed-hook-command-matching.ts:9-32 at v1.4.266):
// a path under .orca/agent-hooks/, also inside a PowerShell -EncodedCommand.
func hasOrcaCommand(v any) bool {
	switch t := v.(type) {
	case map[string]any:
		for k, c := range t {
			if s, ok := c.(string); ok && (k == "command" || k == "statusLine") && isOrcaCommand(s) {
				return true
			}
			if hasOrcaCommand(c) {
				return true
			}
		}
	case []any:
		for _, c := range t {
			if hasOrcaCommand(c) {
				return true
			}
		}
	}
	return false
}

func isOrcaCommand(s string) bool {
	norm := func(x string) string { return strings.ToLower(strings.ReplaceAll(x, `\`, "/")) }
	if strings.Contains(norm(s), ".orca/agent-hooks/") {
		return true
	}
	fields := strings.Fields(s)
	for i, f := range fields {
		if strings.EqualFold(f, "-EncodedCommand") && i+1 < len(fields) {
			if dec, ok := decodePowerShell(fields[i+1]); ok && strings.Contains(norm(dec), ".orca/agent-hooks/") {
				return true
			}
		}
	}
	return false
}

// decodePowerShell decodes an -EncodedCommand argument: base64 of UTF-16LE.
func decodePowerShell(arg string) (string, bool) {
	raw, err := base64.StdEncoding.DecodeString(strings.Trim(arg, `"'`))
	if err != nil || len(raw)%2 != 0 {
		return "", false
	}
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
	}
	return string(utf16.Decode(u)), true
}
