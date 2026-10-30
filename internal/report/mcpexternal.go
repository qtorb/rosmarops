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

// mcpWhyKey is the «what it means» code for a finding (BV-124): one per
// category, with money's asking variant when a person approves.
func mcpWhyKey(f rules.Finding) string {
	k := mcpBase + ".why." + f.Params.Category
	if f.Params.Category == rules.CategoryMoney && f.Params.Disposition == rules.MCPApprovalPerson {
		return k + ".asking"
	}
	return k
}

// mcpWhatItMeans is the finding's explanatory text, shown below the title like
// every other rule's «why» (BV-124): the why line for its category, then the
// «another AI model» sentence when another model approves, then the provider's
// own-confirmation note when the catalog has one.
func mcpWhatItMeans(cat catalog.Catalog, f rules.Finding, p map[string]string) string {
	parts := []string{cat.Text(mcpWhyKey(f), p)}
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
