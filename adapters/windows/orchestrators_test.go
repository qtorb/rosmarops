package windows

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/platform"
)

const orchSecret = "sk-proj-ORCHSECRETNEVERKEPT0123456789abcdef"

func put(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func noSecret(t *testing.T, v any) {
	t.Helper()
	b, _ := json.Marshal(v)
	if strings.Contains(string(b), "ORCHSECRET") {
		t.Fatalf("a secret value reached the scan: %s", b)
	}
}

func TestReadPaseo(t *testing.T) {
	home := t.TempDir()
	base := filepath.Join(home, ".paseo")
	put(t, filepath.Join(base, "config.json"), `{"version":1,"daemon":{"listen":"0.0.0.0:6767","auth":{"password":"$2b$10$hash"}},
		"features":{"dictation":{"stt":{"provider":"openai","apiKey":"`+orchSecret+`"}},"voiceMode":{"tts":{"provider":"openai","apiKey":" "}}},
		"agents":{"providers":{"x":{"env":{"TOKEN":"`+orchSecret+`"}}}}}`)
	put(t, filepath.Join(base, "agents", "home-u-repo", "a1.json"), `{"id":"a1","provider":"claude","cwd":"/home/u/repo","lastModeId":"bypassPermissions","config":{"modeId":"default"},"launchEnv":{"OPENAI_API_KEY":"`+orchSecret+`"}}`)
	put(t, filepath.Join(base, "agents", "home-u-repo", "a2.json"), `{"id":"a2","provider":"codex","cwd":"/home/u/repo","config":{"modeId":"full-access"},"archivedAt":"2026-11-01T00:00:00Z"}`)
	put(t, filepath.Join(base, "agents", "home-u-repo", "bad.json"), `{`)
	put(t, filepath.Join(base, "schedules", "s1.json"), `{"id":"s1","prompt":"`+orchSecret+`","cadence":{"type":"cron","expression":"0 9 * * *"},"target":{"type":"new-agent","config":{"provider":"claude","cwd":"/r","modeId":"bypassPermissions"}},"status":"active"}`)
	put(t, filepath.Join(base, "schedules", "s2.json"), `{"id":"s2","target":{"type":"agent","agentId":"a1"},"status":"paused"}`)
	put(t, filepath.Join(base, "projects", "projects.json"), `[{"projectId":"p","rootPath":"/home/u/repo","kind":"git"},{"projectId":"q","rootPath":"/old","archivedAt":"x"}]`)
	put(t, filepath.Join(base, "daemon-keypair.json"), `{"secretKey":"`+orchSecret+`"}`)

	p := ReadPaseo(home, func(string) (string, bool) { return "", false })
	if p == nil || p.ConfigStatus != platform.MCPSourceRead {
		t.Fatalf("paseo %+v", p)
	}
	noSecret(t, p)
	if p.Listen != "0.0.0.0:6767" || strings.Join(p.APIKeys, ",") != "/features/dictation/stt/apiKey" {
		t.Errorf("config: listen %q keys %v (a blank key is no key)", p.Listen, p.APIKeys)
	}
	modes := map[string]platform.PaseoAgent{}
	for _, a := range p.Agents {
		modes[filepath.Base(a.Path)] = a
	}
	if a := modes["a1.json"]; a.Provider != "claude" || a.Mode != "bypassPermissions" || a.Archived {
		t.Errorf("lastModeId wins over config.modeId: %+v", a)
	}
	if a := modes["a2.json"]; a.Mode != "full-access" || !a.Archived {
		t.Errorf("a2 %+v", a)
	}
	if a := modes["bad.json"]; a.Status != platform.MCPSourceFailed {
		t.Errorf("bad %+v", a)
	}
	if len(p.Schedules) != 2 || p.Schedules[0].Mode != "bypassPermissions" || p.Schedules[0].ScheduleStatus != "active" || p.Schedules[1].Provider != "" {
		t.Errorf("schedules %+v", p.Schedules)
	}
	if strings.Join(p.Projects, ",") != "/home/u/repo" {
		t.Errorf("projects %v", p.Projects)
	}

	other := t.TempDir()
	put(t, filepath.Join(other, "config.json"), `{}`)
	if q := ReadPaseo(home, func(k string) (string, bool) { return other, k == "PASEO_HOME" }); q == nil || q.Home != other {
		t.Errorf("PASEO_HOME: %+v", q)
	}
	if ReadPaseo(t.TempDir(), func(string) (string, bool) { return "", false }) != nil {
		t.Error("no ~/.paseo is no Paseo")
	}
}

func TestReadClaudeSquad(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".claude-squad")
	put(t, filepath.Join(dir, "config.json"), `{"default_program":"claude","auto_yes":false,"daemon_poll_interval":1000,"branch_prefix":"u/","profiles":[{"name":"c","program":"codex"}]}`)
	put(t, filepath.Join(dir, "state.json"), `{"help_screens_seen":0,"instances":[{"title":"t","program":"claude","auto_yes":true,"diff_stats":{"added":1,"content":"+KEY=`+orchSecret+`"}}]}`)
	put(t, filepath.Join(dir, "daemon.pid"), "123")
	c := ReadClaudeSquad(home)
	if c == nil || c.ConfigStatus != platform.MCPSourceRead {
		t.Fatalf("squad %+v", c)
	}
	noSecret(t, c)
	if c.AutoYes || c.DefaultProgram != "claude" || strings.Join(c.Profiles, ",") != "codex" || !c.DaemonPID {
		t.Errorf("config %+v", c)
	}
	if len(c.Instances) != 1 || c.Instances[0].Program != "claude" || !c.Instances[0].AutoYes {
		t.Errorf("instances %+v", c.Instances)
	}
	if ReadClaudeSquad(t.TempDir()) != nil {
		t.Error("no ~/.claude-squad is no Claude Squad")
	}
}

func TestReadOrca(t *testing.T) {
	home := t.TempDir()
	claude := filepath.Join(home, ".claude")
	env := func(k string) (string, bool) {
		if k == "XDG_CONFIG_HOME" {
			return filepath.Join(home, ".config"), true
		}
		return "", false
	}
	if ReadOrca(agentdirs.LinuxLayout, home, claude, env) != nil {
		t.Fatal("no Orca folder is no Orca")
	}
	put(t, filepath.Join(home, ".config", "Orca", "profiles", "p", "profile-state.db"), orchSecret)
	put(t, filepath.Join(claude, "settings.json"), `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"\"${HOME-}/.orca/agent-hooks/claude-hook.sh\""}]}]}}`)
	enc := func(s string) string {
		u := utf16.Encode([]rune(s))
		b := make([]byte, 2*len(u))
		for i, r := range u {
			b[2*i], b[2*i+1] = byte(r), byte(r>>8)
		}
		return base64.StdEncoding.EncodeToString(b)
	}
	put(t, filepath.Join(home, ".gemini", "settings.json"), `{"hooks":{"AfterAgent":[{"hooks":[{"type":"command","command":"powershell -NoProfile -EncodedCommand `+enc(`& (Join-Path $env:USERPROFILE '.orca\agent-hooks\gemini-hook.cmd')`)+`"}]}]}}`)
	put(t, filepath.Join(home, ".cursor", "hooks.json"), `{"hooks":{"stop":[{"command":"./mine.sh"}]}}`)
	o := ReadOrca(agentdirs.LinuxLayout, home, claude, env)
	if o == nil || o.UserData != filepath.Join(home, ".config", "Orca") || o.Home != "" {
		t.Fatalf("orca %+v", o)
	}
	noSecret(t, o)
	var agents []string
	for _, h := range o.Hooks {
		agents = append(agents, h.Agent)
	}
	if strings.Join(agents, ",") != "claude-code,gemini-cli" {
		t.Errorf("hooks %+v (Cursor's own hook is not Orca's)", o.Hooks)
	}
}
