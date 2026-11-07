package rules_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/rules"
)

// BV-194 (CTO-22 Gemini §3, §5, §6): the Gemini CLI remainder — the .env
// loading rule, extensions, the admin/user policy tiers, and settings.json
// hooks.

// G6. A user policy that allows run_shell_command with no prefix runs any
// command without asking (CTO-22 Gemini §6): runs_commands is nobody.
func TestGemini194_PolicyAllowsShell(t *testing.T) {
	scan := geScan(geUser(""))
	scan.Policies = []platform.GeminiPolicyLayer{{
		Kind: platform.GeminiPolicyUser, Status: platform.MCPSourceRead,
		Rules: []platform.GeminiPolicyRule{{ToolNames: []string{"run_shell_command"}, Decision: "allow", Path: geGemini + "/policies/p.toml"}},
	}}
	_, res, client := geminiEvaluate(t, scan, nil, nil)
	if c := geCell(t, res, client, rules.RowRunsCommands); c.Decider != rules.DeciderNobody {
		t.Errorf("policy allow run_shell_command: runs %+v, want nobody", c)
	}
}

// G7. An extension's mcpServers load like the user layer's, with the
// extension's own config_path (CTO-22 Gemini §5): the graph has the mcp_server
// node attributed to Gemini CLI.
func TestGemini194_ExtensionMCP(t *testing.T) {
	scan := geScan(geUser(""))
	scan.Extensions = []platform.GeminiExtension{{
		Name: "demo", Dir: geGemini + "/extensions/demo", ConfigPath: geGemini + "/extensions/demo/gemini-extension.json",
		Status: platform.MCPSourceRead,
		MCP:    []platform.GeminiMCPServer{{ID: "extsrv", Command: "node", Args: []string{"server.js"}}},
	}}
	g, _, client := geminiEvaluate(t, scan, nil, nil)
	var found bool
	for _, n := range g.Nodes {
		if n.Kind == graph.KindAgent && n.Type == graph.AgentMCPServer && n.Name == "extsrv" && n.Platform == knowledge.GeminiCLIProduct {
			found = true
			if n.ConfigPath != scan.Extensions[0].ConfigPath {
				t.Errorf("extension server config_path %q, want the extension json", n.ConfigPath)
			}
		}
	}
	if !found {
		t.Errorf("no mcp_server node for the extension server")
	}
	_ = client
}

// G8. A project hook whose command runs a repo script, in a trusted folder, is
// a project_hook_runs_repo_script (CTO-22 Gemini §5).
func TestGemini194_ProjectHookRepoScript(t *testing.T) {
	l := platform.GeminiLayer{Kind: platform.GeminiLayerProject, Path: repoWeb + "/.gemini/settings.json", Root: repoWeb, Status: platform.MCPSourceRead}
	l.Hooks = []platform.GeminiHook{{Event: "BeforeTool", Command: "node .gemini/hook.js", Path: l.Path, Root: repoWeb, ScriptPath: ".gemini/hook.js", ScriptSize: 42}}
	scan := geScan(geUser(""), l)
	scan.TrustedFolders = map[string]string{repoWeb: "TRUST_FOLDER"}
	_, res, _ := geminiEvaluate(t, scan, nil, nil)
	// The finding's first evidence node is the hook resource, not the client,
	// so scan the findings by rule rather than by geRules (client-keyed).
	var found bool
	for _, f := range res.Findings {
		if f.Rule == rules.RuleProjectHookRunsRepoScript {
			found = true
		}
	}
	if !found {
		t.Errorf("trusted project hook: missing project_hook_runs_repo_script")
	}

	// With hooks disabled, it is not a finding.
	l2 := l
	l2.HooksDisabled = true
	off := geScan(geUser(""), l2)
	off.TrustedFolders = map[string]string{repoWeb: "TRUST_FOLDER"}
	_, resOff, _ := geminiEvaluate(t, off, nil, nil)
	for _, f := range resOff.Findings {
		if f.Rule == rules.RuleProjectHookRunsRepoScript {
			t.Errorf("hooks disabled: project_hook_runs_repo_script should not fire")
		}
	}
}

// G9. The .env loading rule (CTO-22 Gemini §3): a credential at a denied .env
// location is not reachable; one at an allowed location is. The denied set
// stands for a non-winning .env, or a non-Google variable in an untrusted
// root's winning .env.
func TestGemini194_EnvDenied(t *testing.T) {
	deniedLoc := repoWeb + "/.env:1"
	t.Run("denied not reachable", func(t *testing.T) {
		scan := geScan(geUser(""))
		scan.EnvDenied = []string{deniedLoc}
		_, res, client := geminiEvaluate(t, scan, nil, []entry{{repoWeb + "/.env", 1, "OPENAI_API_KEY", openAIKey}})
		if r := geRules(res, client); r[rules.RuleCredReadable] || r[rules.RuleCredReadAndSend] {
			t.Errorf("denied .env credential should not be reachable (have %v)", r)
		}
	})
	t.Run("allowed reachable", func(t *testing.T) {
		scan := geScan(geUser(""))
		scan.EnvDenied = []string{deniedLoc}
		_, res, client := geminiEvaluate(t, scan, nil, []entry{{repoWeb + "/.env", 2, "GOOGLE_API_KEY", sbSecret}})
		if r := geRules(res, client); !r[rules.RuleCredReadable] && !r[rules.RuleCredReadAndSend] {
			t.Errorf("allowed .env credential should be reachable (have %v)", r)
		}
	})
}
