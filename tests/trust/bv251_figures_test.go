package trust_test

// BV-251 (CTO-62 §2.3, option A, Albert 17-11): with one wake source on,
// each agent alone on R2 scores as CTO-62 measured, and stays red and D.
//   - Hermes Agent with its gateway (approvers «*», the default): 29;
//   - Hermes Agent with only cron tasks: 31;
//   - Claude Code with a channel (access.json list; the --channels launch not
//     seen): 29;
//   - Cline with a Telegram connector (--allowed-user-id not seen): 26.
// R2 and R2-todos at factory do not change (TestR2FactoryScores and the
// figures tests of BV-248/BV-249 pin them).

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/profiles"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

func bv251Hermes(t *testing.T, gateway bool, ao platform.AlwaysOnScan) (report.V2, func(*pipeline.Input)) {
	t.Helper()
	hd := fcHome + "/.hermes"
	cfg := platform.HermesConfig{Path: hd + "/config.yaml", ConfigDir: hd, Status: platform.MCPSourceRead, ApprovalMode: "smart", TerminalBackend: "local"}
	if gateway {
		cfg.GatewayPlatforms = []platform.HermesPlatform{{Name: "telegram", Enabled: true, EnabledSet: true}}
	}
	source := platform.MCPSource{Platform: "hermes", ConfigPath: cfg.Path, ConfigDir: cfg.ConfigDir, Status: platform.MCPSourceRead, LoadsMCP: true}
	hermes := platform.HermesScan{Configs: []platform.HermesConfig{cfg}, Sources: []platform.MCPSource{source}}
	settings := r2Settings()
	g := r2Assemble(t, pipeline.Input{
		MCP:          platform.MCPScan{Sources: hermes.Sources},
		Settings:     settings,
		Hermes:       hermes,
		CredFiles:    platform.CredentialFileScan{Entries: r2Creds()},
		Instructions: platform.InstructionScan{Files: r2Instr("hermes", hd)},
		AlwaysOn:     ao,
	})
	mod := func(all *pipeline.Input) {
		for i := range all.Hermes.Configs {
			all.Hermes.Configs[i].GatewayPlatforms = cfg.GatewayPlatforms
		}
		all.AlwaysOn.Channels = append(all.AlwaysOn.Channels, ao.Channels...)
		all.AlwaysOn.Schedules = append(all.AlwaysOn.Schedules, ao.Schedules...)
	}
	return r2BuildV2(t, &g, &settings, func(in *report.V2Input) { in.Hermes = hermes }), mod
}

func bv251Claude(t *testing.T, ao platform.AlwaysOnScan) (report.V2, func(*pipeline.Input)) {
	t.Helper()
	settings := r2Settings(platform.ClaudeSettingsFile{Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead})
	g := r2Assemble(t, pipeline.Input{
		Settings:     settings,
		CredFiles:    platform.CredentialFileScan{Entries: r2Creds()},
		Instructions: platform.InstructionScan{Files: r2Instr("claude-code", fcClaudeDir)},
		AlwaysOn:     ao,
	})
	mod := func(all *pipeline.Input) { all.AlwaysOn.Channels = append(all.AlwaysOn.Channels, ao.Channels...) }
	return r2BuildV2(t, &g, &settings, nil), mod
}

func bv251Cline(t *testing.T, ao platform.AlwaysOnScan) (report.V2, func(*pipeline.Input)) {
	t.Helper()
	scan := platform.ClineScan{ConfigDir: fcHome + "/.cline/data", Present: true, GlobalStateStatus: platform.MCPSourceRead, HooksEnabled: true, MigrationVersion: 3}
	settings := r2Settings()
	g := r2Assemble(t, pipeline.Input{
		Settings:     settings,
		Cline:        scan,
		Extensions:   []platform.VSCodeExtension{{ID: "saoudrizwan.claude-dev"}},
		CredFiles:    platform.CredentialFileScan{Entries: r2Creds()},
		Instructions: platform.InstructionScan{Files: r2Instr("cline", fcHome+"/.cline/data")},
		AlwaysOn:     ao,
	})
	mod := func(all *pipeline.Input) { all.AlwaysOn.Channels = append(all.AlwaysOn.Channels, ao.Channels...) }
	return r2BuildV2(t, &g, &settings, func(in *report.V2Input) { in.Cline = scan }), mod
}

func bv251Findings(v report.V2, product string) map[string]int {
	out := map[string]int{}
	id := overviewByProduct(v, product).AgentID
	for _, f := range v.Findings {
		if len(f.Evidence.Nodes) > 0 && f.Evidence.Nodes[0] == id {
			out[f.Rule]++
		}
	}
	return out
}

func TestBV251_ActsWithoutYouFigures(t *testing.T) {
	two := 2
	hd := fcHome + "/.hermes"
	cases := []struct {
		name    string
		product string
		build   func(*testing.T) (report.V2, func(*pipeline.Input))
		score   int
		scope   string
	}{
		{"Hermes with its gateway (approvers *)", knowledge.HermesProduct, func(t *testing.T) (report.V2, func(*pipeline.Input)) {
			return bv251Hermes(t, true, platform.AlwaysOnScan{})
		}, 29, rules.WakeAnyone},
		{"Hermes with only cron tasks", knowledge.HermesProduct, func(t *testing.T) (report.V2, func(*pipeline.Input)) {
			n := 1
			return bv251Hermes(t, false, platform.AlwaysOnScan{Schedules: []platform.AgentSchedule{{
				Product: knowledge.HermesProduct, ConfigDir: hd, Path: hd + "/cron/jobs.json", Tasks: &n, Active: &n,
			}}})
		}, 31, rules.WakeTasks},
		{"Claude Code with a channel (access.json list)", knowledge.ClaudeCodeProduct, func(t *testing.T) (report.V2, func(*pipeline.Input)) {
			return bv251Claude(t, platform.AlwaysOnScan{Channels: []platform.InboundChannel{{
				Product: knowledge.ClaudeCodeProduct, ConfigDir: fcClaudeDir, Path: fcClaudeDir + "/channels/telegram", Platform: "telegram",
				Status: platform.ChannelLaunchFlagRequired, Origin: platform.ChannelOriginConfig, LaunchFlag: "--channels",
				Approvers: platform.ApproversListed, AllowlistCount: &two,
			}}})
		}, 29, rules.WakeListed},
		{"Cline with a Telegram connector", knowledge.ClineProduct, func(t *testing.T) (report.V2, func(*pipeline.Input)) {
			data := fcHome + "/.cline/data"
			return bv251Cline(t, platform.AlwaysOnScan{Channels: []platform.InboundChannel{{
				Product: knowledge.ClineProduct, ConfigDir: data, Path: data + "/connectors/telegram", Platform: "telegram",
				Status: platform.ChannelConfigured, Origin: platform.ChannelOriginConfig, Approvers: platform.ApproversUnknown,
			}}})
		}, 26, rules.WakeUnknown},
	}
	for _, c := range cases {
		v, mod := c.build(t)
		got, band, _ := r2AgentIndexID(t, v, c.product)
		a := classOfSingle(t, v, c.product, mod)
		var acts *rules.Finding
		for i := range v.Findings {
			if v.Findings[i].Rule == rules.RuleActsWithoutYou {
				acts = &v.Findings[i]
			}
		}
		t.Logf("%s: %d (%s) class %s F %v; findings %v", c.name, got, band, a.Class, ptrF(a.F), bv251Findings(v, c.product))
		if acts == nil {
			t.Errorf("%s: no acts_without_you finding", c.name)
		} else if acts.Params.Scope != c.scope || acts.Severity != rules.SeverityYellow {
			t.Errorf("%s: acts_without_you %s/%s, want %s yellow", c.name, acts.Params.Scope, acts.Severity, c.scope)
		}
		if got != c.score || band != "exposed" || a.Class != "D" {
			t.Errorf("%s: %d (%s) class %s, want %d exposed D", c.name, got, band, a.Class, c.score)
		}
	}
}

// Without a wake source, nothing changes: R2's factory figures are pinned by
// TestR2FactoryScores; here the same builders give no acts_without_you and
// only_you.
func TestBV251_NoSourceNoFinding(t *testing.T) {
	v, _ := bv251Hermes(t, false, platform.AlwaysOnScan{})
	for _, f := range v.Findings {
		if f.Rule == rules.RuleActsWithoutYou {
			t.Fatalf("acts_without_you without a wake source: %+v", f)
		}
	}
	o := overviewByProduct(v, knowledge.HermesProduct).Orders
	if o == nil || o.Value != rules.OrdersOnlyYou {
		t.Errorf("Hermes orders %+v, want only_you", o)
	}
	_ = graph.EdgeWakes
}

// CTO-64 §2 (advisor 17-11): with Claude Code's sandbox effective and
// autoAllowBashIfSandboxed not written, the runs row carries
// sandbox_autoallow_default_by_version; written (P2 writes false), it does
// not. Score and class do not change.
func TestBV251_AutoAllowByVersion(t *testing.T) {
	prev := graph.HostOS
	defer func() { graph.HostOS = prev }()
	graph.HostOS = "linux"
	carries := func(v report.V2) bool {
		for _, c := range overviewByProduct(v, knowledge.ClaudeCodeProduct).Rows {
			if c.Row == rules.RowRunsCommands {
				for _, a := range c.Data.Assumptions {
					if a == rules.AssumeSandboxAutoAllowByVersion {
						return true
					}
				}
			}
		}
		return false
	}
	written := bv252Claude(t, nil, profiles.P2, nil)
	unwritten := bv252Claude(t, nil, profiles.P2, func(s *platform.ClaudeSandbox, _ *platform.ClaudePermissions) {
		s.AutoAllowBash, s.AutoAllowBashSet = false, false
	})
	factory := bv252Claude(t, nil, "", nil)
	if carries(written) || carries(factory) {
		t.Errorf("assumption with the key written (P2) or without a sandbox (factory)")
	}
	if !carries(unwritten) {
		t.Errorf("no assumption with an effective sandbox and the key not written")
	}
	s1, b1, _ := r2AgentIndexID(t, factory, knowledge.ClaudeCodeProduct)
	if s1 != 31 || b1 != "exposed" {
		t.Errorf("factory Claude Code %d %s, want 31 exposed", s1, b1)
	}
}
