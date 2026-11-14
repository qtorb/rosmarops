package trust_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-237 (CTO-50 §5): what the engine reads of the Claude Code and Grok Build
// profiles. Each case is R2's agent with one change; without the change the
// agent is R2's (31 and 32).

// bv237Agent is the overview and index entry of product in v.
func bv237Agent(t *testing.T, v report.V2, product string) (rules.OverviewAgent, int) {
	t.Helper()
	score, _, id := r2AgentIndexID(t, v, product)
	for _, o := range v.AgentsOverview {
		if o.AgentID == id {
			return o, score
		}
	}
	t.Fatalf("%s: no overview", product)
	return rules.OverviewAgent{}, 0
}

func bv237Row(t *testing.T, a rules.OverviewAgent, row string) rules.OverviewCell {
	t.Helper()
	for _, c := range a.Rows {
		if c.Row == row {
			return c
		}
	}
	t.Fatalf("no row %s", row)
	return rules.OverviewCell{}
}

func bv237Kind(t *testing.T, a rules.OverviewAgent, kind string) rules.RiskKindCell {
	t.Helper()
	for _, k := range a.Kinds {
		if k.Kind == kind {
			return k
		}
	}
	t.Fatalf("no kind %s", kind)
	return rules.RiskKindCell{}
}

// bv237Grok is R2's Grok Build with the user layer changed by mod, and Stripe
// loaded from the user layer when stripe is set.
func bv237Grok(t *testing.T, stripe bool, mod func(*platform.GrokLayer)) report.V2 {
	t.Helper()
	l := platform.GrokLayer{Kind: platform.GrokLayerUser, Path: fcGrokCfg, Status: platform.MCPSourceRead}
	if mod != nil {
		mod(&l)
	}
	scan := platform.GrokScan{ConfigDir: fcGrokDir, Exists: true, Layers: []platform.GrokLayer{l}}
	servers := graph.GrokMCPServers(scan)
	if stripe {
		servers = append(servers, r2Stripe("grok-build", fcGrokCfg, fcGrokDir))
	}
	settings := r2Settings()
	g := r2Assemble(t, pipeline.Input{
		MCP:          platform.MCPScan{Servers: servers},
		Settings:     settings,
		Grok:         scan,
		CredFiles:    platform.CredentialFileScan{Entries: r2Creds()},
		Instructions: platform.InstructionScan{Files: r2Instr("grok-build", fcGrokDir)},
	})
	return r2BuildV2(t, &g, &settings, func(in *report.V2Input) { in.Grok = scan })
}

func deny(tool string) platform.GrokRule { return platform.GrokRule{Action: "deny", Tool: tool} }

// TestBV237ClaudeDenyAllMCP (§5.1): a Claude Code deny of mcp__* covers every
// MCP server: Stripe is denied, with no finding; without it, R2's auto mode
// hands the call to the classifier.
func TestBV237ClaudeDenyAllMCP(t *testing.T) {
	t.Parallel()
	services := func(v report.V2) string {
		a, _ := bv237Agent(t, v, "claude-code")
		for _, s := range a.Services {
			if s.Service == "Stripe" {
				return s.Approval
			}
		}
		t.Fatal("no Stripe service")
		return ""
	}
	if got := services(r2ClaudeCodeStripe(t)); got != rules.MCPApprovalAnotherModel {
		t.Errorf("without the deny: %s, want another_model", got)
	}
	t.Run("deny mcp__*", func(t *testing.T) {
		settings := r2Settings(platform.ClaudeSettingsFile{
			Scope: platform.MCPScopeUser, ConfigPath: fcClaudeDir + "/settings.json", ConfigDir: fcClaudeDir, Status: platform.MCPSourceRead,
			Permissions: platform.ClaudePermissions{Deny: []string{"mcp__*"}},
		})
		g := r2Assemble(t, pipeline.Input{
			MCP:          platform.MCPScan{Servers: []platform.MCPServer{r2Stripe("claude-code", fcHome+"/.claude.json", fcClaudeDir)}},
			Settings:     settings,
			CredFiles:    platform.CredentialFileScan{Entries: r2Creds()},
			Instructions: platform.InstructionScan{Files: r2Instr("claude-code", fcClaudeDir)},
		})
		v := r2BuildV2(t, &g, &settings, nil)
		if got := services(v); got != rules.MCPApprovalDeny {
			t.Errorf("deny mcp__*: %s, want deny", got)
		}
		if _, score := bv237Agent(t, v, "claude-code"); score != 31 {
			t.Errorf("deny mcp__*: score %d, want R2's 31 (no Stripe finding)", score)
		}
	})
}

// TestBV237GrokRules (§5, point 3): Grok Build's [permission] rules.
func TestBV237GrokRules(t *testing.T) {
	t.Parallel()
	base, baseScore := bv237Agent(t, bv237Grok(t, false, nil), "grok-build")
	if baseScore != 32 {
		t.Fatalf("R2 Grok %d, want 32", baseScore)
	}
	if d := bv237Row(t, base, rules.RowRunsCommands).Decider; d != rules.DeciderYou {
		t.Errorf("R2 runs: %s", d)
	}

	t.Run("deny edit, bash, webfetch, mcp", func(t *testing.T) {
		v := bv237Grok(t, true, func(l *platform.GrokLayer) {
			l.Rules = []platform.GrokRule{deny("edit"), deny("bash"), deny("webfetch"), deny("mcp")}
		})
		a, _ := bv237Agent(t, v, "grok-build")
		if d := bv237Row(t, a, rules.RowWritesFiles).Decider; d != rules.DeciderBlocked {
			t.Errorf("writes: %s, want blocked", d)
		}
		if d := bv237Row(t, a, rules.RowRunsCommands).Decider; d != rules.DeciderBlocked {
			t.Errorf("runs: %s, want blocked", d)
		}
		// Web search is not a tool of [permission] rules: it still sends out.
		if d := bv237Row(t, a, rules.RowSendsOut).Decider; d != rules.DeciderNobody {
			t.Errorf("sends: %s, want nobody (web search stays open)", d)
		}
		if k := bv237Kind(t, a, rules.KindMoney); k.State != rules.StateBlocked {
			t.Errorf("money: %+v, want blocked (deny mcp)", k)
		}
	})

	t.Run("always-approve with deny edit", func(t *testing.T) {
		open, _ := bv237Agent(t, bv237Grok(t, false, func(l *platform.GrokLayer) {
			l.PermissionMode, l.HasPermissionMode = "always-approve", true
		}), "grok-build")
		closed, _ := bv237Agent(t, bv237Grok(t, false, func(l *platform.GrokLayer) {
			l.PermissionMode, l.HasPermissionMode = "always-approve", true
			l.Rules = []platform.GrokRule{deny("edit")}
		}), "grok-build")
		if bv237Row(t, open, rules.RowWritesFiles).Decider != rules.DeciderNobody || bv237Row(t, closed, rules.RowWritesFiles).Decider != rules.DeciderBlocked {
			t.Errorf("writes: %s without the deny, %s with it", bv237Row(t, open, rules.RowWritesFiles).Decider, bv237Row(t, closed, rules.RowWritesFiles).Decider)
		}
	})

	t.Run("deny WebSearch, compact", func(t *testing.T) {
		a, score := bv237Agent(t, bv237Grok(t, false, func(l *platform.GrokLayer) { l.Deny = []string{"WebSearch"} }), "grok-build")
		if score <= baseScore {
			t.Errorf("deny WebSearch: score %d, want above R2's %d (no egress left)", score, baseScore)
		}
		if d := bv237Row(t, a, rules.RowSendsOut).Decider; d == rules.DeciderNobody {
			t.Errorf("sends: %s, want not nobody", d)
		}
	})

	t.Run("allow bash with a pattern", func(t *testing.T) {
		a, score := bv237Agent(t, bv237Grok(t, false, func(l *platform.GrokLayer) {
			l.Rules = []platform.GrokRule{{Action: "allow", Tool: "bash", Pattern: "git status"}}
		}), "grok-build")
		if k := bv237Kind(t, a, rules.KindRunsCommands); k.State != rules.StateBounded || k.Scope != rules.KindScopeListedCommands {
			t.Errorf("runs kind %+v, want bounded listed_commands", k)
		}
		if p := bv237Row(t, a, rules.RowRunsCommands).Data.PreapprovedRules; p == nil || *p != 1 {
			t.Errorf("preapproved rules %v, want 1", p)
		}
		if score != baseScore {
			t.Errorf("allow git status: score %d, want R2's %d", score, baseScore)
		}
	})
}
