package report

import (
	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/rules"
)

// The orchestrator rules (BV-264) render one signed sentence each, their
// html.headline key, everywhere a finding has a title: the report, the
// Markdown and AI exports, the space view and the findings window. They have
// no separate «what» or «why» (as computer_use_app_allowed, BV-193), and
// their «when it does not apply» line takes the same parameters.

// IsOrchestratorRule reports the four orchestrator rules.
func IsOrchestratorRule(r string) bool {
	switch r {
	case rules.RuleOrchestratorSkipsApprovals, rules.RuleOrchestratorVulnerableVersion,
		rules.RuleOrchestratorNetworkListen, rules.RuleOrchestratorOpenSignup:
		return true
	}
	return false
}

// IsSubagentRule reports the four Claude Code subagent rules (BV-268). They
// render as the orchestrator rules do: one signed sentence, html.headline.<rule>,
// with a second line when the subagent is in the user's folder.
func IsSubagentRule(r string) bool {
	switch r {
	case rules.RuleSubagentAllTools, rules.RuleSubagentPermissiveMode, rules.RuleSubagentHookCommand, rules.RuleSubagentInlineMCP:
		return true
	}
	return false
}

// IsSentenceRule reports the rules whose one signed sentence is their title
// everywhere: the orchestrator and the subagent rules.
func IsSentenceRule(r string) bool { return IsOrchestratorRule(r) || IsSubagentRule(r) }

// orchestratorKey is the finding's sentence: html.headline.<rule>, and for a
// version found in npm's cache, its .npm_cache variant.
func orchestratorKey(f rules.Finding) string {
	key := "html.headline." + f.Rule
	if f.Rule == rules.RuleOrchestratorVulnerableVersion && f.Params.Scope == rules.VersionNPMCache {
		key += ".npm_cache"
	}
	return key
}

// orchestratorParams fills the sentences' {orchestrator}, {agent}, {version}
// and {fixed}: the orchestrator is the finding's agent, the launched agent its
// target, and the fixed version comes from Paperclip's knowledge base.
func orchestratorParams(f rules.Finding) map[string]string {
	if IsSubagentRule(f.Rule) {
		// BV-268 (option a): the subagent's name is target_agent, its mode scope.
		return map[string]string{"agent": f.Params.Agent, "subagent": f.Params.TargetAgent, "mode": f.Params.Scope}
	}
	p := map[string]string{"orchestrator": f.Params.Agent, "agent": f.Params.TargetAgent, "version": f.Params.Version}
	if kb, err := knowledge.LoadPaperclip(); err == nil {
		p["fixed"] = kb.FixedVersion.Version
	}
	return p
}

// OrchestratorTitle is an orchestrator or subagent finding's sentence; a
// subagent in the user's folder adds the html.subagent.user_folder line.
func OrchestratorTitle(cat catalog.Catalog, f rules.Finding) string {
	t := cat.Text(orchestratorKey(f), orchestratorParams(f))
	if IsSubagentRule(f.Rule) && f.Params.ResourceType == rules.SubagentUserFolder {
		t += " " + cat.Text(subagentUserFolderKey, nil)
	}
	return t
}

// subagentUserFolderKey is the line added to a subagent finding when the file
// is in the user's folder.
const subagentUserFolderKey = "html.subagent.user_folder"

// orchestratorFixKeys are the orchestrator rules that have a step of their
// own (html.fix.<rule>), filled like their sentence. The others keep the
// generic html.fix.none line.
var orchestratorFixKeys = []string{"html.fix." + rules.RuleOrchestratorVulnerableVersion, "html.fix." + rules.RuleOrchestratorNetworkListen, paseoListenFixKey}

// paseoListenFixKey is Paseo's step for orchestrator_network_listen: its key
// is daemon.listen, not Paperclip's bind (BV-268).
const paseoListenFixKey = "html.fix." + rules.RuleOrchestratorNetworkListen + ".paseo"

// orchestratorFixKey is the finding's own step, "" when it has none.
func orchestratorFixKey(f rules.Finding) string {
	if f.Rule == rules.RuleOrchestratorNetworkListen && f.Params.Scope == rules.PaseoListen {
		return paseoListenFixKey
	}
	for _, k := range orchestratorFixKeys {
		if k == "html.fix."+f.Rule {
			return k
		}
	}
	return ""
}

// whenParams are the parameters of a finding's «when it does not apply» line:
// the orchestrator rules' own, nil for every other rule (unchanged).
func whenParams(f rules.Finding) map[string]string {
	if IsSentenceRule(f.Rule) {
		return orchestratorParams(f)
	}
	return nil
}
