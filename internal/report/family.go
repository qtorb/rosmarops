package report

import "github.com/qtorb/rosmarops/internal/rules"

// Each problem of the report's table belongs to one family: what it is
// about, said with a name above its title and an icon in a box to its left,
// coloured by the problem's level. A rule that fits no family has neither.
// The families only change how the report presents problems: not the
// findings, their number or the score.

// The families.
const (
	familyKey      = "key"
	familyBetween  = "between_agents"
	familyAgent    = "agent"
	familyMCP      = "mcp"
	familyOrch     = "orchestrator"
	familySubagent = "subagent"
	familyProject  = "project"
	familyDesktop  = "desktop"
)

// ruleFamily is the family of each rule. package_known_malicious has none:
// it comes from the threat-intel cross on any node, not from one kind of
// thing.
var ruleFamily = map[string]string{
	rules.RuleCredReadAndSend:                  familyKey,
	rules.RuleCredReadable:                     familyKey,
	rules.RuleCredInGit:                        familyKey,
	rules.RuleN8NKeyWithData:                   familyKey,
	rules.RuleForeignAgentConfigWritable:       familyBetween,
	rules.RuleForeignAgentInstructionsWritable: familyBetween,
	rules.RuleOwnInstructionsWritable:          familyAgent,
	rules.RuleOwnConfigWritable:                familyAgent,
	rules.RuleActsWithoutYou:                   familyAgent,
	rules.RuleMCPExternalAction:                familyMCP,
	rules.RuleMCPUnpinnedLaunch:                familyMCP,
	rules.RuleMCPProjectAutoApprove:            familyMCP,
	rules.RuleN8NMCPBridge:                     familyMCP,
	rules.RuleOrchestratorSkipsApprovals:       familyOrch,
	rules.RuleOrchestratorVulnerableVersion:    familyOrch,
	rules.RuleOrchestratorNetworkListen:        familyOrch,
	rules.RuleOrchestratorOpenSignup:           familyOrch,
	rules.RuleSubagentAllTools:                 familySubagent,
	rules.RuleSubagentPermissiveMode:           familySubagent,
	rules.RuleSubagentHookCommand:              familySubagent,
	rules.RuleSubagentInlineMCP:                familySubagent,
	rules.RuleProjectHookRunsRepoScript:        familyProject,
	rules.RuleComputerUseAppAllowed:            familyDesktop,
}

// familyIcons are the families' drawings, on a 24 grid, drawn with a 2 px
// stroke in the current colour.
var familyIcons = map[string]string{
	familyKey:      `<circle cx="7.5" cy="15.5" r="5.5"/><path d="M11.5 11.5 21 2M17 6l3 3M14 9l2 2"/>`,
	familyBetween:  `<circle cx="6" cy="12" r="3.5"/><circle cx="18" cy="12" r="3.5"/><path d="M9.5 12h5"/><path d="M12.5 9.5l2.5 2.5-2.5 2.5"/>`,
	familyAgent:    `<circle cx="12" cy="12" r="8"/><circle cx="12" cy="12" r="2.2"/>`,
	familyMCP:      `<path d="M9 3v5M15 3v5"/><path d="M6 8h12v3a6 6 0 0 1-12 0z"/><path d="M12 17v4"/>`,
	familyOrch:     `<circle cx="12" cy="5" r="2.5"/><circle cx="5" cy="19" r="2.5"/><circle cx="12" cy="19" r="2.5"/><circle cx="19" cy="19" r="2.5"/><path d="M12 7.5v9M12 12H5v4.5M12 12h7v4.5"/>`,
	familySubagent: `<circle cx="9" cy="8" r="5"/><circle cx="17.5" cy="17.5" r="3"/><path d="M12.6 11.6l2.7 3.7"/>`,
	familyProject:  `<path d="M3 7.5A1.5 1.5 0 0 1 4.5 6h4l2 2.2h9A1.5 1.5 0 0 1 21 9.7v7.8A1.5 1.5 0 0 1 19.5 19h-15A1.5 1.5 0 0 1 3 17.5z"/>`,
	familyDesktop:  `<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M9 20h6M12 16v4"/>`,
}

// issueFamily is a problem's family: its kind's for the grouped problems,
// its finding's rule's for a problem of its own.
func issueFamily(is issue) string {
	switch is.kind {
	case issueCred:
		return familyKey
	case issueConfig, issueConfigMany:
		return familyBetween
	}
	if len(is.findings) == 0 {
		return ""
	}
	return ruleFamily[is.findings[0].Rule]
}

// familyIcon is a family's icon box: the drawing in a square coloured by
// the level (red for urgent, amber to review). "" for no family. It goes
// only on a problem's row and its section's header: an icon is not
// repeated on every line.
func familyIcon(family string, red bool) string {
	d, ok := familyIcons[family]
	if !ok {
		return ""
	}
	cls := "r3-ico amber"
	if red {
		cls = "r3-ico red"
	}
	return `<span class="` + cls + `" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + d + `</svg></span>`
}

// familyName is the family's name line above a title, escaped; "" for no
// family.
func (c htmlCtx) familyName(family string) string {
	if family == "" {
		return ""
	}
	return `<span class="r3-fl">` + c.t("html.family."+family, nil) + `</span>`
}

// familyKeys are the catalog codes of the families' names.
func familyKeys() []string {
	var out []string
	for _, f := range []string{familyKey, familyBetween, familyAgent, familyMCP, familyOrch, familySubagent, familyProject, familyDesktop} {
		out = append(out, "html.family."+f)
	}
	return out
}

// familyCSS is the style of the icon box and the family's name (0.20 table,
// §8).
const familyCSS = `
.r3-pc{display:flex;align-items:flex-start;gap:14px}
.r3-pt{min-width:0;flex:1}
.r3-ico{flex:none;width:36px;height:36px;border-radius:9px;display:inline-flex;align-items:center;justify-content:center}
.r3-ico svg{width:20px;height:20px}
.r3-ico.red{background:var(--ds-red-tint);color:var(--ds-red)}
.r3-ico.amber{background:var(--ds-amber-tint);color:var(--ds-amber)}
.r3-pa{margin-top:16px;padding-top:12px;border-top:1px solid var(--ds-line)}
.r3-pa h3{margin:0 0 6px;font-size:17px;line-height:1.3;font-weight:700}
.r3-fl{display:block;margin-bottom:2px;font-size:12px;line-height:1.3;font-weight:700;text-transform:uppercase;letter-spacing:.05em;color:var(--ds-muted)}
@media (max-width:700px){
.r3-pc{gap:12px}
.r3-tbl tr.r3-hi .c-f{margin-left:48px}
}`
