package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-198 M3 (CTO-23 §0, firma 2): a web or MCP tool the agent can use without
// asking is internet egress, for every agent. These tests check the single
// rule where it bites (Cline useBrowser/useMcp, Gemini google_web_search, Grok
// web_search) and where it does not (Copilot and Antigravity CLI ask before the
// web and MCP by default, so their default posture is unchanged).

// m3Rules returns the rules of a client's findings.
func m3Rules(res rules.Result, client string) map[string]bool {
	out := map[string]bool{}
	for _, f := range res.Findings {
		if len(f.Evidence.Nodes) > 0 && f.Evidence.Nodes[0] == client {
			out[f.Rule] = true
		}
	}
	return out
}

// M3 Cline: a fresh install approves the browser and MCP without asking, so a
// reachable credential is read-and-send and the sends row decides nobody.
func TestM3_Cline_DefaultBrowserMCPEgress(t *testing.T) {
	_, res, client := clineEvaluate(t, clBase(), nil, nil, []entry{{home + "/.aws/credentials", 1, "SUPABASE_SERVICE_ROLE_KEY", serviceRole}})
	if !m3Rules(res, client)[rules.RuleCredReadAndSend] {
		t.Errorf("Cline: useBrowser/useMcp should make the credential read-and-send (have %v)", m3Rules(res, client))
	}
	if c := clCell(t, res, client, rules.RowSendsOut); c.Decider != rules.DeciderNobody {
		t.Errorf("Cline sends: browser/MCP egress, want nobody, got %+v", c)
	}
}

// M3 Gemini: google_web_search is allowed without asking, so a workspace
// credential is read-and-send, not merely readable.
func TestM3_Gemini_WebSearchEgress(t *testing.T) {
	scan := geScan(geUser(""))
	_, res, client := geminiEvaluate(t, scan, nil, []entry{{repoWeb + "/.env", 1, "SUPABASE_SERVICE_ROLE_KEY", serviceRole}})
	rs := m3Rules(res, client)
	if !rs[rules.RuleCredReadAndSend] || rs[rules.RuleCredReadable] {
		t.Errorf("Gemini: web_search egress should give cred_read_and_send, not cred_readable (have %v)", rs)
	}
}

// M3 Grok: web_search is in the read-only allow list, so in the default mode a
// reachable credential is read-and-send.
func TestM3_Grok_WebSearchEgress(t *testing.T) {
	scan := platform.GrokScan{ConfigDir: grokDir, Exists: true, Layers: []platform.GrokLayer{grokUserLayer(nil)}}
	_, res, client := grokEvaluate(t, scan, nil, []entry{{home + "/.aws/credentials", 1, "SUPABASE_SERVICE_ROLE_KEY", serviceRole}})
	if !m3Rules(res, client)[rules.RuleCredReadAndSend] {
		t.Errorf("Grok: web_search egress should make the credential read-and-send (have %v)", m3Rules(res, client))
	}
}

// M3 Copilot (si aplica): the URL fetch and MCP ask by default, so there is no
// default egress — a workspace credential is readable, not read-and-send.
func TestM3_Copilot_NoDefaultEgress(t *testing.T) {
	scan := platform.CopilotScan{ConfigDir: copilotDir, Present: true, Layers: []platform.CopilotLayer{copilotUserLayer(nil)}}
	_, res, client := copilotEvaluate(t, scan, true, nil, nil, []entry{{repoWeb + "/.env", 1, "SUPABASE_SERVICE_ROLE_KEY", serviceRole}})
	rs := m3Rules(res, client)
	if rs[rules.RuleCredReadAndSend] {
		t.Errorf("Copilot: web and MCP ask by default, so no cred_read_and_send (have %v)", rs)
	}
	if !rs[rules.RuleCredReadable] {
		t.Errorf("Copilot: the workspace credential should still be readable (have %v)", rs)
	}
}

// M3 Antigravity CLI (si aplica): read_url, execute_url and MCP ask by default,
// so there is no default egress — a workspace credential is readable only.
func TestM3_Antigravity_NoDefaultEgress(t *testing.T) {
	scan := platform.AntigravityScan{ConfigDir: agGemini, Present: true, Settings: platform.AntigravitySettings{Path: agSettings, Status: platform.MCPSourceRead}}
	_, res, client := antigravityEvaluate(t, scan, nil, nil, nil, []entry{{repoWeb + "/.env", 1, "SUPABASE_SERVICE_ROLE_KEY", serviceRole}})
	rs := agRules(res, client)
	if rs[rules.RuleCredReadAndSend] {
		t.Errorf("Antigravity: web and MCP ask by default, so no cred_read_and_send (have %v)", rs)
	}
	if !rs[rules.RuleCredReadable] {
		t.Errorf("Antigravity: the workspace credential should still be readable (have %v)", rs)
	}
}
