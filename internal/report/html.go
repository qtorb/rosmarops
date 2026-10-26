package report

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/rules"
)

// HTMLFileName is the visual report with a fixed name, replaced on every
// run, next to the per-run copy with the Markdown's base name (informe HTML
// v1, §1).
const HTMLFileName = "report.html"

// cssToken is one colour token of informe HTML v2 §1, in light and dark.
type cssToken struct{ Name, Light, Dark string }

// htmlTokens are the colour tokens of informe HTML v2 §1.
var htmlTokens = []cssToken{
	{"ink", "#1B1A18", "#EDEBE6"},
	{"mut", "#4D4A45", "#C4C1BA"},
	{"red", "#94200B", "#FF9A85"},
	{"amb", "#6B4700", "#F5C451"},
	{"grn", "#1A5532", "#6FD6A3"},
	{"redfill", "#C2381C", "#FF6A4D"},
	{"ambfill", "#B07A00", "#F0B429"},
	{"grnfill", "#2E8B57", "#4CC38A"},
	{"bg", "#F6F5F2", "#131311"},
	{"card", "#FFFFFF", "#1D1C1A"},
	{"line", "#D9D5CC", "#3A3833"},
	{"red-bg", "#FDECE7", "#2E1913"},
	{"amb-bg", "#FDF5DF", "#2A2310"},
	{"grn-bg", "#E8F5EE", "#12281D"},
	{"code", "#F0EEE8", "#2A2926"},
	{"focus", "#1F4FD1", "#8FB1FF"},
}

// textTokens and fillTokens are the text and fill colours of §1: text at
// least 7:1 on --card and --bg, fills at least 3:1.
var (
	textTokens = []string{"ink", "mut", "red", "amb", "grn"}
	fillTokens = []string{"redfill", "ambfill", "grnfill"}
)

// todayPill is the background of the today tag by severity, with white
// text, in both modes (§1).
var todayPill = map[string]string{rules.SeverityRed: "#9E2410", rules.SeverityYellow: "#6B4700"}

// htmlCSS is the style of informe HTML v3 (v1 and v2 where v3 does not
// change them): the tokens and accessibility of v2 §1–§2, the help of v2
// §4, the three-step layout of v3 §1–§7.
func htmlCSS() string {
	var light, dark strings.Builder
	for _, t := range htmlTokens {
		fmt.Fprintf(&light, "--%s:%s;", t.Name, t.Light)
		fmt.Fprintf(&dark, "--%s:%s;", t.Name, t.Dark)
	}
	return ":root{" + strings.TrimSuffix(light.String(), ";") + "}\n@media (prefers-color-scheme:dark){:root{" + strings.TrimSuffix(dark.String(), ";") + "}}\n" + htmlRules + overviewCSS
}

const htmlRules = `*{box-sizing:border-box}
svg{flex-shrink:0}
body{margin:0;background:var(--bg);color:var(--ink);font-family:system-ui,-apple-system,"Segoe UI",sans-serif;font-size:16px;line-height:1.5;overflow-wrap:anywhere}
main{max-width:1320px;margin:0 auto;padding:16px 32px 16px}
code,pre{font-family:ui-monospace,Consolas,monospace}
code{font-size:.88em}
a{color:var(--ink);text-decoration:underline}
:focus-visible{outline:3px solid var(--focus);outline-offset:2px;border-radius:6px}
.hdr{font-size:14px;font-weight:400;color:var(--mut);margin:0 0 16px}
.flow{display:grid;grid-template-columns:minmax(0,.78fr) minmax(0,1.08fr) minmax(0,1.5fr);column-gap:36px;align-items:start}
.head{display:flex;align-items:center;gap:14px;margin-bottom:12px}
.num{width:48px;height:48px;flex:none;border-radius:50%;background:var(--ink);color:var(--bg);display:flex;align-items:center;justify-content:center;font-size:22px;font-weight:800;box-shadow:0 0 0 8px var(--bg)}
.now .num{box-shadow:0 0 0 5px var(--bg),0 0 0 8px var(--ink),0 0 0 14px var(--bg)}
.step{font-size:22px;font-weight:800;letter-spacing:-.01em;margin:0;line-height:1.25}
.hint{display:block;font-size:14px;font-weight:600;color:var(--mut);line-height:1.2}
.line{flex:1;min-width:24px;height:2px;background:var(--mut);opacity:.5;margin-right:-36px;position:relative}
.line::after{content:"";position:absolute;right:0;top:-5px;border-top:6px solid transparent;border-bottom:6px solid transparent;border-left:9px solid var(--mut)}
.iname{display:flex;align-items:center;gap:6px;flex-wrap:nowrap;font-size:13px;font-weight:700;text-transform:uppercase;letter-spacing:.05em;color:var(--mut)}
.score{font-size:84px;font-weight:800;line-height:.95;letter-spacing:-.03em;margin:2px 0 10px;white-space:nowrap;overflow-wrap:normal}
.score .of{font-size:26px;font-weight:600;color:var(--mut);letter-spacing:0}
.unavail{font-size:21px;font-weight:700;color:var(--mut);margin:2px 0 10px}
h1.hl{font-size:21px;font-weight:700;line-height:1.3;margin:0 0 12px}
.meta{font-size:15px;color:var(--mut);line-height:1.7;margin:0}
.sagent{font-size:15px;font-weight:600;color:var(--ink);margin:0 0 10px}
.scale{font-size:14px;color:var(--mut);margin:0 0 10px}
.tbox{background:var(--card);border:1px solid var(--line);border-radius:16px;padding:18px 22px}
.tbox h2{font-size:20px;font-weight:700;line-height:1.3;margin:0 0 8px}
.lbl{font-size:13px;font-weight:700;text-transform:uppercase;letter-spacing:.05em;color:var(--mut)}
.tf{font-size:15px;font-weight:600;margin:0 0 6px}
.tf .tchip{margin-left:8px}
.tbox>p.imp{font-size:15px;margin:0 0 10px}
.proj{display:inline-block;font-size:14px;font-weight:600;border:1px solid var(--line);border-radius:999px;padding:3px 12px;margin:0 0 12px}
button{font-family:inherit;font-size:14px;font-weight:600;background:var(--ink);color:var(--bg);padding:8px 14px;border-radius:8px;border:0;cursor:pointer;min-height:44px}
button.wide{display:flex;width:100%;justify-content:center;align-items:center;min-height:48px;border-radius:10px;font-size:16px}
.live{margin-left:10px;font-size:14px}
.how{font-size:14px;color:var(--mut);margin:10px 0 0}
.tbox details.manual{border-top:1px solid var(--line);margin-top:12px;padding-top:10px}
.manual{font-size:15px;margin-top:12px}
.manual>summary{min-height:44px;display:flex;align-items:center;gap:8px;cursor:pointer;color:var(--mut);list-style:none}
.tbox .manual>summary{font-size:14px}
.tbox .after{display:block;font-size:14px;color:var(--mut);margin:10px 0 0}
.tbox .live{display:block;margin:0}
.grp{display:flex;align-items:center;gap:8px;font-size:14px;font-weight:700}
.grp:not(:first-of-type){margin-top:10px}
.dot{width:10px;height:10px;flex:none;border-radius:50%}
details.row{border-bottom:1px solid var(--line)}
.grp+details.row{border-top:1px solid var(--line)}
details.row>summary{display:flex;gap:10px;align-items:center;min-height:44px;font-size:15px;list-style:none;cursor:pointer}
details>summary::-webkit-details-marker{display:none}
details.row>summary .t{flex:1;min-width:0;line-height:1.3;padding:6px 0}
details.row>summary .x{font-size:13px;color:var(--mut);min-width:86px;text-align:right;white-space:nowrap}
details.rest>summary{display:flex;gap:8px;align-items:center;min-height:44px;font-size:15px;font-weight:600;color:var(--ink);list-style:none;cursor:pointer}
.chev{color:var(--mut);transition:transform .15s}
details[open]>summary .chev{transform:rotate(90deg)}
.rb{padding:0 0 12px 20px;font-size:15px}
.tchip{font-size:12px;font-weight:600;color:var(--mut);border:1px solid var(--line);padding:1px 8px;border-radius:999px;white-space:nowrap}
.chip-any{display:inline-block;font-size:13px;font-weight:600;color:var(--red);background:var(--red-bg);padding:3px 9px;border-radius:999px}
.gain{font-size:15px;font-weight:600}
.tech{font-size:14px;color:var(--mut);margin-top:12px}
.tech>summary{min-height:44px;display:flex;align-items:center;cursor:pointer}
.fix{background:var(--bg);border-radius:12px;padding:14px 16px;margin-top:12px}
.rb .fix{background:var(--card)}
.fix-t{font-weight:700;margin-bottom:6px}
pre{background:var(--code);padding:10px 12px;font-size:13px;white-space:pre-wrap;word-break:break-all;border-radius:8px}
.after{display:flex;gap:8px;align-items:center;font-size:15px;color:var(--ink);margin-top:12px}
.none{font-size:17px;margin:0}
.empty{font-size:15px;color:var(--mut);margin:0}
.cov{background:var(--card);border:1px solid var(--line);border-radius:12px;padding:14px 16px;margin-top:20px;font-size:14px}
.cov h3{font-size:15px;margin:0 0 4px}
.cov p.note{color:var(--mut)}
.sig{list-style:none;padding:0;margin:8px 0 0}
.sig li{display:flex;align-items:center;gap:8px;font-size:15px}
.sig .n{color:var(--mut)}
.rot{font-size:15px;margin:0 0 12px;padding-left:20px}
.cov p,.cov ul{margin:0}
.more{display:grid;grid-template-columns:repeat(4,minmax(0,1fr));gap:28px;border-top:1px solid var(--line);margin-top:20px;padding-top:14px}
.more h3{font-size:14px;font-weight:700;margin:0 0 4px}
.more p{font-size:14px;color:var(--mut);line-height:1.45;margin:0 0 8px}
.more details>summary{font-size:14px;color:var(--mut);min-height:44px;display:flex;align-items:center;cursor:pointer}
button.b2{display:inline-flex;align-items:center;min-height:44px;padding:0 14px;border-radius:10px;border:1px solid var(--line);background:transparent;color:var(--ink);font-size:14px;font-weight:600;margin:0 6px 6px 0}
.foot{font-size:13px;color:var(--mut);margin:8px 0 0}
button.i{width:28px;height:28px;min-height:28px;padding:0;margin:0 2px;border:2px solid var(--mut);background:transparent;color:var(--mut);border-radius:50%;position:relative;display:inline-flex;align-items:center;justify-content:center;vertical-align:middle;font:700 14px Georgia,serif;text-transform:none;letter-spacing:0}
button.i::after{content:"";position:absolute;inset:-8px}
.vh{position:absolute;width:1px;height:1px;overflow:hidden;clip:rect(0 0 0 0);clip-path:inset(50%);white-space:nowrap}
.gl{display:block;background:var(--bg);border-left:4px solid var(--mut);border-radius:6px;padding:8px 12px;margin:8px 0;font-size:15px;font-weight:400;line-height:1.5;color:var(--ink);text-transform:none;letter-spacing:normal}
.gl[hidden]{display:none}
@media (max-width:1099px){.flow{grid-template-columns:minmax(0,1fr);row-gap:36px}.line{display:none}.more{grid-template-columns:repeat(2,minmax(0,1fr))}.tchip{white-space:normal}}
@media (prefers-reduced-motion:reduce){*{transition:none!important}details[open]>summary .chev{transform:none}}`

// htmlJS is the only script (informe HTML v1 §6, v2 §7): copy a prompt,
// with the aria-live notice of v2 §2; save the card the binary generated,
// from its templates, with a Blob; copy or download the export for your AI
// (copying falls back to downloading); open and close a help «i». No
// network, no storage, no timer but the copy notices'.
const htmlJS = `document.querySelectorAll('button[data-copied]').forEach(function(b){b.addEventListener('click',function(){var p=b.previousElementSibling;var l=b.nextElementSibling;function sel(){var r=document.createRange();r.selectNodeContents(p);var s=window.getSelection();s.removeAllRanges();s.addRange(r)}try{navigator.clipboard.writeText(p.innerText).then(function(){l.textContent=b.dataset.copied;setTimeout(function(){l.textContent=''},1500)},sel)}catch(e){sel()}})});
var cb=document.getElementById('card-button');
function save(c,type,name){var a=document.createElement('a');a.href=URL.createObjectURL(new Blob([c],{type:type}));a.download=name;document.body.appendChild(a);a.click();a.remove()}
if(cb){cb.addEventListener('click',function(){save(document.getElementById('card-svg').innerHTML,'image/svg+xml',cb.dataset.svg);save(document.getElementById('card-txt').content.textContent,'text/plain',cb.dataset.txt)})}
var ac=document.getElementById('ai-copy'),ad=document.getElementById('ai-download');
function aiText(){return document.getElementById('ai-export').content.textContent}
function aiSave(){save(aiText(),'text/markdown',ad.dataset.file)}
if(ad){ad.addEventListener('click',aiSave)}
if(ac){ac.addEventListener('click',function(){var l=ac.nextElementSibling;try{navigator.clipboard.writeText(aiText()).then(function(){l.textContent=ac.dataset.done;setTimeout(function(){l.textContent=''},1500)},aiSave)}catch(e){aiSave()}})}
document.querySelectorAll('button.i').forEach(function(b){b.addEventListener('click',function(e){e.preventDefault();e.stopPropagation();var g=document.getElementById(b.getAttribute('aria-controls'));var open=b.getAttribute('aria-expanded')==='true';b.setAttribute('aria-expanded',open?'false':'true');g.hidden=open})});`

// Icons (§2): 22×22 by default, viewBox 0 0 24 24, in the group's colour.
func icon(kind, colour string, size int) string {
	s := strconv.Itoa(size)
	head := `<svg width="` + s + `" height="` + s + `" viewBox="0 0 24 24" aria-hidden="true">`
	switch kind {
	case "red":
		return head + `<path d="M12 2.5 1.5 21h21L12 2.5z" fill="` + colour + `"/><path d="M12 9v5.5" stroke="#fff" stroke-width="2.2" stroke-linecap="round"/><circle cx="12" cy="17.6" r="1.3" fill="#fff"/></svg>`
	case "yellow":
		return head + `<circle cx="12" cy="12" r="10" fill="` + colour + `"/><path d="M12 7v6" stroke="#fff" stroke-width="2.2" stroke-linecap="round"/><circle cx="12" cy="16.5" r="1.3" fill="#fff"/></svg>`
	}
	return head + `<circle cx="12" cy="12" r="10" fill="` + colour + `"/><path d="m7.5 12.5 3 3 6-6.5" stroke="#fff" stroke-width="2.2" fill="none" stroke-linecap="round" stroke-linejoin="round"/></svg>`
}

// sevFill is the fill token of a severity: icons, the card's left bar and
// the today block's border (§1).
func sevFill(sev string) string {
	if sev == rules.SeverityRed {
		return "var(--redfill)"
	}
	return "var(--ambfill)"
}

// bandVar is the colour token of a band.
var bandVar = map[string]string{
	index.BandExposed: "var(--red)", index.BandOpenGaps: "var(--amb)", index.BandContained: "var(--grn)",
}

// htmlCtx carries what the HTML renderer shares.
type htmlCtx struct {
	v      V2
	cat    catalog.Catalog
	home   string
	sb     *strings.Builder
	score  map[string]*index.AgentIndex // agent id → its score
	gloss  *int                         // the last help id, g1, g2…
	exists func(name string) bool       // a file of the reports directory exists
	rows   map[string]*credRow          // finding id → its row (v3 §12.2)
}

// help is the «i» of term (§4): the button and, right after it, its hidden
// answer, with an id unique in the page.
func (c htmlCtx) help(term string) string {
	*c.gloss++
	id := "g" + strconv.Itoa(*c.gloss)
	q, a := c.t("html.gloss."+term+".q", nil), c.t("html.gloss."+term+".a", nil)
	return `<button type="button" class="i" aria-expanded="false" aria-controls="` + id + `"><span aria-hidden="true">i</span><span class="vh">` + q +
		`</span></button><span class="gl" id="` + id + `" hidden><b>` + q + `</b> ` + a + `</span>`
}

func (c htmlCtx) t(key string, p map[string]string) string {
	return html.EscapeString(c.cat.Text(key, p))
}

func (c htmlCtx) w(format string, a ...any) { fmt.Fprintf(c.sb, format, a...) }

// listNames joins names with ", " and html.list.and before the last (§3.2).
func listNames(cat catalog.Catalog, names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + cat.Text("html.list.and", nil) + names[len(names)-1]
}

// userPath replaces the user's folder at the start of file with repl
// ("%USERPROFILE%" for the agent prompt, "~" for the manual steps; §5).
func userPath(file, home, repl string) string {
	if home == "" {
		return file
	}
	h := strings.TrimRight(graph.CanonicalPath(home), "/")
	if strings.HasPrefix(graph.CanonicalPath(file), h+"/") {
		return repl + file[len(h):]
	}
	return file
}

// RenderHTML renders the visual report (informe HTML v3 over v1 and v2):
// the header, the three steps, what we could not read, the «More» strip
// and the footer, from v and the catalog only. home is the user's folder,
// for the paths of the fix boxes.
func RenderHTML(v V2, cat catalog.Catalog, home string, loc *time.Location) string {
	return renderHTML(v, cat, home, loc, func(string) bool { return false })
}

// renderHTML is RenderHTML; exists says whether a file is in the reports
// directory (the memory's links, v2 §5).
func renderHTML(v V2, cat catalog.Catalog, home string, loc *time.Location, exists func(string) bool) string {
	if loc == nil {
		loc = time.Local
	}
	at, err := time.Parse(time.RFC3339, v.GeneratedAt)
	if err != nil {
		at = time.Now()
	}
	local := at.In(loc)
	var sb strings.Builder
	c := htmlCtx{v: v, cat: cat, home: home, sb: &sb, score: map[string]*index.AgentIndex{}, gloss: new(int), exists: exists, rows: credRows(v)}
	for i := range v.Index.Agents {
		c.score[v.Index.Agents[i].AgentID] = &v.Index.Agents[i]
	}
	c.w(`<!DOCTYPE html><html lang="%s"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>%s</title><style>%s</style></head><body><main>`,
		html.EscapeString(cat.Lang()), c.t("html.doc_title", nil), htmlCSS())

	// §1.1 Header.
	c.w(`<div class="hdr">%s</div>`, c.t("html.header", map[string]string{"date": cat.DayLabel(local)}))

	// §2 The flow: 1 → 2 → 3.
	today, hasToday := c.todayFinding()
	c.w(`<div class="flow">`)
	c.renderStep1()
	c.renderStep2(today, hasToday)
	c.renderStep3(today.ID)
	c.w(`</div>`)

	// §11 How your agents work: right after the steps.
	c.renderOverview()

	// §6.1 What we could not read, only with coverage gaps.
	cov := v.Coverage
	if len(cov.SourcesFailed)+len(cov.AgentsUnsupported)+len(cov.ScanErrors) > 0 || !cov.GraphBuilt {
		c.w(`<div class="cov"><h3>%s</h3><p>%s</p><ul>`, c.t("html.coverage.title", nil), c.t("html.coverage.body", nil))
		for _, f := range cov.SourcesFailed {
			code, _, _ := strings.Cut(f.Reason, ": ")
			c.w(`<li><code>%s</code> (%s)</li>`, html.EscapeString(f.Path), html.EscapeString(code))
		}
		for _, a := range cov.AgentsUnsupported {
			c.w(`<li><code>%s</code> (%s)</li>`, html.EscapeString(a.ConfigPath), html.EscapeString(a.Platform))
		}
		for _, s := range cov.ScanErrors {
			c.w(`<li><code>%s</code></li>`, html.EscapeString(s))
		}
		if !cov.GraphBuilt {
			c.w(`<li><code>%s</code></li>`, graph.FileName)
		}
		c.w(`</ul></div>`)
	}

	// §9.4 Other signals on this computer (adjustment B): the red
	// technical-report categories, not in the score.
	if len(v.Signals) > 0 {
		c.w(`<div class="cov"><h3>%s</h3><p class="note">%s</p><ul class="sig">`, c.t("html.signals.title", nil), c.t("html.signals.note", nil))
		for _, sg := range v.Signals {
			n := ""
			if sg.Count > 1 {
				n = ` <span class="n">· ` + strconv.Itoa(sg.Count) + `</span>`
			}
			c.w(`<li><span class="dot" style="background:var(--redfill)"></span>%s%s</li>`, c.t("html.signal."+sg.Category, nil), n)
		}
		c.w(`</ul></div>`)
	}

	// §6.2 More: memory, what we don't know, your AI, the card.
	c.w(`<div class="more"><div>`)
	c.renderMemory()
	c.w(`</div><div><h3>%s</h3>`, c.t("html.unknown.title", nil))
	for _, a := range v.Index.Scope.Evaluated {
		c.w(`<p>%s</p>`, c.t(unknownBody(a), map[string]string{"agent": a}))
	}
	c.w(`</div><div><h3>%s</h3><p>%s</p><button type="button" class="b2" id="ai-copy" data-done="%s">%s</button><span class="live" aria-live="polite"></span><button type="button" class="b2" id="ai-download" data-file="rosmarops-ai-%s.md">%s</button><template id="ai-export">%s</template></div>`,
		c.t("html.ai.title", nil), c.t("html.ai.note", nil), c.t("html.fix.copied", nil), c.t("html.ai.copy", nil), local.Format("2006-01-02"), c.t("html.ai.download", nil),
		html.EscapeString(RenderAIExport(v, cat, home)))
	c.w(`<div><h3>%s</h3><p>%s</p>`, c.t("html.card.title", nil), c.t("html.card.note", nil))
	if a, ok := c.scored(); ok {
		if svg, text, ok := index.Card(*a, v.Silenced.Count, local, cat); ok {
			day := local.Format("2006-01-02")
			c.w(`<button type="button" class="b2" id="card-button" data-svg="rosmarops-card-%s.svg" data-txt="rosmarops-card-%s.txt">%s</button><template id="card-svg">%s</template><template id="card-txt">%s</template>`,
				day, day, c.t("html.card.button", nil), svg, html.EscapeString(text))
		}
	}
	c.w(`</div></div>`)

	// §6.3 Footer: the technical report (the agents not evaluated moved to
	// step 1, v3 §13.1).
	c.w(`<p class="foot">%s</p>`, c.t("html.legacy.pointer", map[string]string{"file": v.base + ".md"}))
	c.w(`</main><script>%s</script></body></html>`+"\n", htmlJS)
	return sb.String()
}

// scoredAgents are the evaluated agents with a score, lowest first (ties
// by name; v3 §3 point 6).
func (c htmlCtx) scoredAgents() []*index.AgentIndex {
	var out []*index.AgentIndex
	for i := range c.v.Index.Agents {
		if c.v.Index.Agents[i].Value != nil {
			out = append(out, &c.v.Index.Agents[i])
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if *out[i].Value != *out[j].Value {
			return *out[i].Value < *out[j].Value
		}
		return out[i].Agent < out[j].Agent
	})
	return out
}

// scored is the agent with the lowest score: the step 1 score and the card
// (v3 §3 point 6).
func (c htmlCtx) scored() (*index.AgentIndex, bool) {
	if s := c.scoredAgents(); len(s) > 0 {
		return s[0], true
	}
	return nil, false
}

// unknownBody is the key of an evaluated agent's «What we don't know»
// paragraph (v3 §3 point 6): Cursor has its own.
func unknownBody(agent string) string {
	if agent == graph.ClientDisplayName(knowledge.CursorProduct) {
		return "html.unknown.body.cursor"
	}
	return "html.unknown.body"
}

// ofScoredAgent reports a finding of the score's agent: its agent's, or
// one with no agent, which weighs in every agent's score.
func (c htmlCtx) ofScoredAgent(f rules.Finding, a *index.AgentIndex) bool {
	return rules.IsMachineFinding(f) || len(f.Evidence.Nodes) > 0 && f.Evidence.Nodes[0] == a.AgentID
}

// todayFinding is today's finding, when there is one with its action.
func (c htmlCtx) todayFinding() (rules.Finding, bool) {
	for _, f := range c.v.Findings {
		if f.ID == c.v.Today.FindingID {
			return f, true
		}
	}
	return rules.Finding{}, false
}

// stepHead is a step's head (v3 §2): the decorative number, the title as
// tag with its id, the hint under it, and the arrow to the next step.
func (c htmlCtx) stepHead(n int, tag, id, title, hint string, arrow bool) {
	if hint != "" {
		hint = `<span class="hint">` + hint + `</span>`
	}
	idAttr := ""
	if id != "" {
		idAttr = ` id="` + id + `"`
	}
	c.w(`<div class="head"><div class="num" aria-hidden="true">%d</div><div><%s class="step"%s>%s</%s>%s</div>`, n, tag, idAttr, title, tag, hint)
	if arrow {
		c.w(`<span class="line" aria-hidden="true"></span>`)
	}
	c.w(`</div>`)
}

// renderStep1 is v3 §3: your situation.
func (c htmlCtx) renderStep1() {
	v := c.v
	c.w(`<section class="s1" aria-labelledby="step1">`)
	c.stepHead(1, "h2", "step1", c.t("html.step.1", nil), "", true)
	c.w(`<div class="iname"><span>%s</span>%s</div>`, c.t("html.index.name", nil), c.help("score"))
	a, scored := c.scored()
	switch {
	case scored:
		of := c.cat.Text("html.index.of", nil)
		c.w(`<div class="score" role="img" aria-label="%s" style="color:%s">%d<span class="of">%s</span></div>`,
			html.EscapeString(strconv.Itoa(*a.Value)+of), bandVar[a.Band], *a.Value, html.EscapeString(of))
	default:
		reason := index.ReasonNotApplicable
		if len(v.Index.Agents) > 0 {
			reason = v.Index.Agents[0].Unavailable
		}
		c.w(`<div class="unavail">%s</div>`, c.t("html.index.unavailable", map[string]string{"reason": c.cat.Text("html.index.reason."+reason, nil)}))
	}

	// The score's agent and band, right under it (v3 §10.1).
	if scored {
		c.w(`<p class="sagent">%s · %s%s</p>`, html.EscapeString(a.Agent), c.t("band."+a.Band, nil), c.help("agent"))
		c.w(`<p class="scale">%s</p>`, c.t("html.index.scale", nil)) // v3 §13.1
	}

	// The headline (h1): the first finding of the score's agent (v3 §10.2),
	// else the report's first; without findings, the perfect line at 100,
	// else the status.
	headline := c.t("html.status."+v.Status, nil)
	switch {
	case len(v.Findings) > 0:
		f := v.Findings[0]
		if scored {
			for _, x := range v.Findings {
				if c.ofScoredAgent(x, a) {
					f = x
					break
				}
			}
		}
		headline = c.t("html.headline."+f.Rule, map[string]string{"agent": f.Params.Agent})
	case scored && *a.Value == 100:
		headline = c.t("html.index.perfect", nil)
	}
	c.w(`<h1 class="hl">%s</h1><p class="meta">`, headline)
	red, yellow := RowCounts(v)
	c.w(`%s`, c.t("html.step.1.counts", map[string]string{"red": strconv.Itoa(red), "yellow": strconv.Itoa(yellow)}))
	if n := v.Silenced.Count; n > 0 {
		c.w(`<br>%s`, html.EscapeString(c.cat.Plural("html.silenced", n, map[string]string{"count": strconv.Itoa(n)})))
	}
	if n := v.TestData.Count; n > 0 { // v3 §12.1
		c.w(`<br>%s`, html.EscapeString(c.cat.Plural("html.testdata", n, map[string]string{"count": strconv.Itoa(n)})))
	}
	c.w(`<br>%d %s`, v.repos, c.t("html.count.repos", nil))
	// Which agents are evaluated, and which detected but not yet (v3 §13.1).
	if ev := v.Index.Scope.Evaluated; len(ev) > 0 {
		if ne := v.Index.Scope.NotEvaluated; len(ne) > 0 {
			c.w(`<br>%s`, c.t("html.step.1.coverage", map[string]string{"evaluated": listNames(c.cat, ev), "pending": listNames(c.cat, ne)}))
		} else {
			c.w(`<br>%s`, c.t("html.step.1.coverage.all", map[string]string{"evaluated": listNames(c.cat, ev)}))
		}
	}
	if scored {
		switch cmp := a.Comparison; cmp.Status {
		case index.CompareDirect:
			if *cmp.Previous == *a.Value {
				c.w(`<br>%s`, c.t("html.index.unchanged", nil))
			} else {
				c.w(`<br>%s`, c.t("html.index.delta", map[string]string{"prev": strconv.Itoa(*cmp.Previous), "now": strconv.Itoa(*a.Value)}))
			}
			if d := cmp.MethodDelta; d != nil && *d != 0 {
				c.w(` %s`, c.t("html.index.method_change", map[string]string{"delta": fmt.Sprintf("%+d", *d)}))
			}
		case index.CompareNotComparableRules:
			c.w(`<br>%s`, c.t("html.index.not_comparable.rules", nil))
		case index.CompareNotComparableScope:
			c.w(`<br>%s`, c.t("html.index.not_comparable.scope", map[string]string{"prev": strconv.Itoa(cmp.PrevRepos), "now": strconv.Itoa(cmp.NowRepos)}))
		}
	}
	// The other evaluated agents, by score (v3 §3 point 6).
	if scored {
		for _, o := range c.scoredAgents()[1:] {
			c.w(`<br>%s`, c.t("html.step.1.other", map[string]string{"agent": o.Agent, "score": strconv.Itoa(*o.Value), "band": c.cat.Text("band."+o.Band, nil)}))
		}
	}
	if v.Index.HistoryReset {
		c.w(`<br>%s`, c.t("html.history.reset", nil))
	}
	c.w(`</p></section>`)
}

// gitEnvHelp is the «i» env after cred_in_git's impact, only when the key
// is also in a .env* file (v3 §9.2).
func (c htmlCtx) gitEnvHelp(f rules.Finding) string {
	if f.Rule != rules.RuleCredInGit {
		return ""
	}
	for _, file := range graph.CredentialFiles(graph.Node{Locations: f.Evidence.Locations}) {
		if strings.HasPrefix(strings.ToLower(filepath.Base(filepath.FromSlash(strings.ReplaceAll(file, `\`, "/")))), ".env") {
			return c.help("env")
		}
	}
	return ""
}

// renderRotate is the body of rotate_credential (v3 §9.2): the three steps,
// the button that copies html.agent.prompt.rotated with the configuration
// file and the entry, and «I'd rather do it by hand» with the same entry.
// wide is the step 2 button.
func (c htmlCtx) renderRotate(f rules.Finding, a rules.Action, wide bool) {
	p := c.params(f)
	c.w(`<ol class="rot"><li>%s</li><li>%s</li><li>%s</li></ol>`, c.t("html.fix.rotate.step1", p), c.t("html.fix.rotate.step2", p), c.t("html.fix.rotate.step3", p))
	pp := copyParams(p)
	pp["file"] = userPath(a.File, c.home, "%USERPROFILE%")
	class := ""
	if wide {
		class = ` class="wide"`
	}
	c.w(`<pre hidden>%s

%s</pre><button type="button"%s data-copied="%s">%s</button><span class="live" aria-live="polite"></span>`,
		c.t("html.agent.prompt.rotated", pp), html.EscapeString(a.Value), class, c.t("html.fix.copied", p), c.t("html.fix.copy.generic", p))
	mp := copyParams(p)
	mp["file"] = userPath(a.File, c.home, "~")
	// By hand, the entry inside its list, nested in full (v3 §14.3).
	c.w(`<details class="manual"><summary><span class="chev" aria-hidden="true">›</span>%s</summary><ol><li>%s</li><li>%s<pre>%s</pre></li><li>%s</li></ol></details>`,
		c.t("html.fix.manual", p), c.manualOpen(mp), c.t("html.fix.manual.rotated", p), html.EscapeString(rotatedNested(a.Value)), c.t("html.fix.manual.save", p))
}

// rotatedNested is the rotate_credential entry inside
// {"allow_list": {"rotated_credentials": [entry]}}, indented by 2 spaces
// as the entry is (v3 §14.3). An entry that is not a JSON object is
// returned as it is.
func rotatedNested(entry string) string {
	var e map[string]any
	if json.Unmarshal([]byte(entry), &e) != nil {
		return entry
	}
	out, err := json.MarshalIndent(map[string]any{"allow_list": map[string]any{"rotated_credentials": []any{e}}}, "", "  ")
	if err != nil {
		return entry
	}
	return string(out)
}

// adjustActions are the actions that change a settings file (v3 §4: the
// hint and the copy button).
var adjustActions = map[string]bool{
	rules.ActionAskReadRule: true, rules.ActionAskEditRule: true, rules.ActionBlockReadsOutside: true, rules.ActionManualDefaultMode: true,
}

// renderStep2 is v3 §4: what you do today.
func (c htmlCtx) renderStep2(f rules.Finding, ok bool) {
	var a rules.Action
	found := false
	if ok {
		for _, x := range f.Actions {
			if x.ID == c.v.Today.ActionID {
				a, found = x, true
			}
		}
	}
	if !found {
		c.w(`<section class="now" aria-labelledby="step2">`)
		c.stepHead(2, "p", "step2", c.t("html.step.2", nil), "", true)
		c.w(`<p class="none">%s</p></section>`, c.t("html.none", nil))
		return
	}
	p := c.params(f)
	hint := ""
	if adjustActions[a.Code] {
		hint = c.t("html.step.2.hint", nil)
	}
	c.w(`<section class="now" aria-labelledby="today-title">`)
	c.stepHead(2, "p", "", c.t("html.step.2", nil), hint, true)
	c.w(`<div class="tbox"><h2 id="today-title">%s</h2><div class="lbl">%s</div><p class="tf">%s%s%s</p><p class="imp">%s`,
		c.t("html.action."+a.Code+".title", p), c.t("html.today.fixes", nil), c.rowTitle(f), c.masterHelp(f), c.others(f), c.impact(f))
	if f.Rule == rules.RuleCredReadAndSend && f.Radius == rules.RadiusAnyProject {
		c.w(` %s%s`, c.t("html.today.reach", p), c.help("auto"))
	}
	c.w(`%s</p>`, c.gitEnvHelp(f))
	// The projection (BV-41): only with a score, a rise, and an action that
	// is not partial.
	if s, ok := c.agentScore(f); ok && s.Value != nil && s.Projection != nil &&
		s.Projection.Value > *s.Value && a.Completeness != rules.CompletenessPartial {
		key := "html.today.projection"
		if len(c.v.Index.Scope.Evaluated) > 1 {
			key = "html.today.projection.agent" // v3 §10.3: whose score it is
		}
		c.w(`<div class="proj">%s</div>`, c.t(key, map[string]string{"agent": s.Agent, "now": strconv.Itoa(*s.Value), "then": strconv.Itoa(s.Projection.Value)}))
	}
	after := "html.after.full"
	if a.Code == rules.ActionRotateCredential {
		c.renderRotate(f, a, true)
		c.w(`<p class="after">%s</p></div></section>`, c.t("html.after.rotate", p)) // v3 §13.2
		return
	}
	if a.Code == rules.ActionRemoveFromDisk {
		c.w(`<p>%s</p><ul>`, c.t("html.fix.remove", p))
		for _, l := range a.Locations {
			c.w(`<li><code>%s</code></li>`, html.EscapeString(l))
		}
		c.w(`</ul>`)
	} else {
		prompt, add := "html.agent.prompt.merge", "html.fix.manual.add"
		if !a.FileExists {
			prompt, add = "html.agent.prompt.create", "html.fix.manual.create"
		}
		pp := copyParams(p)
		pp["file"] = userPath(a.File, c.home, "%USERPROFILE%")
		c.w(`<pre hidden>%s

%s</pre><button type="button" class="wide" data-copied="%s">%s</button><span class="live" aria-live="polite"></span>`,
			c.t(prompt, pp), html.EscapeString(a.Value), c.t("html.fix.copied", p), c.t("html.fix.copy", p))
		c.w(`<p class="how">%s %s</p>`, c.t("html.fix.step2", p), c.t("html.fix.step3", p))
		mp := copyParams(p)
		mp["file"] = userPath(a.File, c.home, "~")
		settings := ""
		if strings.EqualFold(filepath.ToSlash(mp["file"]), "~/.claude/settings.json") {
			settings = c.help("settings")
		}
		c.w(`<details class="manual"><summary><span class="chev" aria-hidden="true">›</span>%s</summary><ol><li>%s%s</li><li>%s<pre>%s</pre></li><li>%s</li></ol></details>`,
			c.t("html.fix.manual", p), c.manualOpen(mp), settings, c.t(add, p), html.EscapeString(a.Value), c.t("html.fix.manual.save", p))
		after = map[string]string{
			rules.CompletenessFull: "html.after.full", rules.CompletenessRadiusOnly: "html.after.radius", rules.CompletenessPartial: "html.after.partial",
		}[a.Completeness]
	}
	env := ""
	if a.Completeness == rules.CompletenessRadiusOnly {
		env = c.help("env")
	}
	c.w(`<p class="after">%s%s</p></div></section>`, c.t(after, p), env)
}

// step3Cap is how many rows step 3 shows before the rest fold (v3 §12.3).
const step3Cap = 8

// renderStep3 is v3 §5: every row but today's, in two groups; one row per
// credential (§12.2), the first step3Cap shown and the rest folded (§12.3).
func (c htmlCtx) renderStep3(todayID string) {
	var list []rules.Finding
	for _, f := range c.v.Findings {
		if r := c.rows[f.ID]; !slices.Contains(r.ids, todayID) {
			list = append(list, f)
		}
	}
	// Within each group, the score's agent's rows first, each agent in the
	// report's order (v3 §10.5).
	if a, ok := c.scored(); ok {
		sort.SliceStable(list, func(i, j int) bool { return c.ofScoredAgent(list[i], a) && !c.ofScoredAgent(list[j], a) })
	}
	var rows []rules.Finding
	for _, sev := range []string{rules.SeverityRed, rules.SeverityYellow} {
		done := map[*credRow]bool{}
		for _, f := range list {
			if r := c.rows[f.ID]; f.Severity == sev && !done[r] {
				done[r] = true
				rows = append(rows, r.rep)
			}
		}
	}
	c.w(`<section aria-labelledby="step3">`)
	n := strconv.Itoa(len(rows))
	c.stepHead(3, "h2", "step3", c.t("html.step.3", nil), html.EscapeString(c.cat.Plural("html.step.3.hint", len(rows), map[string]string{"count": n})), false)
	if len(rows) == 0 {
		c.w(`<p class="empty">%s</p></section>`, c.t("html.step.3.none", nil))
		return
	}
	for i, f := range rows {
		if i == step3Cap {
			rest := len(rows) - step3Cap
			c.w(`<details class="rest"><summary><span class="chev" aria-hidden="true">›</span>%s</summary>`,
				html.EscapeString(c.cat.Plural("html.step.3.more", rest, map[string]string{"count": strconv.Itoa(rest)})))
		}
		if i == 0 || i == step3Cap || f.Severity != rows[i-1].Severity {
			c.w(`<div class="grp"><span class="dot" style="background:%s"></span>%s</div>`, sevFill(f.Severity), c.t("html.group."+f.Severity, nil))
		}
		c.renderRow(f)
	}
	if len(rows) > step3Cap {
		c.w(`</details>`)
	}
	c.w(`</section>`)
}

// renderRow is one row of step 3 (v3 §5): the title and the memory's
// verdict as text; open, the card body of v1 §3.6.
func (c htmlCtx) renderRow(f rules.Finding) {
	p := c.params(f)
	c.w(`<details class="row"><summary><span class="chev" aria-hidden="true">›</span><span class="t">%s</span><span class="x">%s</span></summary><div class="rb"><p>%s%s</p>`,
		c.rowTitle(f), c.verdict(f), c.impact(f), c.configHelp(f)+c.gitEnvHelp(f))
	if o := c.others(f); o != "" {
		c.w(`<p>%s</p>`, o)
	}
	if f.Radius == rules.RadiusAnyProject {
		c.w(`<p><span class="chip-any">%s</span></p>`, c.t("html.chip.any_project", p))
	}
	if c.multi(f) {
		c.renderMultiRow(f)
		return
	}
	if g := c.gain(f, "html.gain"); g != "" {
		c.w(`%s`, g)
	}
	if a, ok := rules.BestAction(f, c.v.Findings); ok {
		c.renderFix(f, a, "html.fix.title")
	}
	c.w(`<details class="tech"><summary>%s</summary><p>%s `, c.t("html.tech", nil), c.t("html.tech.where", nil))
	c.where(f.Evidence.Locations)
	c.w(`</p><p>%s</p><ul>`, c.t("html.tech.confidence."+f.Confidence, p))
	c.assumptions(f)
	c.w(`</ul></details></div></details>`)
}

// gain is the p.gain line of f with key (html.gain, or html.gain.agent
// with {agent}): the score of f's agent without f, when it has one.
func (c htmlCtx) gain(f rules.Finding, key string) string {
	a, ok := c.agentScore(f)
	if !ok || a.Value == nil {
		return ""
	}
	for _, wt := range a.Weights {
		if wt.FindingID == f.ID {
			return `<p class="gain">` + c.t(key, map[string]string{"agent": f.Params.Agent, "now": strconv.Itoa(*a.Value), "then": strconv.Itoa(wt.Without)}) + `</p>`
		}
	}
	return ""
}

// renderMultiRow is the rest of a merged row's body (v3 §14.1): a fix box
// per agent with its own action, gain and texts; one details.tech with the
// locations joined and each agent's confidence and assumptions.
func (c htmlCtx) renderMultiRow(f rules.Finding) {
	ms := c.members(f)
	for _, m := range ms {
		if a, ok := rules.BestAction(m, c.v.Findings); ok {
			c.renderFixBox(m, a, c.t("html.fix.title.agent", map[string]string{"agent": m.Params.Agent}), c.gain(m, "html.gain.agent"))
		}
	}
	var locs []string
	for _, m := range ms {
		locs = append(locs, m.Evidence.Locations...)
	}
	c.w(`<details class="tech"><summary>%s</summary><p>%s `, c.t("html.tech", nil), c.t("html.tech.where", nil))
	c.where(uniqueSorted(locs))
	c.w(`</p>`)
	for _, m := range ms {
		c.w(`<p><b>%s</b></p><p>%s</p><ul>`, html.EscapeString(m.Params.Agent), c.t("html.tech.confidence."+m.Confidence, c.params(m)))
		c.assumptions(m)
		c.w(`</ul>`)
	}
	c.w(`</details></div></details>`)
}

// where is the «Where» line's locations: the first five, then how many
// more.
func (c htmlCtx) where(locs []string) {
	for i, l := range locs {
		if i == 5 {
			c.w(` %s`, c.t("html.tech.more", map[string]string{"count": strconv.Itoa(len(locs) - 5)}))
			break
		}
		c.w(`<code>%s</code> `, html.EscapeString(l))
	}
}

// assumptions are f's own assumptions as list items, with f's params.
func (c htmlCtx) assumptions(f rules.Finding) {
	p := c.params(f)
	for _, a := range f.Evidence.Assumptions {
		if !sharedAssumption[a] {
			c.w(`<li>%s</li>`, c.t("unknown."+a, p))
		}
	}
}

// renderMemory is the memory block of the More strip (v3 §6.2, v2 §5): the
// first run's line, or the evolution, the counts and the reason; the
// moved and gone lines folded.
func (c htmlCtx) renderMemory() {
	m := c.v.Memory
	c.w(`<h3>%s</h3>`, c.t("html.memory.title", nil))
	for _, a := range c.v.NewAgents {
		c.w(`<p>%s</p>`, c.t("html.memory.new_agent", map[string]string{"agent": a}))
	}
	for _, s := range c.v.NewMCP { // v3 §13.3
		c.w(`<p>%s</p>`, c.t("html.memory.new_mcp", map[string]string{"name": s}))
	}
	if !c.v.memoryPrev {
		c.w(`<p>%s</p>`, c.t("html.memory.first", nil))
		return
	}
	join := func(ns []int) string {
		out := make([]string, len(ns))
		for i, n := range ns {
			out[i] = strconv.Itoa(n)
		}
		return strings.Join(out, " → ")
	}
	if len(c.v.Index.Scope.Evaluated) > 1 {
		// One series per agent, in step 1's order (v3 §10.4).
		for _, a := range c.scoredAgents() {
			if s := c.v.seriesBy[a.Agent]; len(s) > 0 {
				c.w(`<p>%s</p>`, c.t("html.memory.evolution.agent", map[string]string{"agent": a.Agent, "series": join(s)}))
			}
		}
	} else if len(c.v.series) > 0 {
		c.w(`<p>%s</p>`, c.t("html.memory.evolution", map[string]string{"series": join(c.v.series)}))
	}
	c.w(`<p class="mcounts">%s</p>`, c.t("memory.counts", map[string]string{
		"new": strconv.Itoa(m.Counts[memory.VerdictNew]), "still": strconv.Itoa(m.Counts[memory.VerdictStillObserved]),
		"gone": strconv.Itoa(m.Counts[memory.VerdictNoLongerObserved]), "again": strconv.Itoa(m.Counts[memory.VerdictObservedAgain]),
	}))
	if m.Reason != "" {
		c.w(`<p>%s</p>`, c.t("memory.not_comparable", map[string]string{"reason": c.cat.Text("memory.reason."+m.Reason, nil)}))
	}
	var lines []string
	for _, mv := range m.Moved {
		lines = append(lines, c.t("memory.moved", map[string]string{"rule": mv.Rule}))
	}
	for _, g := range m.NoLongerObserved {
		report := html.EscapeString(g.LastReport)
		if name := g.LastReport + ".html"; c.exists(name) {
			report = `<a href="` + html.EscapeString(name) + `">` + html.EscapeString(name) + `</a>`
		}
		lines = append(lines, c.withHTML("memory.no_longer_observed.line", map[string]string{
			"severity": c.cat.Text("severity."+g.Severity, nil), "rule": g.Rule, "open_since": g.OpenSince,
		}, "last_report", report))
	}
	if len(lines) > 0 {
		c.w(`<details><summary>%s</summary>`, html.EscapeString(c.cat.Plural("html.memory.more", len(lines), map[string]string{"count": strconv.Itoa(len(lines))})))
		for _, l := range lines {
			c.w(`<p>%s</p>`, l)
		}
		c.w(`</details>`)
	}
}

// withHTML is key's text, escaped, with the parameter name replaced by the
// markup raw.
func (c htmlCtx) withHTML(key string, p map[string]string, name, raw string) string {
	q := copyParams(p)
	q[name] = "\x00"
	parts := strings.SplitN(c.cat.Text(key, q), "\x00", 2)
	if len(parts) != 2 {
		return c.t(key, p)
	}
	return html.EscapeString(parts[0]) + raw + html.EscapeString(parts[1])
}

// verdict is the memory verdict of a row, as text (v3 §5); none on the
// first run and for not_comparable.
func (c htmlCtx) verdict(f rules.Finding) string {
	if !c.v.memoryPrev || f.Memory == nil {
		return ""
	}
	switch f.Memory.Verdict {
	case memory.VerdictNew:
		return c.t("html.verdict.new", nil)
	case memory.VerdictStillObserved:
		date := f.Memory.OpenSince
		if d, err := time.Parse("2006-01-02", f.Memory.OpenSince); err == nil {
			date = c.cat.DayMonthLabel(d)
		}
		return c.t("html.verdict.still", map[string]string{"date": date})
	case memory.VerdictObservedAgain:
		return c.t("html.verdict.again", nil)
	case memory.VerdictNotChecked:
		return c.t("html.verdict.not_checked", nil)
	}
	return ""
}

// renderFix is the fix box of v1 §5 for action a, titled titleKey: the
// body of a step 3 row (v3 §5), whose only «i» is config.
func (c htmlCtx) renderFix(f rules.Finding, a rules.Action, titleKey string) {
	c.renderFixBox(f, a, c.t(titleKey, c.params(f)), "")
}

// renderFixBox is renderFix with its title, and a gain line after it (the
// boxes of a merged row, v3 §14.1), already escaped.
func (c htmlCtx) renderFixBox(f rules.Finding, a rules.Action, title, gain string) {
	p := c.params(f)
	c.w(`<div class="fix"><div class="fix-t">%s</div>%s`, title, gain)
	if a.Code == rules.ActionRotateCredential {
		c.renderRotate(f, a, false)
		c.w(`<div class="after">%s<span>%s</span></div></div>`, icon("green", "var(--grnfill)", 22), c.t("html.after.rotate", p)) // v3 §13.2
		return
	}
	if a.Code == rules.ActionRemoveFromDisk {
		c.w(`<p>%s</p><ul>`, c.t("html.fix.remove", p))
		for _, l := range a.Locations {
			c.w(`<li><code>%s</code></li>`, html.EscapeString(l))
		}
		c.w(`</ul>`)
		c.w(`<div class="after">%s<span>%s</span></div></div>`, icon("green", "var(--grnfill)", 22), c.t("html.after.full", p))
		return
	}
	prompt := "html.agent.prompt.merge"
	if !a.FileExists {
		prompt = "html.agent.prompt.create"
	}
	pp := copyParams(p)
	pp["file"] = userPath(a.File, c.home, "%USERPROFILE%")
	c.w(`<ol><li>%s</li><li>%s</li><li>%s</li></ol>`, c.t("html.fix.step1", p), c.t("html.fix.step2", p), c.t("html.fix.step3", p))
	c.w(`<pre>%s

%s</pre><button type="button" data-copied="%s">%s</button><span class="live" aria-live="polite"></span>`, c.t(prompt, pp), html.EscapeString(a.Value), c.t("html.fix.copied", p), c.t("html.fix.copy", p))
	mp := copyParams(p)
	mp["file"] = userPath(a.File, c.home, "~")
	add := "html.fix.manual.add"
	if !a.FileExists {
		add = "html.fix.manual.create"
	}
	c.w(`<details class="manual"><summary><span class="chev" aria-hidden="true">›</span>%s</summary><ol><li>%s</li><li>%s<pre>%s</pre></li><li>%s</li></ol></details>`,
		c.t("html.fix.manual", p), c.manualOpen(mp), c.t(add, p), html.EscapeString(a.Value), c.t("html.fix.manual.save", p))
	after := map[string]string{
		rules.CompletenessFull: "html.after.full", rules.CompletenessRadiusOnly: "html.after.radius", rules.CompletenessPartial: "html.after.partial",
	}[a.Completeness]
	c.w(`<div class="after">%s<span>%s</span></div></div>`, icon("green", "var(--grnfill)", 22), c.t(after, p))
}

// manualOpen is html.fix.manual.open with the path in code.
func (c htmlCtx) manualOpen(p map[string]string) string {
	return c.withHTML("html.fix.manual.open", p, "file", "<code>"+html.EscapeString(p["file"])+"</code>")
}

func copyParams(p map[string]string) map[string]string {
	q := make(map[string]string, len(p))
	for k, v := range p {
		q[k] = v
	}
	return q
}

// agentScore is the score of a finding's agent.
func (c htmlCtx) agentScore(f rules.Finding) (*index.AgentIndex, bool) {
	if rules.IsMachineFinding(f) {
		return c.scored() // in every agent's score: the step 1 agent's
	}
	if len(f.Evidence.Nodes) == 0 {
		return nil, false
	}
	a, ok := c.score[f.Evidence.Nodes[0]]
	return a, ok
}

// params are the HTML texts' params of a finding: agent, target_agent,
// count, repos, cred_name and family.
func (c htmlCtx) params(f rules.Finding) map[string]string {
	p := findingParams(f)
	p["repos"] = strconv.Itoa(c.v.repos)
	p["cred_name"] = c.cat.Text("html.cred."+c.credKey(f)+".name", nil)
	p["family"] = familyName(f)
	return p
}

// familyName is the name of the family of the finding's first member, or
// the folder of its first location (§5, html.after.radius).
func familyName(f rules.Finding) string {
	if len(f.Group.Members) > 0 && f.Group.Members[0].Family != "" {
		return filepath.Base(filepath.FromSlash(strings.ReplaceAll(f.Group.Members[0].Family, `\`, "/")))
	}
	for _, l := range f.Evidence.Locations {
		files := graph.CredentialFiles(graph.Node{Locations: []string{l}})
		if len(files) == 1 {
			dir := strings.ReplaceAll(files[0], `\`, "/")
			if i := strings.LastIndex(dir, "/"); i > 0 {
				dir = dir[:i]
			}
			if i := strings.LastIndex(dir, "/"); i >= 0 {
				return dir[i+1:]
			}
			return dir
		}
	}
	return ""
}

// modelAPIs are the services whose impact is html.cred.model_api.impact.
var modelAPIs = map[string]bool{"openai": true, "openrouter": true, "anthropic": true}

// credKey resolves <k> of html.cred.<k> (§4): db for a db_credential;
// <service>.<scope> when the key exists; <service> when it exists (model
// APIs have a name, and the model_api impact); else generic.
func (c htmlCtx) credKey(f rules.Finding) string {
	svc, scope := f.Params.Service, f.Params.Scope
	switch {
	case f.CredType() == graph.CredentialDBCredential:
		return "db"
	case f.CredType() == graph.CredentialPrivateKey:
		return "private_key"
	case scope != "" && c.cat.Has("html.cred."+svc+"."+scope+".name"):
		return svc + "." + scope
	case c.cat.Has("html.cred." + svc + ".name"):
		return svc
	}
	return "generic"
}

// title is the finding's title (§4), escaped.
func (c htmlCtx) title(f rules.Finding) string { return html.EscapeString(c.titleText(f)) }

// titleText is the finding's title (§4).
func (c htmlCtx) titleText(f rules.Finding) string {
	p := c.params(f)
	if f.Rule == rules.RuleCredReadAndSend || f.Rule == rules.RuleCredReadable {
		return c.cat.Text("html.f.cred.title", p)
	}
	if f.Rule == rules.RuleCredInGit {
		return c.cat.Text("html.f.cred_in_git.title", p)
	}
	return c.cat.Text("html.f."+f.Rule+".title", p)
}

// rowTitle is the title of the finding's row (v3 §12.2): with the agents
// of its credential and rule joined, escaped.
func (c htmlCtx) rowTitle(f rules.Finding) string {
	r := c.rows[f.ID]
	if r == nil || len(r.agents) < 2 {
		return c.title(f)
	}
	p := c.params(f)
	p["agent"] = listNames(c.cat, r.agents)
	return html.EscapeString(c.cat.Text("html.f.cred.title.multi", p)) // v3 §14.1
}

// members are the findings of f's row, in the row's order (v3 §12.2).
func (c htmlCtx) members(f rules.Finding) []rules.Finding {
	r := c.rows[f.ID]
	if r == nil {
		return []rules.Finding{f}
	}
	byID := map[string]rules.Finding{}
	for _, x := range c.v.Findings {
		byID[x.ID] = x
	}
	out := make([]rules.Finding, 0, len(r.ids))
	for _, id := range r.ids {
		out = append(out, byID[id])
	}
	return out
}

// multi reports a row of more than one agent (v3 §14.1).
func (c htmlCtx) multi(f rules.Finding) bool {
	r := c.rows[f.ID]
	return r != nil && len(r.agents) > 1
}

// masterHelp is the «i» of master after the title of a finding whose
// credential is the Supabase service_role key (§4).
func (c htmlCtx) masterHelp(f rules.Finding) string {
	if (f.Rule == rules.RuleCredReadAndSend || f.Rule == rules.RuleCredReadable) && c.credKey(f) == "supabase.service_role" {
		return c.help("master")
	}
	return ""
}

// configHelp is the «i» of config at the end of the impact of
// foreign_agent_config_writable (§4).
func (c htmlCtx) configHelp(f rules.Finding) string {
	if f.Rule == rules.RuleForeignAgentConfigWritable {
		return c.help("config")
	}
	return ""
}

// others is the pill after a credential finding's title with the other keys
// of the merged finding (M12, BV-43); empty when there are none.
func (c htmlCtx) others(f rules.Finding) string {
	n := f.OthersCount()
	if n == 0 || (f.Rule != rules.RuleCredReadAndSend && f.Rule != rules.RuleCredReadable) {
		return ""
	}
	return `<span class="tchip">` + html.EscapeString(c.cat.Plural("html.f.cred.others", n, map[string]string{"count": strconv.Itoa(n)})) + `</span>`
}

// impact is the finding's impact (§4), escaped.
func (c htmlCtx) impact(f rules.Finding) string { return html.EscapeString(c.impactText(f)) }

// impactText is the finding's impact (§4).
func (c htmlCtx) impactText(f rules.Finding) string {
	p := c.params(f)
	if f.Rule == rules.RuleCredInGit {
		if f.Params.HasRemote != nil && *f.Params.HasRemote {
			return c.cat.Text("html.f.cred_in_git.impact.remote", p)
		}
		return c.cat.Text("html.f.cred_in_git.impact.local", p)
	}
	if f.Rule != rules.RuleCredReadAndSend && f.Rule != rules.RuleCredReadable {
		return c.cat.Text("html.f."+f.Rule+".impact", p)
	}
	k := c.credKey(f)
	key := "html.cred." + k + ".impact"
	if modelAPIs[f.Params.Service] && k == f.Params.Service {
		key = "html.cred.model_api.impact"
	}
	out := c.cat.Text(key, p)
	if f.Rule == rules.RuleCredReadable {
		if c.multi(f) { // v3 §14.1: the agents of the row
			pm := copyParams(p)
			pm["agent"] = listNames(c.cat, c.rows[f.ID].agents)
			out += " " + c.cat.Text("html.f.cred_readable.extra.multi", pm)
		} else {
			out += " " + c.cat.Text("html.f.cred_readable.extra", p)
		}
	}
	return out
}

// HTMLKeys are the catalog codes the HTML can emit (§9): html.*, band.*,
// the unknown.* of the technical details and the memory's codes.
func HTMLKeys() []string {
	keys := []string{
		"html.doc_title", "html.header", "html.status.green", "html.status.yellow", "html.status.red", "html.list.and",
		"html.index.of", "html.index.name", "html.index.perfect", "html.index.delta", "html.index.not_comparable.rules",
		"html.index.not_comparable.scope", "html.index.unavailable",
		"html.index.reason.coverage", "html.index.reason.not_applicable",
		"html.card.button", "html.card.note", "html.card.counts", "html.card.method",
		"html.count.repos", "html.none",
		"html.today.reach",
		"html.group.red", "html.group.yellow", "html.chip.any_project", "html.gain",
		"html.tech", "html.tech.where", "html.tech.more", "html.tech.confidence.high", "html.tech.confidence.medium",
		"html.unknown.title", "html.unknown.body", "html.coverage.title", "html.coverage.body",
		"html.f.cred.title", "html.f.cred_readable.extra", "html.cred.model_api.impact",
		"html.fix.title", "html.fix.step1", "html.fix.step2", "html.fix.step3", "html.fix.copy", "html.fix.copied",
		"html.fix.manual", "html.fix.manual.open", "html.fix.manual.add", "html.fix.manual.create", "html.fix.manual.save",
		"html.fix.remove", "html.agent.prompt.merge", "html.agent.prompt.create",
		"html.after.full", "html.after.radius", "html.after.partial",
		"html.history.reset", "html.today.projection",
		"html.f.cred.others.one", "html.f.cred.others.other",
		// v2
		"html.index.unchanged", "html.legacy.pointer",
		"html.ai.note", "html.ai.copy", "html.ai.download",
		"ai.instruction", "ai.what", "ai.today", "ai.findings", "ai.limits", "ai.more",
		"html.memory.title", "html.memory.first", "html.memory.evolution",
		"memory.counts", "memory.not_comparable", "memory.moved", "memory.no_longer_observed.line",
		"memory.reason." + memory.ReasonRulesChanged, "memory.reason." + memory.ReasonScopeChanged, "memory.reason." + memory.ReasonSaltChanged,
		"severity.red", "severity.yellow",
		"html.verdict.new", "html.verdict.still", "html.verdict.again", "html.verdict.not_checked",
	}
	// v3
	keys = append(keys, "html.step.1", "html.step.1.counts", "html.step.2", "html.step.2.hint", "html.step.3",
		"html.step.3.hint.one", "html.step.3.hint.other", "html.step.3.none", "html.today.fixes", "html.ai.title", "html.card.title",
		"html.step.1.other", "html.unknown.body.cursor", "html.today.projection.agent", "html.memory.evolution.agent")
	// v3 §9 (BV-50)
	keys = append(keys, "html.f.cred_in_git.title", "html.f.cred_in_git.impact.remote", "html.f.cred_in_git.impact.local",
		"html.fix.rotate.step1", "html.fix.rotate.step2", "html.fix.rotate.step3", "html.agent.prompt.rotated", "html.fix.copy.generic",
		"html.silenced.one", "html.silenced.other", "html.card.silenced", "html.memory.new_agent",
		"html.signals.title", "html.signals.note", "html.index.method_change")
	// v3 §12 (BV-54)
	keys = append(keys, "html.testdata.one", "html.testdata.other", "html.step.3.more.one", "html.step.3.more.other")
	// BV-55: the memory fold's label (v3 §6.2)
	keys = append(keys, "html.memory.more.one", "html.memory.more.other")
	// v3 §13 (BV-56)
	keys = append(keys, "html.index.scale", "html.step.1.coverage", "html.step.1.coverage.all", "html.after.rotate", "html.memory.new_mcp")
	// v3 §14 (BV-57)
	keys = append(keys, "html.f.cred.title.multi", "html.f.cred_readable.extra.multi", "html.fix.title.agent", "html.gain.agent", "html.fix.manual.rotated")
	// v3 §11 (BV-61)
	keys = append(keys, overviewKeys()...)
	for _, sg := range SignalOrder {
		keys = append(keys, "html.signal."+sg)
	}
	for _, r := range rules.AllRules {
		keys = append(keys, "html.headline."+r)
	}
	for _, a := range rules.AllActions {
		keys = append(keys, "html.action."+a+".title")
	}
	for _, g := range glossTerms {
		keys = append(keys, "html.gloss."+g+".q", "html.gloss."+g+".a")
	}
	for _, b := range index.AllBands {
		keys = append(keys, "band."+b)
	}
	for _, r := range []string{rules.RuleForeignAgentConfigWritable, rules.RuleForeignAgentInstructionsWritable, rules.RuleOwnConfigWritable, rules.RuleOwnInstructionsWritable} {
		keys = append(keys, "html.f."+r+".title", "html.f."+r+".impact")
	}
	for _, k := range []string{"db", "private_key", "generic", "supabase", "supabase.service_role", "supabase.secret", "github", "npm", "railway", "stripe", "aws", "slack"} {
		keys = append(keys, "html.cred."+k+".name", "html.cred."+k+".impact")
	}
	for _, k := range []string{"openai", "openrouter", "anthropic"} {
		keys = append(keys, "html.cred."+k+".name")
	}
	for _, a := range rules.AllAssumptions {
		if !sharedAssumption[a] {
			keys = append(keys, "unknown."+a)
		}
	}
	sort.Strings(keys)
	return keys
}

// glossTerms are the terms of the help «i» (informe HTML v2, §4).
var glossTerms = []string{"agent", "score", "master", "auto", "env", "config", "settings"}

// LegacyHTMLKeys are the HTML keys allowed to fall back to English (v1 §9):
// none since the legacy section left the HTML (v2 §3, CTO-7 M5).
var LegacyHTMLKeys = map[string]bool{}

// WriteHTML writes the visual report to dir, as base.html and report.html
// (§1). Both are in the reports directory.
func WriteHTML(v V2, cat catalog.Catalog, home string, loc *time.Location, dir, base string) (string, error) {
	page := []byte(renderHTML(v, cat, home, loc, func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, filepath.Base(name)))
		return err == nil
	}))
	path := filepath.Join(dir, base+".html")
	if err := os.WriteFile(path, page, 0o600); err != nil {
		return "", fmt.Errorf("write html report: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, HTMLFileName), page, 0o600); err != nil {
		return "", fmt.Errorf("write html report: %w", err)
	}
	return path, nil
}
