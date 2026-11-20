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
	p := map[string]string{"orchestrator": f.Params.Agent, "agent": f.Params.TargetAgent, "version": f.Params.Version}
	if kb, err := knowledge.LoadPaperclip(); err == nil {
		p["fixed"] = kb.FixedVersion.Version
	}
	return p
}

// OrchestratorTitle is an orchestrator finding's sentence.
func OrchestratorTitle(cat catalog.Catalog, f rules.Finding) string {
	return cat.Text(orchestratorKey(f), orchestratorParams(f))
}

// whenParams are the parameters of a finding's «when it does not apply» line:
// the orchestrator rules' own, nil for every other rule (unchanged).
func whenParams(f rules.Finding) map[string]string {
	if IsOrchestratorRule(f.Rule) {
		return orchestratorParams(f)
	}
	return nil
}
