package report

import (
	"html"
	"strconv"
	"strings"

	"github.com/qtorb/rosmarops/internal/rules"
)

// The «Start here» card of the first block: one recommended action, the first
// of three cases that holds. A template that fixes urgent findings, when the
// person does not use one yet; else, rotating a key saved in git whose service
// is known; else, the first problem of the table. With no problems there is no
// card. Its numbers are the ones blocks 2 and 3 show, counted by the same
// functions.

// renderR3Start is the card, or nothing when there are no problems.
func (c htmlCtx) renderR3Start(iss []issue) {
	if len(iss) == 0 {
		return
	}
	var text, action string
	urgent, _, _ := c.r3RowCounts(iss)
	k, _ := c.r3TemplateFixes(iss)
	gitAt, service := 0, ""
	for i, is := range iss {
		if s := c.issueGitService(is); s != "" {
			gitAt, service = i+1, s
			break
		}
	}
	switch {
	// The same condition under which block 3 says how many urgent findings
	// the template fixes: some, and the person does not already use one.
	case k > 0 && c.tplInUseLine(c.templateAgent()) == "":
		text = c.r3StartText("html.r3.start.tpl", map[string]string{"k": strconv.Itoa(k), "n": strconv.Itoa(urgent)}, "")
		action = `<a class="r3-btn" href="#v-pc" data-view="v-pc">` + c.t("html.r3.tpl.button", nil) + `</a><span>` + c.t("html.r3.tpl.note", nil) + `</span>`
	case gitAt > 0:
		text = c.r3StartText("html.r3.start.git", map[string]string{"service": serviceDisplay(service)}, "")
		action = `<a class="r3-btn" href="#p-` + strconv.Itoa(gitAt) + `">` + c.t("html.r3.start.steps", nil) + `</a>`
	default:
		text = c.r3StartText("html.r3.start.first", map[string]string{"title": r3Mark}, c.issueTitle(iss[0]))
		action = `<a class="r3-btn" href="#p-1">` + c.t("html.r3.start.steps", nil) + `</a>`
	}
	c.w(`<div class="r3-start"><div class="r3-start-t"><p class="r3-start-e">%s</p><p>%s</p></div><div class="r3-start-a">%s</div></div>`,
		c.t("html.r3.start.eyebrow", nil), text, action)
}

// r3Mark stands for a piece of HTML that is already escaped (a problem's
// title) until the sentence around it has been escaped.
const r3Mark = "\x00"

// r3StartText is the card's sentence, escaped, with its first sentence in
// bold; markHTML, already escaped, takes the place of r3Mark.
func (c htmlCtx) r3StartText(key string, p map[string]string, markHTML string) string {
	e := html.EscapeString(c.cat.Text(key, p))
	if i := strings.Index(e, ". "); i >= 0 {
		e = "<b>" + e[:i+1] + "</b>" + e[i+1:]
	}
	return strings.Replace(e, r3Mark, markHTML, 1)
}

// issueGitService is the service of a problem's key saved in git, the one its
// «Change your key» step names; "" when the problem has no key in git or its
// service is not known (empty or "unknown"), as the table's fix column reads it.
func (c htmlCtx) issueGitService(is issue) string {
	if len(is.inGit()) == 0 {
		return ""
	}
	service := is.service
	if is.kind == issueOne {
		if f := is.findings[0]; f.Rule == rules.RuleCredInGit {
			for _, cr := range credsOf(f) {
				if s := c.v.credServices[cr]; s.service != "" && s.service != "unknown" {
					service = s.service
				}
			}
		}
	}
	if service == "" || service == "unknown" {
		return ""
	}
	return service
}

// r3StartKeys are the catalog codes of the verdict and the card.
var r3StartKeys = []string{
	"html.r3.verdict.exposed", "html.r3.verdict.open_gaps", "html.r3.verdict.contained",
	"html.r3.start.eyebrow", "html.r3.start.tpl", "html.r3.start.git", "html.r3.start.steps", "html.r3.start.first",
}
