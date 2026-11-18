package report

import "html"

// CompanyHTMLKeys are the catalog codes of «Your company» and of the
// integrity line of an update the administrator's list accepted.
var CompanyHTMLKeys = []string{
	"report.company.h2", "report.company.config_trusted", "report.company.config_changed",
	"report.company.config_untrusted", "report.company.config_invalid",
	"report.company.summary_on", "report.company.summary_off", "report.company.report_dir_overridden",
	"report.integrity.binary_admin_list",
}

// renderCompany is «Your company», a section of the body before the footer
// notes, only when the computer has a company configuration: whether it is
// trusted and what it can do, whether it changed, and whether the company
// reads a summary of this review, shown exactly as the company reads it.
func (c htmlCtx) renderCompany() {
	co := c.v.Company
	if co == nil {
		return
	}
	path := map[string]string{"path": c.v.CompanyPath}
	c.w(`<section class="company"><h2>%s</h2>`, c.t("report.company.h2", nil))
	switch co.State {
	case "trusted":
		sha16 := co.SHA256
		if len(sha16) > 16 {
			sha16 = sha16[:16]
		}
		c.w(`<p>%s</p>`, html.EscapeString(c.cat.Text("report.company.config_trusted", map[string]string{"path": c.v.CompanyPath, "sha16": sha16})))
		if co.Changed {
			c.w(`<p class="warn">%s</p>`, c.t("report.company.config_changed", nil))
		}
		if co.ReportDirOverridden {
			c.w(`<p class="warn">%s</p>`, c.t("report.company.report_dir_overridden", nil))
		}
	case "untrusted":
		c.w(`<p class="warn">%s</p>`, html.EscapeString(c.cat.Text("report.company.config_untrusted", path)))
	case "invalid":
		c.w(`<p class="warn">%s</p>`, html.EscapeString(c.cat.Text("report.company.config_invalid", path)))
	}
	if co.SummaryEnabled && c.v.CompanyRead != nil {
		c.w(`<p>%s</p><pre><code>%s</code></pre>`, c.t("report.company.summary_on", nil), html.EscapeString(PrettySummary(*c.v.CompanyRead)))
	} else {
		c.w(`<p>%s</p>`, c.t("report.company.summary_off", nil))
	}
	c.w(`</section>`)
}
