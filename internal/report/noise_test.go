package report_test

import (
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/rules"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// noiseReport is the real case of BV-54 point 7 (decision 2026-10-26,
// ruido del informe): a Supabase key in scripts/smoke_test.ps1 and
// vars_raw.txt of 27 clones of the webapp family; other keys only in
// *_test.go, tests/*.test.mjs, .env.example and the history of test files
// of /src/tool; and the Supabase service_role key of /src/a, within Claude
// Code's and Cursor's reach.
func noiseReport(t *testing.T, extra ...platform.CredentialEntry) report.V2 {
	t.Helper()
	salt := []byte("0123456789abcdef0123456789abcdef")
	id := graph.Identifier(salt)
	sb := "sb_secret_" + strings.Repeat("S", 22) + "_" + strings.Repeat("M", 8)
	stripe := func(c string) string { return "sk_live_" + strings.Repeat(c, 24) }
	roots := []string{"/src/a", "/src/b", "/src/tool"}
	fams := [][]string{{"/src/a"}, {"/src/b"}, {"/src/tool"}}
	var webapp []string
	var cs []finding.CommittedSecret
	for i := 0; i < 27; i++ {
		root := fmt.Sprintf("/src/webapp-%02d", i)
		webapp = append(webapp, root)
		cs = append(cs,
			finding.CommittedSecret{PatternID: "supabase_secret_key", Ref: id("supabase_secret_key", sb), Repo: root, Presence: finding.PresenceTracked, File: "scripts/smoke_test.ps1", Line: 3, HasRemote: true},
			finding.CommittedSecret{PatternID: "supabase_secret_key", Ref: id("supabase_secret_key", sb), Repo: root, Presence: finding.PresenceTracked, File: "vars_raw.txt", Line: 1, HasRemote: true})
	}
	roots = append(roots, webapp...)
	fams = append(fams, webapp)
	for _, k := range []struct{ c, presence, file string }{
		{"A", finding.PresenceTracked, "internal/scan/secrets/patterns_test.go"},
		{"B", finding.PresenceTracked, "tests/no-secret-in-tree.test.mjs"},
		{"C", finding.PresenceTracked, ".env.example"},
		{"D", finding.PresenceHistoryOnly, "testdata/keys.txt"},
	} {
		line := 0
		if k.presence == finding.PresenceTracked {
			line = 2
		}
		cs = append(cs, finding.CommittedSecret{PatternID: "stripe_secret_key", Ref: id("stripe_secret_key", stripe(k.c)), Repo: "/src/tool", Presence: k.presence, File: k.file, Line: line})
	}
	v1Findings := []finding.Finding{{ID: "st:1", Severity: finding.SeverityRed, Category: "secret_tracked", Evidence: finding.Evidence{RepoPath: "/src/webapp-00", File: "vars_raw.txt"}, Committed: cs}}
	v1 := report.Build("t", v1Findings, nil, 0, 0, 0, 0, nil)
	v1.GeneratedAt = v2Now.Format(time.RFC3339)
	mcp := platform.MCPScan{Sources: []platform.MCPSource{{Platform: "cursor", ConfigPath: "/h/.cursor/mcp.json", ConfigDir: "/h/.cursor", Status: platform.MCPSourceRead, LoadsMCP: true}}}
	settings := platform.ClaudeSettingsScan{Files: []platform.ClaudeSettingsFile{{
		Scope: platform.MCPScopeUser, ConfigPath: "/h/.claude/settings.json", ConfigDir: "/h/.claude", Status: platform.MCPSourceRead,
		Permissions: platform.ClaudePermissions{Allow: []string{"Bash(npx tsx *)"}},
	}}}
	cf := platform.CredentialFileScan{Entries: []platform.CredentialEntry{
		{Path: "/src/a/dashboard/.env.local", Line: 4, Key: "SUPABASE_SERVICE_ROLE_KEY", Value: secrets.NewSecretValue(serviceRoleJWT)},
	}}
	cf.Entries = append(cf.Entries, extra...)
	f, _, err := graph.FromScansWithRepos(mcp, settings, platform.CursorScan{}, cf, graph.Repos{Findings: v1Findings}, salt, v2Now)
	if err != nil {
		t.Fatal(err)
	}
	g, err := graph.Infer(graph.Build(f, graph.Provenance{EngineVersion: "t", RulesVersion: "0", KnowledgeVersion: knowledge.Version, HostID: graph.HostID(salt)}, v2Now), roots, "/h")
	if err != nil {
		t.Fatal(err)
	}
	bases, _ := knowledge.All()
	v, _ := report.BuildV2(report.V2Input{
		Graph: &g, GraphSHA256: strings.Repeat("ab", 32), Bases: bases, Base: bases[knowledge.ClaudeCodeProduct],
		RepoRoots: roots, Families: fams, V1: v1, GeneratedAt: v2Now, ConfigFile: "/h/.rosmarops/config.json",
		Salt: salt,
	})
	return v
}

// BV-54 point 7, the real case: one cred_in_git, the shared key's, with its
// 27 repositories and both files; the other four keys counted as test data,
// in the HTML's step 1 and in the Markdown with their files; the
// service_role key within both agents' reach, one row with both names.
func TestNoise_RealCase(t *testing.T) {
	v := noiseReport(t)
	var git []rules.Finding
	readers := map[string]bool{}
	for _, f := range v.Findings {
		switch f.Rule {
		case rules.RuleCredInGit:
			git = append(git, f)
		case rules.RuleCredReadAndSend, rules.RuleCredReadable:
			if f.Params.Service == "supabase" {
				readers[f.Params.Agent] = true
			}
		}
	}
	if len(git) != 1 || git[0].Params.Count != 27 {
		t.Fatalf("cred_in_git %+v", git)
	}
	locs := strings.Join(git[0].Evidence.Locations, "\n")
	if strings.Count(locs, "smoke_test.ps1") != 27 || strings.Count(locs, "vars_raw.txt") != 27 {
		t.Errorf("locations %v", git[0].Evidence.Locations)
	}
	if v.TestData.Count != 4 || len(v.TestData.Files) != 4 {
		t.Fatalf("test data %+v", v.TestData)
	}
	if !readers["Claude Code"] || !readers["Cursor"] {
		t.Fatalf("the service_role key is not within both agents' reach: %v", readers)
	}
	// report.v2.json carries no test data.
	raw, err := report.RenderV2JSON(v)
	if err != nil {
		t.Fatal(err)
	}
	if i := strings.Index(string(raw), "patterns_test.go"); i >= 0 {
		t.Errorf("report.v2.json names a test-data file: %s", string(raw)[max(0, i-400):i+40])
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		line := htmlEscape(cat.Plural("html.testdata", 4, map[string]string{"count": "4"}))
		_, meta, _ := strings.Cut(page, `<p class="meta">`)
		meta, _, _ = strings.Cut(meta, `</p>`)
		if !strings.Contains(meta, "<br>"+line+"<br>") {
			t.Errorf("%s: step 1 lacks %q: %s", lang, line, meta)
		}
		want := htmlEscape(cat.Text("html.f.cred.title.multi", map[string]string{
			"cred_name": cat.Text("html.cred.supabase.service_role.name", nil),
			"agent":     listNames(cat, rowAgents(v)),
		}))
		if n := strings.Count(page, want); n != 1 {
			t.Errorf("%s: %d rows %q", lang, n, want)
		}
		for _, a := range []string{"Claude Code", "Cursor"} {
			single := htmlEscape(cat.Text("html.f.cred.title", map[string]string{"cred_name": cat.Text("html.cred.supabase.service_role.name", nil), "agent": a}))
			if strings.Contains(page, `<span class="t">`+single+`</span>`) {
				t.Errorf("%s: a row of %s alone", lang, a)
			}
		}
		if m := regexp.MustCompile(`\[(html|band|memory|unknown)\.[a-z0-9_.]+\]`).FindString(page); m != "" {
			t.Errorf("%s: key rendered %s", lang, m)
		}
		writeCapture(t, "noise."+lang+".html", page)
	}
	cat, _ := catalog.Load("en")
	md := report.RenderV2Markdown(v, cat, time.UTC)
	assertRenderedTexts(t, md)
	if !strings.Contains(md, cat.Plural("report.testdata", 4, map[string]string{"count": "4"})) {
		t.Error("markdown lacks the test-data line")
	}
	for _, f := range v.TestData.Files {
		if !strings.Contains(md, "- "+cat.Text("report.testdata.entry", map[string]string{"repo": f.Repo, "path": f.Path})+"\n") {
			t.Errorf("markdown lacks %s %s", f.Repo, f.Path)
		}
	}
	var heads []string
	for _, l := range strings.Split(md, "\n") {
		if strings.HasPrefix(l, "### ") {
			heads = append(heads, l)
		}
	}
	var rows []string
	for _, h := range heads {
		if strings.Contains(h, "service_role") {
			rows = append(rows, h)
		}
	}
	if len(rows) != 1 || !strings.Contains(rows[0], listNames(cat, rowAgents(v))) {
		t.Errorf("markdown rows of the service_role key: %q", rows)
	}
	writeCapture(t, "noise.en.md", md)
}

// rowAgents are the evaluated agents in the order of v3 §3 point 6.
func rowAgents(v report.V2) []string {
	var out []string
	for _, a := range v.Index.Agents {
		if a.Value != nil {
			out = append(out, a.Agent)
		}
	}
	if len(out) == 2 {
		a, b := v.Index.Agents[0], v.Index.Agents[1]
		if *b.Value < *a.Value || *b.Value == *a.Value && b.Agent < a.Agent {
			out[0], out[1] = out[1], out[0]
		}
	}
	return out
}

func listNames(cat catalog.Catalog, names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + cat.Text("html.list.and", nil) + names[len(names)-1]
}

// writeCapture writes a rendered page for the owner's captures, only with
// BV54_CAPTURES set to a directory.
func writeCapture(t *testing.T, name, content string) {
	if dir := os.Getenv("BV54_CAPTURES"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// v3 §12.3: with 20 rows in step 3, 8 shown and 12 in the fold, whose
// summary counts them; the hint still counts the 20; the fold repeats the
// group label of its first row.
func TestNoise_Step3Cap(t *testing.T) {
	base := noiseReport(t)
	var git rules.Finding
	for _, f := range base.Findings {
		if f.Rule == rules.RuleCredInGit {
			git = f
		}
	}
	for _, n := range []int{8, 9, 20} {
		v := cloneV2(base)
		v.Today = rules.Today{}
		v.Findings = nil
		for i := 0; i < n; i++ {
			f := git
			f.ID = fmt.Sprintf("f_%016d", i)
			if i >= 5 {
				f.Severity = rules.SeverityYellow
			}
			v.Findings = append(v.Findings, f)
		}
		for _, lang := range []string{"en", "es"} {
			cat, _ := catalog.Load(lang)
			page := renderHTML(t, lang, v)
			if n == 20 {
				writeCapture(t, "step3-cap."+lang+".html", page)
			}
			_, step3, _ := strings.Cut(page, `<section aria-labelledby="step3">`)
			step3, _, _ = strings.Cut(step3, `</section>`)
			if !strings.Contains(step3, htmlEscape(cat.Plural("html.step.3.hint", n, map[string]string{"count": fmt.Sprint(n)}))) {
				t.Errorf("%d %s: the hint does not count every row", n, lang)
			}
			shown, fold, folded := strings.Cut(step3, `<details class="rest">`)
			if got := strings.Count(shown, `<details class="row">`); got != min(n, 8) {
				t.Errorf("%d %s: %d rows shown", n, lang, got)
			}
			if folded != (n > 8) {
				t.Fatalf("%d %s: fold %v", n, lang, folded)
			}
			if !folded {
				continue
			}
			rest := n - 8
			summary := `<summary><span class="chev" aria-hidden="true">›</span>` + htmlEscape(cat.Plural("html.step.3.more", rest, map[string]string{"count": fmt.Sprint(rest)})) + `</summary><div class="grp">`
			if !strings.HasPrefix(fold, summary) || strings.Count(fold, `<details class="row">`) != rest {
				t.Errorf("%d %s: fold %.300s", n, lang, fold)
			}
			if lang == "es" && n == 20 && !strings.Contains(fold, "Ver los 12 avisos restantes") {
				t.Error("es: no «Ver los 12 avisos restantes»")
			}
		}
	}
}

// BV-55 point 1: every count of findings the user sees counts rows as v3
// §12.2 groups them: step 1 and the console add up to the rows of steps 2
// and 3; the report's file name keeps counting findings. One credential
// within two agents' reach adds 1, not 2.
func TestNoise_CountsByRow(t *testing.T) {
	full := noiseReport(t)
	only := cloneV2(full)
	only.Today, only.Findings = rules.Today{}, nil
	for _, f := range full.Findings {
		if f.Params.Scope == "service_role" {
			only.Findings = append(only.Findings, f)
		}
	}
	if len(only.Findings) != 2 {
		t.Fatalf("%d service_role findings, want one per agent", len(only.Findings))
	}
	for name, v := range map[string]report.V2{"real case": full, "one key, two agents": only} {
		red, yellow := report.RowCounts(v)
		fr, fy := report.V2Counts(v)
		if fr+fy <= red+yellow {
			t.Fatalf("%s: rows %d+%d, findings %d+%d: test would be vacuous", name, red, yellow, fr, fy)
		}
		if name == "one key, two agents" && red+yellow != 1 {
			t.Errorf("%s: rows %d+%d", name, red, yellow)
		}
		for _, lang := range []string{"en", "es"} {
			cat, _ := catalog.Load(lang)
			page := renderHTML(t, lang, v)
			counts := htmlEscape(cat.Text("html.step.1.counts", map[string]string{"red": fmt.Sprint(red), "yellow": fmt.Sprint(yellow)}))
			if !strings.Contains(page, `<p class="meta">`+counts) {
				t.Errorf("%s %s: step 1 lacks %q", name, lang, counts)
			}
			shown := strings.Count(page, `<div class="tbox">`)
			_, step3, _ := strings.Cut(page, `<section aria-labelledby="step3">`)
			step3, _, _ = strings.Cut(step3, `</section>`)
			if shown += strings.Count(step3, `<details class="row">`); shown != red+yellow {
				t.Errorf("%s %s: %d rows in steps 2 and 3, counts %d+%d", name, lang, shown, red, yellow)
			}
			line := cat.Text("console.summary", map[string]string{
				"status": cat.Text("report.status."+v.Status, nil), "red": fmt.Sprint(red), "yellow": fmt.Sprint(yellow),
				"legacy_red": fmt.Sprint(v.Legacy.Counts.Red), "legacy_yellow": fmt.Sprint(v.Legacy.Counts.Yellow),
			})
			if got := report.ConsoleSummary(v, cat, "/r/report.md"); !strings.HasPrefix(got, line+"\n") {
				t.Errorf("%s %s: console %q, want %q", name, lang, got, line)
			}
		}
		if fn := report.FilenameV2(report.Report{GeneratedAt: v.GeneratedAt}, v); !strings.HasSuffix(fn, fmt.Sprintf("__%dR-%dY", fr, fy)) {
			t.Errorf("%s: file name %s does not count findings", name, fn)
		}
	}
}

// BV-56, v3 §13, with the real case: the header says what RosmarOps did,
// not what left the computer; the scale under the score; the coverage line;
// step 3's hint without «when you have time»; rotate_credential's last line
// is html.after.rotate, in the HTML and the export for your AI. With an
// agent detected and not evaluated, the coverage line names it, and the
// footer no longer does.
func TestNoise_Wording(t *testing.T) {
	v := noiseReport(t)
	var today rules.Finding
	for _, f := range v.Findings {
		if f.ID == v.Today.FindingID {
			today = f
		}
	}
	if today.Rule != rules.RuleCredInGit {
		t.Fatalf("today is %s, not cred_in_git", today.Rule)
	}
	g := v2Graph(t)
	one := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		_, head, _ := strings.Cut(page, `<body><main>`)
		head, _, _ = strings.Cut(head, `</p>`)
		if strings.Contains(head, "nada ha salido") || strings.Contains(head, "nothing left") ||
			!strings.Contains(head, htmlEscape(cat.Text("html.header", map[string]string{"date": ""}))[:20]) {
			t.Errorf("%s: header %q", lang, head)
		}
		if !strings.Contains(page, `</p><p class="scale">`+htmlEscape(cat.Text("html.index.scale", nil))+`</p>`) {
			t.Errorf("%s: no scale under the score's agent line", lang)
		}
		if all := htmlEscape(cat.Text("html.step.1.coverage.all", map[string]string{"evaluated": listNames(cat, []string{"Claude Code", "Cursor"})})); !strings.Contains(page, "<br>"+all) {
			t.Errorf("%s: no coverage line %q", lang, all)
		}
		_, step3, _ := strings.Cut(page, `<section aria-labelledby="step3">`)
		step3, _, _ = strings.Cut(step3, `</section>`)
		hint := regexp.MustCompile(`<span class="hint">([^<]*)</span>`).FindStringSubmatch(step3)
		if hint == nil || strings.Contains(hint[1], "cuando tengas tiempo") || strings.Contains(hint[1], "when you have time") {
			t.Errorf("%s: step 3 hint %v", lang, hint)
		}
		after := htmlEscape(cat.Text("html.after.rotate", nil))
		if !strings.Contains(page, `<p class="after">`+after+`</p></div></section>`) || strings.Contains(page, htmlEscape(cat.Text("html.after.full", nil))+`</p></div></section>`) {
			t.Errorf("%s: step 2 does not end with html.after.rotate", lang)
		}
		if ai := report.RenderAIExport(v, cat, "/h"); !strings.Contains(ai, cat.Text("html.after.rotate", nil)+"\n") {
			t.Errorf("%s: the export for your AI lacks html.after.rotate", lang)
		}
		p1 := renderHTML(t, lang, one)
		cov := htmlEscape(cat.Text("html.step.1.coverage", map[string]string{"evaluated": "Claude Code", "pending": "Cursor"}))
		_, foot, _ := strings.Cut(p1, `<p class="foot">`)
		if !strings.Contains(p1, "<br>"+cov) || strings.Contains(foot, "Cursor") {
			t.Errorf("%s: coverage %q, footer %q", lang, cov, foot)
		}
	}
}

// v3 §14.6, a merged row of two agents in the HTML: two fix boxes, each
// with its agent's title, gain and copy button; one details.tech, whose
// assumptions name one agent each, never the agents joined; the findings
// of the row share their locations (the same credential). In the
// Markdown, the .multi texts and no «… and … uses»/«works as its».
func TestNoise_MergedRow(t *testing.T) {
	v := noiseReport(t)
	var row []rules.Finding
	for _, f := range v.Findings {
		if f.Params.Scope == "service_role" {
			row = append(row, f)
		}
	}
	if len(row) != 2 || strings.Join(row[0].Evidence.Locations, ",") != strings.Join(row[1].Evidence.Locations, ",") {
		t.Fatalf("row %d findings, locations differ or test vacuous", len(row))
	}
	agents := rowAgents(v)
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		title := htmlEscape(cat.Text("html.f.cred.title.multi", map[string]string{"cred_name": cat.Text("html.cred.supabase.service_role.name", nil), "agent": listNames(cat, agents)}))
		_, body, ok := strings.Cut(page, `<span class="t">`+title+`</span>`)
		if !ok {
			t.Fatalf("%s: no merged row %q", lang, title)
		}
		body, _, _ = strings.Cut(body, `</div></details>`)
		boxes := strings.Split(body, `<div class="fix">`)[1:]
		if len(boxes) != 2 {
			t.Fatalf("%s: %d fix boxes", lang, len(boxes))
		}
		for i, a := range agents {
			b := boxes[i]
			if !strings.HasPrefix(b, `<div class="fix-t">`+htmlEscape(cat.Text("html.fix.title.agent", map[string]string{"agent": a}))+`</div><p class="gain">`) ||
				!strings.Contains(b, htmlEscape(cat.Text("html.fix.copy", map[string]string{"agent": a}))+`</button>`) {
				t.Errorf("%s: box %d is not %s's: %.300s", lang, i, a, b)
			}
		}
		if strings.Contains(body, `<p class="gain">`+htmlEscape(strings.SplitN(cat.Text("html.gain", nil), "{", 2)[0])) {
			t.Errorf("%s: the row keeps its single gain line", lang)
		}
		if n := strings.Count(body, `<details class="tech">`); n != 1 {
			t.Errorf("%s: %d details.tech", lang, n)
		}
		joined := listNames(cat, agents)
		for _, ul := range regexp.MustCompile(`<ul>(.*?)</ul>`).FindAllStringSubmatch(body, -1) {
			if strings.Contains(ul[1], htmlEscape(joined)) {
				t.Errorf("%s: assumptions with the agents joined: %s", lang, ul[1])
			}
		}
		for _, a := range agents {
			if !strings.Contains(body, `<p><b>`+a+`</b></p>`) {
				t.Errorf("%s: no assumptions heading for %s", lang, a)
			}
		}
	}
	cat, _ := catalog.Load("en")
	md := report.RenderV2Markdown(v, cat, time.UTC)
	joined := listNames(cat, agents)
	for _, never := range []string{joined + " uses", joined + " works as its"} {
		if strings.Contains(md, never) {
			t.Errorf("markdown has %q", never)
		}
	}
	if !strings.Contains(md, cat.Text("finding.cred_read_and_send.what.multi", map[string]string{"agent": joined})) {
		t.Error("markdown lacks the .multi «What happens»")
	}
	for _, a := range agents {
		if !strings.Contains(md, "##### "+a+"\n") {
			t.Errorf("markdown lacks the subtitle of %s", a)
		}
	}
}

// v3 §14.3 and §14.6: the export for your AI of a key in git records it in
// allow_list.rotated_credentials (html.agent.prompt.rotated); by hand, the
// pre is valid JSON which, merged into a minimal valid config.json,
// validates against config.v1.
func TestNoise_RotationRecord(t *testing.T) {
	v := noiseReport(t)
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		ai := report.RenderAIExport(v, cat, "/h")
		if !strings.Contains(ai, "allow_list.rotated_credentials") {
			t.Errorf("%s: the export lacks allow_list.rotated_credentials", lang)
		}
		page := renderHTML(t, lang, v)
		_, manual, ok := strings.Cut(page, htmlEscape(cat.Text("html.fix.manual.rotated", nil))+`<pre>`)
		if !ok {
			t.Fatalf("%s: no html.fix.manual.rotated", lang)
		}
		manual, _, _ = strings.Cut(manual, `</pre>`)
		var nested map[string]any
		if err := json.Unmarshal([]byte(html.UnescapeString(manual)), &nested); err != nil {
			t.Fatalf("%s: the pre is not JSON: %v\n%s", lang, err, manual)
		}
		raw, _ := json.Marshal(config.DefaultConfig())
		var cfg map[string]any
		if err := json.Unmarshal(raw, &cfg); err != nil {
			t.Fatal(err)
		}
		al, _ := cfg["allow_list"].(map[string]any)
		if al == nil {
			al = map[string]any{}
			cfg["allow_list"] = al
		}
		for k, val := range nested["allow_list"].(map[string]any) {
			al[k] = val
		}
		out, _ := json.MarshalIndent(cfg, "", "  ")
		p := filepath.Join(t.TempDir(), "config.json")
		if err := os.WriteFile(p, out, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := config.LoadConfig(p); err != nil {
			t.Errorf("%s: merged config does not validate: %v\n%s", lang, err, out)
		}
	}
}

// v3 §14.5: a finding with no action has no «What to do» heading.
func TestNoise_NoEmptyWhatToDo(t *testing.T) {
	v := cloneV2(noiseReport(t))
	for i := range v.Findings {
		v.Findings[i].Actions = nil
	}
	cat, _ := catalog.Load("en")
	md := report.RenderV2Markdown(v, cat, time.UTC)
	if strings.Contains(md, "#### "+cat.Text("finding.part.todo", nil)) {
		t.Error("markdown has a «What to do» with no action")
	}
}

// v3 §14.5 as amended in BV-58: the line of credentials only in templates
// is left out only when every credential it counts is in the report.testdata
// list. With the real case, the .env.example key of /src/tool is both, so
// the line goes; add a key in an untracked .env.example that only the
// collector reads, which the list does not hold, and the line stays, with
// the same count it would have had.
func TestNoise_TemplatesPostureLine(t *testing.T) {
	cat, _ := catalog.Load("en")
	line := func(v report.V2) (int, bool) {
		for _, p := range v.Posture {
			if p.Code == rules.PostureInTemplatesOnly {
				params := map[string]string{"count": fmt.Sprint(p.Count)}
				for c, n := range p.ByClass {
					params[c] = fmt.Sprint(n)
				}
				return p.Count, strings.Contains(report.RenderV2Markdown(v, cat, time.UTC), "- "+cat.Plural(p.Code, p.Count, params)+"\n")
			}
		}
		return 0, false
	}
	listed := noiseReport(t)
	if n, shown := line(listed); n != 1 || shown || listed.TestData.Count == 0 {
		t.Errorf("all listed: count %d, line shown %v, test data %d", n, shown, listed.TestData.Count)
	}
	missing := noiseReport(t, platform.CredentialEntry{
		Path: "/src/b/.env.example", Line: 10, Key: "DATABASE_URL", Value: secrets.NewSecretValue("postgres://app:apppass@db.example.com:5432/app"),
	})
	if n, shown := line(missing); n != 2 || !shown {
		t.Errorf("one not listed: count %d, line shown %v", n, shown)
	}
	if missing.TestData.Count != listed.TestData.Count {
		t.Errorf("test data changed: %d → %d", listed.TestData.Count, missing.TestData.Count)
	}
}
