package report

import (
	"slices"

	"github.com/qtorb/rosmarops/internal/rules"
)

// «What it means»: one signed sentence for each finding whose title is its
// only sentence (computer_use_app_allowed and the orchestrator, subagent and
// MCP rules), shown in the HTML report where the other findings show their
// impact paragraph. Only the HTML report shows it: the export for your AI, the
// findings window, the space view and the Markdown keep the title alone
// until the sentence has been seen in the report.

// computerUseAllApps is the app id of default_app_access = allow: every app.
const computerUseAllApps = "*"

// meaningKey is the finding's sentence, "" for a rule that has an impact
// paragraph of its own. Where the rule's finding covers cases that the main
// sentence would overstate, a variant says only what the rule sees: every app
// rather than named ones, an orchestrator whose effect on approvals is
// unknown (yellow), a copy in npm's cache rather than the installed version,
// a subagent mode that still asks for some things (yellow), and a project's
// own setting rather than the user's or the managed one.
func meaningKey(f rules.Finding) string {
	if f.Rule != rules.RuleComputerUseAppAllowed && !IsSentenceRule(f.Rule) {
		return ""
	}
	key := "html.meaning." + f.Rule
	switch f.Rule {
	case rules.RuleComputerUseAppAllowed:
		if slices.Contains(f.Evidence.Locations, computerUseAllApps) {
			key += ".all"
		}
	case rules.RuleOrchestratorSkipsApprovals:
		if f.Severity == rules.SeverityYellow {
			key += ".unknown"
		}
	case rules.RuleOrchestratorVulnerableVersion:
		if f.Params.Scope == rules.VersionNPMCache {
			key += ".npm_cache"
		}
	case rules.RuleSubagentPermissiveMode:
		if f.Severity == rules.SeverityYellow {
			key += ".yellow"
		}
	case rules.RuleMCPProjectAutoApprove:
		// A project or local settings file applies in its own project only.
		if f.Radius == rules.RadiusSameProject {
			key += ".project"
		}
	}
	return key
}

// meaningText is the finding's «What it means» sentence, "" when it has none.
func (c htmlCtx) meaningText(f rules.Finding) string {
	key := meaningKey(f)
	if key == "" {
		return ""
	}
	p := orchestratorParams(f)
	if f.Rule == rules.RuleComputerUseAppAllowed {
		p = computerUseParams(f)
	}
	return c.cat.Text(key, p)
}

// MeaningKeys are the catalog codes of the «What it means» sentences.
var MeaningKeys = []string{
	"html.meaning.computer_use_app_allowed", "html.meaning.computer_use_app_allowed.all",
	"html.meaning.orchestrator_skips_approvals", "html.meaning.orchestrator_skips_approvals.unknown",
	"html.meaning.orchestrator_vulnerable_version", "html.meaning.orchestrator_vulnerable_version.npm_cache",
	"html.meaning.orchestrator_network_listen", "html.meaning.orchestrator_open_signup",
	"html.meaning.subagent_all_tools", "html.meaning.subagent_permissive_mode", "html.meaning.subagent_permissive_mode.yellow",
	"html.meaning.subagent_hook_command", "html.meaning.subagent_inline_mcp",
	"html.meaning.mcp_unpinned_launch", "html.meaning.mcp_project_autoapprove", "html.meaning.mcp_project_autoapprove.project",
}
