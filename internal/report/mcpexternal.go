package report

import (
	"strings"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/rules"
)

// Rendering of mcp_external_action findings (BV-117/BV-121), shared by the HTML
// and the two Markdown reports. The strings are AP-10 Anexo 1's: one title per
// category (money has an «asking» variant), the «another AI model» sentence
// when another model approves, the provider's own-confirmation note when the
// catalog has one, and the «what to do» action. The title already says what it
// is, so there is no separate «what».

const mcpBase = "finding." + rules.RuleMCPExternalAction

// mcpTitleKey is the title code for a finding: per category, with money's
// «asking» variant when a person approves.
func mcpTitleKey(f rules.Finding) string {
	k := mcpBase + ".title." + f.Params.Category
	if f.Params.Category == rules.CategoryMoney && f.Params.Disposition == rules.MCPApprovalPerson {
		return k + ".asking"
	}
	return k
}

// mcpBody are the finding's caveat sentences: the «another AI model» line when
// another model approves, then the provider's own-confirmation note when the
// catalog has one. Empty when neither applies.
func mcpBody(cat catalog.Catalog, f rules.Finding, p map[string]string) string {
	var parts []string
	if f.Params.Disposition == rules.MCPApprovalAnotherModel {
		parts = append(parts, cat.Text(mcpBase+".another_model", p))
	}
	if f.Params.VendorNote {
		vp := copyParams(p)
		vp["vendor_note"] = cat.Text(mcpBase+".vendor."+f.Params.MCPID, nil)
		parts = append(parts, cat.Text(mcpBase+".vendor_note", vp))
	}
	return strings.Join(parts, " ")
}

// mcpAction is the finding's «what to do» sentence.
func mcpAction(cat catalog.Catalog, p map[string]string) string {
	return cat.Text(mcpBase+".action", p)
}

// mcpImpact is the HTML impact (one paragraph): the caveats and the action.
func mcpImpact(cat catalog.Catalog, f rules.Finding, p map[string]string) string {
	if body := mcpBody(cat, f, p); body != "" {
		return body + " " + mcpAction(cat, p)
	}
	return mcpAction(cat, p)
}
