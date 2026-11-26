package report

import (
	"strconv"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/rules"
)

// bv307Texts are the signed texts of BV-307 (R1, R2 and R5), word for word,
// with the two that BV-311 (T2) corrected.
var bv307Texts = []struct{ key, es, en string }{
	{"html.coverage.timeout", "{k} proyectos no se terminaron de revisar. Cada uno pasó del minuto que RosmarOps le da. Lo que encontró en ellos hasta ese momento está en este informe; lo que le faltaba por mirar, no:", "{k} projects were not checked to the end. Each one went over the minute RosmarOps gives it. What it found in them up to that point is in this report; what was left to check is not:"}, // BV-311 (T2)
	{"html.coverage.timeout.one", "1 proyecto no se terminó de revisar. Pasó del minuto que RosmarOps le da. Lo que encontró en él hasta ese momento está en este informe; lo que le faltaba por mirar, no:", "1 project was not checked to the end. It went over the minute RosmarOps gives it. What it found in it up to that point is in this report; what was left to check is not:"},                           // BV-311 (T2)
	{"html.coverage.timeout.after", "Suele pasar con proyectos muy grandes o con mucho historial de git. Vuelve a pasar RosmarOps. Si el mismo proyecto se repite, hoy RosmarOps no puede acabarlo y su parte del informe sigue incompleta.", "This usually happens with very large projects or with a long git history. Run RosmarOps again. If the same project comes up again, RosmarOps cannot finish it today and its part of the report stays incomplete."},
	{"html.coverage.reason.source_unsupported", "no se leyó: es un tipo de fichero o de enlace que RosmarOps no lee", "not read: it is a kind of file or link RosmarOps does not read"},
	{"html.coverage.reason.read_error", "no se pudo abrir", "could not be opened"},
	{"html.coverage.reason.parse_error", "se abrió, pero su contenido no se pudo entender", "opened, but its contents could not be understood"},
	{"html.coverage.reason.size_limit_exceeded", "no se abrió: pesa más de 2 MB", "not opened: it is larger than 2 MB"},
	{"html.coverage.reason.file_count_limit_exceeded", "tiene más ficheros de los que RosmarOps lee por proyecto; el resto no se leyó", "has more files than RosmarOps reads per project; the rest were not read"},
	{"problems.b.text", "RosmarOps se cerró antes de acabar la última vez. Si nos lo envías, sabremos que pasó, y en qué versión y sistema. Solo se envían códigos de error, la versión y el sistema.", "RosmarOps closed before finishing last time. If you send it to us, we will know it happened, and on which version and system. Only error codes, the version and the system are sent."},
}

func TestBV307Texts(t *testing.T) {
	es, _ := catalog.Load("es")
	en, _ := catalog.Load("en")
	for _, x := range bv307Texts {
		if got := es.Text(x.key, map[string]string{"k": "{k}"}); got != x.es {
			t.Errorf("es %s = %q, want %q", x.key, got, x.es)
		}
		if got := en.Text(x.key, map[string]string{"k": "{k}"}); got != x.en {
			t.Errorf("en %s = %q, want %q", x.key, got, x.en)
		}
	}
	// Every reason code has its text.
	for _, code := range graph.ReasonCodes {
		for _, cat := range []catalog.Catalog{es, en} {
			if !cat.Has("html.coverage.reason." + code) {
				t.Errorf("no text for reason %s", code)
			}
		}
	}
}

func coverageCtx(cat catalog.Catalog, v V2) (htmlCtx, *strings.Builder) {
	var sb strings.Builder
	return htmlCtx{v: v, cat: cat, sb: &sb, score: map[string]*index.AgentIndex{}, gloss: new(int), rows: credRows(v)}, &sb
}

func timeoutGroups(repos ...string) []LegacyGroup {
	var gs []LegacyGroup
	for _, r := range repos {
		gs = append(gs, LegacyGroup{Category: "scan_timeout", Detail: "per_repo", Family: r, Count: 1, Repos: []string{r}})
	}
	return gs
}

// R1: «What we could not read» names the projects that did not finish,
// between the paragraph and the line after; the findings line links to the
// paragraph; with and without a space, with and without other gaps.
func TestBV307NotFinishedProjects(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		for _, tc := range []struct {
			repos []string
			key   string
		}{
			{[]string{`C:\src\b`, `C:\src\a`}, "html.coverage.timeout"},
			{[]string{`C:\src\a`}, "html.coverage.timeout.one"},
		} {
			for _, space := range []bool{false, true} {
				for _, others := range []bool{false, true} {
					v := V2{Coverage: V2Coverage{GraphBuilt: true}, SpaceConnected: space}
					v.Legacy.Groups = timeoutGroups(tc.repos...)
					if others {
						v.Coverage.SourcesFailed = []graph.SourceFailed{{Path: `C:\x\settings.json`, Reason: "parse_error: bad"}}
					}
					c, sb := coverageCtx(cat, v)
					c.renderCoverage()
					got := sb.String()
					k := strconv.Itoa(len(tc.repos))
					para := `<p id="not-finished">` + escapeHTML(cat.Text(tc.key, map[string]string{"k": k})) + `</p><ul>`
					if !strings.Contains(got, para) {
						t.Errorf("%s %s space=%v: no paragraph %q in %s", lang, k, space, para, got)
					}
					list := `<li><code>C:\src\a</code></li>`
					if len(tc.repos) == 2 {
						list += `<li><code>C:\src\b</code></li>`
					}
					if !strings.Contains(got, list+`</ul><p class="cov-after">`+escapeHTML(cat.Text("html.coverage.timeout.after", nil))+`</p>`) {
						t.Errorf("%s %s: not the projects, by path, then the line after: %s", lang, k, got)
					}
					body := cat.Text("html.coverage.body", nil)
					if others != strings.Contains(got, escapeHTML(body)) {
						t.Errorf("%s %s others=%v: the list of other gaps is drawn=%v", lang, k, others, !others)
					}
					if others && strings.Index(got, "not-finished") > strings.Index(got, escapeHTML(body)) {
						t.Errorf("%s: the projects not finished are not before the rest", lang)
					}
				}
			}
		}
	}
}

// R1: no project not finished, no paragraph, no link; no gaps at all, no
// block.
func TestBV307NothingNotFinished(t *testing.T) {
	cat, _ := catalog.Load("en")
	c, sb := coverageCtx(cat, V2{Coverage: V2Coverage{GraphBuilt: true}})
	c.renderCoverage()
	if sb.Len() != 0 {
		t.Errorf("a block with nothing in it: %s", sb.String())
	}
	if got := c.r3FindingsHTML(3); strings.Contains(got, "<a ") {
		t.Errorf("a link with no project not finished: %s", got)
	}
}

// R1: the findings line's «{k} projects not finished» is the link, the same
// text as before.
func TestBV307FindingsLink(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		for _, repos := range [][]string{{"/a"}, {"/a", "/b", "/c"}} {
			v := V2{}
			v.Legacy.Groups = timeoutGroups(repos...)
			c, _ := coverageCtx(cat, v)
			k := len(repos)
			key, tail := "findings.count.at_least", cat.Text("findings.count.at_least", map[string]string{"n": "9", "k": strconv.Itoa(k)})
			if k == 1 {
				key = "findings.count.at_least.one_project"
				tail = cat.Text(key, map[string]string{"n": "9"})
			}
			full := cat.Text(key, map[string]string{"n": "9", "k": strconv.Itoa(k)})
			_, tail, _ = strings.Cut(tail, " · ")
			want := escapeHTML(strings.TrimSuffix(full, tail)) + `<a href="#not-finished">` + escapeHTML(tail) + `</a>`
			if got := c.r3FindingsHTML(9); got != want {
				t.Errorf("%s %d: %q, want %q", lang, k, got, want)
			}
		}
	}
}

// R2: each source not read says why in words, in the report and in the
// space view; never its reason code.
func TestBV307ReasonsInWords(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		v := V2{Coverage: V2Coverage{GraphBuilt: true}}
		for _, code := range graph.ReasonCodes {
			v.Coverage.SourcesFailed = append(v.Coverage.SourcesFailed, graph.SourceFailed{Path: `C:\u\` + code, Reason: graph.Reason(code, "detail")})
		}
		c, sb := coverageCtx(cat, v)
		c.renderCoverage()
		page := sb.String()
		lines := svCoverage(c)
		for i, code := range graph.ReasonCodes {
			text := cat.Text("html.coverage.reason."+code, nil)
			if want := `<li><code>C:\u\` + code + `</code> — ` + escapeHTML(text) + `</li>`; !strings.Contains(page, want) {
				t.Errorf("%s: no %q", lang, want)
			}
			if want := `C:\u\` + code + ` — ` + text; lines[i] != want {
				t.Errorf("%s space view: %q, want %q", lang, lines[i], want)
			}
			if strings.Contains(page, "("+code+")") || strings.Contains(strings.Join(lines, "\n"), "("+code+")") {
				t.Errorf("%s: the code %s is still shown", lang, code)
			}
		}
	}
}

func soleFinding(id, rule, agent, sev string, mem *rules.FindingMemory) rules.Finding {
	return rules.Finding{
		ID: id, Rule: rule, Severity: sev, Params: rules.Params{Agent: agent},
		Evidence: rules.Evidence{Nodes: []string{agent}, Locations: []string{"/h/" + agent + "/settings.json"}}, Memory: mem,
	}
}

// R3: a problem whose only row has the problem's title draws the row's body
// open under its header, with no title of its own; its «open since» moves
// to the header, open or folded. Other problems keep their rows.
func TestBV307SoleRow(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		mem := &rules.FindingMemory{Verdict: memory.VerdictStillObserved, OpenSince: "2026-11-20"}
		var fs []rules.Finding
		// Eight problems of their own, one row each: six open, two folded.
		// Two agents per rule and level, so none is a single row of three
		// agents or more (BV-297).
		i := 0
		for _, rule := range []string{rules.RuleOwnConfigWritable, rules.RuleOwnInstructionsWritable} {
			for _, sev := range []string{rules.SeverityRed, rules.SeverityYellow} {
				for _, a := range []string{"Cline", "Codex"} {
					fs = append(fs, soleFinding("o"+strconv.Itoa(i), rule, a, sev, mem))
					i++
				}
			}
		}
		c, sb := detailCtx(cat, fs, nil)
		c.v.memoryPrev = true
		iss := c.issues()
		c.renderR3Steps(iss)
		page := sb.String()
		if strings.Contains(page, `<details class="row">`) {
			t.Errorf("%s: a sole row still has its own title", lang)
		}
		if got := strings.Count(page, `<div class="rb rb-sole" id="o`); got != 8 {
			t.Errorf("%s: %d sole rows drawn open, want 8", lang, got)
		}
		since := `<span class="x r3-pv">` + escapeHTML(cat.Text("html.verdict.still", map[string]string{"date": c.openSince(iss[0])})) + `</span>`
		if got := strings.Count(page, since+`</h2>`); got != detailOpen {
			t.Errorf("%s: %d open headers with the row's «open since», want %d", lang, got, detailOpen)
		}
		if got := strings.Count(page, `<span class="r3-fold-r">`+since); got != 2 {
			t.Errorf("%s: %d folded lines with the row's «open since», want 2", lang, got)
		}
		for _, f := range fs {
			if strings.Count(page, `<div class="rb rb-sole" id="`+f.ID+`">`) != 1 {
				t.Errorf("%s: %s is not drawn once, open", lang, f.ID)
			}
		}
	}
}

// R3: a problem with two rows, or whose row has another title, keeps its
// rows as today.
func TestBV307NotSoleRow(t *testing.T) {
	cat, _ := catalog.Load("en")
	fs, svc := albertCase()
	c, sb := detailCtx(cat, fs, svc)
	iss := c.issues()
	for _, is := range iss {
		_, sole := c.soleRowOf(is)
		rows := len(c.issueRows(is))
		if sole && (rows != 1 || is.kind == issueConfigMany || is.kind == issueOwnMany) {
			t.Errorf("%s: drawn as a sole row with %d rows, kind %s", c.issueTitle(is), rows, is.kind)
		}
		if rows == 1 && !sole && is.kind == issueOne {
			t.Errorf("%s: a problem of its own with one row keeps its row's title", c.issueTitle(is))
		}
	}
	c.renderR3Steps(iss)
	if !strings.Contains(sb.String(), `<details class="row">`) {
		t.Error("no problem keeps its rows")
	}
}
