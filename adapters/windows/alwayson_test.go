package windows_test

import (
	"path/filepath"
	"testing"

	windows "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
)

// BV-251 (CTO-53 §1–§3, CTO-62 §1): Hermes Agent's gateway platforms and
// approvers, read without keeping any token; a gateway outside the YAML
// subset leaves the rest of config.yaml read.
func TestReadHermesGateway(t *testing.T) {
	home := t.TempDir()
	hermes := filepath.Join(home, "hermes")
	writeFile(t, filepath.Join(hermes, "config.yaml"), `approvals:
  mode: smart
  approvers:
    - "123"
    - "456"
gateway:
  platforms:
    telegram:
      enabled: true
      token: "1234567890:AAEXAMPLEEXAMPLEEXAMPLEEXAMPLEEXAMP"
    Discord:
      enabled: false
    slack:
      home_channel: general
`)
	scan := windows.ReadHermes(home, hermesEnv(hermes))
	if len(scan.Configs) != 1 {
		t.Fatalf("configs: %+v", scan.Configs)
	}
	c := scan.Configs[0]
	if c.Status != platform.MCPSourceRead || c.GatewayUnread {
		t.Fatalf("config not read: %+v", c)
	}
	want := []platform.HermesPlatform{{Name: "discord", Enabled: false, EnabledSet: true}, {Name: "slack"}, {Name: "telegram", Enabled: true, EnabledSet: true}}
	if len(c.GatewayPlatforms) != len(want) {
		t.Fatalf("platforms %+v", c.GatewayPlatforms)
	}
	for i := range want {
		if c.GatewayPlatforms[i] != want[i] {
			t.Errorf("platform %d: %+v, want %+v", i, c.GatewayPlatforms[i], want[i])
		}
	}
	if !c.ApproversSet || c.ApproversAnyone || c.ApproversCount != 2 {
		t.Errorf("approvers: set %v anyone %v count %d", c.ApproversSet, c.ApproversAnyone, c.ApproversCount)
	}

	writeFile(t, filepath.Join(hermes, "config.yaml"), `approvals:
  mode: smart
gateway:
  platforms: {telegram: {enabled: true}}
`)
	c = windows.ReadHermes(home, hermesEnv(hermes)).Configs[0]
	if c.Status != platform.MCPSourceRead || !c.GatewayUnread || c.ApprovalMode != "smart" || c.ApproversSet {
		t.Errorf("gateway outside the subset: %+v", c)
	}
}

// BV-251: Claude Code's channels (launch_flag_required, access.json counts,
// never the .env), its scheduled tasks; Cline's connectors by name and its
// schedules folder; Hermes Agent's cron jobs (enabled, deliver platform), and
// memory folders by metadata.
func TestReadAlwaysOn(t *testing.T) {
	home := t.TempDir()
	claude := filepath.Join(home, ".claude")
	repo := filepath.Join(home, "repo")
	writeFile(t, filepath.Join(claude, "channels", "telegram", ".env"), "TELEGRAM_BOT_TOKEN=x\n")
	writeFile(t, filepath.Join(claude, "channels", "telegram", "access.json"), `{"dmPolicy":"allowlist","allowFrom":["1","2","3"],"pending":{"abc":"x"}}`)
	writeFile(t, filepath.Join(claude, "channels", "discord", ".env"), "")
	writeFile(t, filepath.Join(repo, ".claude", "scheduled_tasks.json"), `[{"prompt":"secret"},{"prompt":"two"}]`)
	writeFile(t, filepath.Join(claude, "projects", "p1", "memory", "MEMORY.md"), "12345")
	writeFile(t, filepath.Join(claude, "settings.json"), `{"autoMemoryDirectory":"~/mem","permissions":{}}`)
	writeFile(t, filepath.Join(home, "mem", "x.md"), "123")
	data := filepath.Join(home, ".cline", "data")
	writeFile(t, filepath.Join(data, "connectors", "telegram", "mybot.json"), `{"botUsername":"x"}`)
	writeFile(t, filepath.Join(home, ".cline", "schedules", "x"), "")
	writeFile(t, filepath.Join(repo, "memory-bank", "a.md"), "ab")
	hermes := filepath.Join(home, ".hermes")
	writeFile(t, filepath.Join(hermes, "cron", "jobs.json"), `{"jobs":[{"enabled":true,"schedule":{"kind":"cron"},"deliver":"telegram:-100123","prompt":"secret"},{"enabled":false,"deliver":"local"}]}`)
	none := agentdirs.Env(func(string) (string, bool) { return "", false })
	user := windows.ReadClaudeSettings(filepath.Join(claude, "settings.json"), platform.MCPScopeUser, "", claude)
	scan := windows.ReadAlwaysOn(agentdirs.LinuxLayout, home, claude, []string{repo}, none, platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{user}})

	byPlat := map[string]platform.InboundChannel{}
	for _, c := range scan.Channels {
		byPlat[c.Product+"/"+c.Platform] = c
	}
	tg := byPlat[knowledge.ClaudeCodeProduct+"/telegram"]
	if tg.Status != platform.ChannelLaunchFlagRequired || tg.LaunchFlag != "--channels" || tg.Approvers != platform.ApproversListed || tg.AllowlistCount == nil || *tg.AllowlistCount != 3 {
		t.Errorf("claude telegram: %+v", tg)
	}
	if d := byPlat[knowledge.ClaudeCodeProduct+"/discord"]; d.Approvers != platform.ApproversListed || d.AllowlistCount == nil || *d.AllowlistCount != 0 {
		t.Errorf("claude discord without access.json (pairing, empty list): %+v", d)
	}
	if c := byPlat[knowledge.ClineProduct+"/telegram"]; c.Status != platform.ChannelConfigured || c.Approvers != platform.ApproversUnknown {
		t.Errorf("cline telegram: %+v", c)
	}
	sched := map[string]platform.AgentSchedule{}
	for _, s := range scan.Schedules {
		sched[s.Product] = s
	}
	if s := sched[knowledge.ClaudeCodeProduct]; s.Tasks == nil || *s.Tasks != 2 {
		t.Errorf("claude tasks: %+v", s)
	}
	if s := sched[knowledge.ClineProduct]; s.Tasks != nil || s.Path == "" {
		t.Errorf("cline schedules (not read): %+v", s)
	}
	if s := sched[knowledge.HermesProduct]; s.Tasks == nil || *s.Tasks != 2 || *s.Active != 1 || len(s.Destinations) != 2 || s.Destinations[0] != "local" || s.Destinations[1] != "telegram" {
		t.Errorf("hermes cron: %+v", s)
	}
	mem := map[string]platform.AgentMemory{}
	claudeMem := 0
	for _, m := range scan.Memory {
		mem[m.Product] = m
		if m.Product == knowledge.ClaudeCodeProduct {
			claudeMem++
			if m.Path == filepath.Join(home, "mem") && (m.Files != 1 || m.Bytes != 3) {
				t.Errorf("autoMemoryDirectory: %+v", m)
			}
		}
	}
	if claudeMem != 2 {
		t.Errorf("claude memory folders: %d, want 2 (projects/p1/memory and autoMemoryDirectory)", claudeMem)
	}
	if m := mem[knowledge.ClineProduct]; m.Files != 1 || m.Bytes != 2 || !m.InRepository {
		t.Errorf("cline memory bank: %+v", m)
	}
	if len(scan.Failed) != 0 {
		t.Errorf("failed: %+v", scan.Failed)
	}
}

// BV-310 C2: autoMemoryDirectory is read in every settings scope read (user,
// project, local, managed), and autoMemoryEnabled false marks the memory it
// applies to as off: the managed settings first, then the project's local and
// project settings, then the user's.
func TestReadAlwaysOnAutoMemorySettings(t *testing.T) {
	home := t.TempDir()
	claude := filepath.Join(home, ".claude")
	on, off := filepath.Join(home, "on"), filepath.Join(home, "off")
	none := agentdirs.Env(func(string) (string, bool) { return "", false })
	for _, d := range []string{on, off} {
		writeFile(t, filepath.Join(claude, "projects", windows.ClaudeProjectDirName(d), "memory", "MEMORY.md"), "1")
	}
	writeFile(t, filepath.Join(claude, "projects", "-somewhere-else", "memory", "MEMORY.md"), "1")
	writeFile(t, filepath.Join(home, "proj-mem", "a.md"), "1")
	writeFile(t, filepath.Join(home, "local-mem", "a.md"), "1")
	writeFile(t, filepath.Join(home, "managed-mem", "a.md"), "1")
	writeFile(t, filepath.Join(claude, "settings.json"), `{"autoMemoryEnabled":false}`)
	writeFile(t, filepath.Join(on, ".claude", "settings.json"), `{"autoMemoryEnabled":true,"autoMemoryDirectory":"~/proj-mem"}`)
	writeFile(t, filepath.Join(off, ".claude", "settings.json"), `{"autoMemoryEnabled":true}`)
	writeFile(t, filepath.Join(off, ".claude", "settings.local.json"), `{"autoMemoryEnabled":false,"autoMemoryDirectory":"~/local-mem"}`)
	writeFile(t, filepath.Join(home, "managed.json"), `{"autoMemoryDirectory":"~/managed-mem"}`)
	read := func(path, scope, root string) platform.ClaudeSettingsFile {
		return windows.ReadClaudeSettings(path, scope, root, claude)
	}
	settings := platform.ClaudeSettingsScan{
		Files: []platform.ClaudeSettingsFile{
			read(filepath.Join(claude, "settings.json"), platform.MCPScopeUser, ""),
			read(filepath.Join(on, ".claude", "settings.json"), platform.MCPScopeProject, on),
			read(filepath.Join(on, ".claude", "settings.local.json"), platform.MCPScopeLocal, on),
			read(filepath.Join(off, ".claude", "settings.json"), platform.MCPScopeProject, off),
			read(filepath.Join(off, ".claude", "settings.local.json"), platform.MCPScopeLocal, off),
		},
		Managed: []platform.ManagedSource{{Kind: platform.ManagedFiles, Files: []platform.ClaudeSettingsFile{
			read(filepath.Join(home, "managed.json"), platform.MCPScopeManaged, ""),
		}}},
	}
	scan := windows.ReadAlwaysOn(agentdirs.LinuxLayout, home, claude, []string{on, off}, none, settings)
	got := map[string]bool{}
	for _, m := range scan.Memory {
		if m.Product == knowledge.ClaudeCodeProduct {
			got[m.Path] = m.AutoMemoryOff
		}
	}
	want := map[string]bool{
		filepath.Join(claude, "projects", windows.ClaudeProjectDirName(on), "memory"):  false, // the project turns it on
		filepath.Join(claude, "projects", windows.ClaudeProjectDirName(off), "memory"): true,  // its local settings turn it off
		filepath.Join(claude, "projects", "-somewhere-else", "memory"):                 false, // no known project: a project turns it on
		filepath.Join(home, "proj-mem"):                                                false, // project scope
		filepath.Join(home, "local-mem"):                                               true,  // local scope, off there
		filepath.Join(home, "managed-mem"):                                             false, // managed scope
	}
	if len(got) != len(want) {
		t.Fatalf("claude memory: %v, want %v", got, want)
	}
	for p, w := range want {
		if v, ok := got[p]; !ok || v != w {
			t.Errorf("%s: off %v (present %v), want off %v", p, v, ok, w)
		}
	}

	// Off in the user settings and turned on in no project: every memory is off.
	settings.Files = settings.Files[:1]
	settings.Managed = nil
	for _, m := range windows.ReadAlwaysOn(agentdirs.LinuxLayout, home, claude, []string{on, off}, none, settings).Memory {
		if m.Product == knowledge.ClaudeCodeProduct && !m.AutoMemoryOff {
			t.Errorf("%s: on, want off (autoMemoryEnabled false in the user settings)", m.Path)
		}
	}
	// A managed setting wins over the project's.
	writeFile(t, filepath.Join(home, "managed.json"), `{"autoMemoryEnabled":false}`)
	settings.Files = []platform.ClaudeSettingsFile{read(filepath.Join(on, ".claude", "settings.json"), platform.MCPScopeProject, on)}
	settings.Managed = []platform.ManagedSource{{Kind: platform.ManagedFiles, Files: []platform.ClaudeSettingsFile{read(filepath.Join(home, "managed.json"), platform.MCPScopeManaged, "")}}}
	for _, m := range windows.ReadAlwaysOn(agentdirs.LinuxLayout, home, claude, []string{on}, none, settings).Memory {
		if m.Product == knowledge.ClaudeCodeProduct && !m.AutoMemoryOff {
			t.Errorf("%s: on, want off (managed autoMemoryEnabled false)", m.Path)
		}
	}
}
