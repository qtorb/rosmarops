package report

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/rules"
)

// Catalog codes of the v2 Markdown. §6 lists most; the ones marked "render"
// are the headings and labels the fixed structure of §6 needs, which §6 does
// not list. CatalogKeys is the whole set: the catalog test checks it against
// messages/en.json both ways.
var staticKeys = []string{
	"report.status.green", "report.status.yellow", "report.status.red",
	"report.scope", "report.coverage_gap",
	"section.today", "section.findings", "section.posture", "section.coverage", "section.legacy",
	"today.nothing_urgent", "today.why_this", "today.what_remains", "today.verify",
	"radius.any_project", "radius.same_project",
	"confidence.high", "confidence.medium",
	"legacy.group.one", "legacy.group.other", "legacy.also_in_v2",
	// render
	"report.title",
	"severity.red", "severity.yellow",
	"finding.part.what", "finding.part.why", "finding.part.todo", "finding.part.unknown", "finding.part.evidence",
	"evidence.edges",
	"group.family.one", "group.family.other", "group.outside_repos",
	"action.file_missing",
	"findings.none", "posture.none", "coverage.none", "legacy.none",
	// BV-39
	"section.assumptions", "console.summary", "console.report",
	// BV-43
	"section.memory", "memory.counts", "memory.not_comparable", "memory.moved", "memory.no_longer_observed.line",
	"memory.reason." + memory.ReasonRulesChanged, "memory.reason." + memory.ReasonScopeChanged,
	// BV-50
	"report.silenced.one", "report.silenced.other", "report.silenced.entry",
	"report.testdata.one", "report.testdata.other", "report.testdata.entry",
	"report.silenced.expired.one", "report.silenced.expired.other",
	"test_fixture." + rules.FixtureIgnoredScope, "test_fixture." + rules.FixtureFingerprintMatch, "memory.new_agent", "memory.new_mcp",
	"console.progress.agents", "console.progress.repos", "console.progress.report",
	"console.config_changed", "console.config_change.rotated", "console.config_change.fixture", "console.config_change.other",
	"console.config_confirm", "console.config_accepted", "console.config_rejected", "console.config_unconfirmed",
}

// CatalogKeys returns every code the v2 Markdown and the run's console
// summary can emit, sorted.
func CatalogKeys() []string {
	keys := append([]string{}, staticKeys...)
	for _, r := range rules.AllRules {
		keys = append(keys, "finding."+r+".title", "finding."+r+".what")
		if r == rules.RuleCredInGit {
			keys = append(keys, "finding."+r+".why.remote", "finding."+r+".why.local")
		} else {
			keys = append(keys, "finding."+r+".why")
		}
	}
	// The merged rows' texts (v3 §14.2).
	keys = append(keys, "finding.cred_read_and_send.title.multi", "finding.cred_read_and_send.what.multi",
		"finding.cred_read_and_send.why.multi", "finding.cred_readable.title.multi", "finding.cred_readable.what.multi")
	for _, a := range rules.AllActions {
		keys = append(keys, "action."+a+".label", "action."+a+".how")
	}
	for _, c := range rules.AllCompleteness {
		keys = append(keys, "completeness."+c)
	}
	for _, a := range rules.AllAssumptions {
		keys = append(keys, "unknown."+a)
	}
	for _, p := range rules.AllPostures {
		keys = append(keys, p+".one", p+".other")
	}
	sort.Strings(keys)
	return keys
}

// RenderV2Markdown renders the v2 report in the fixed order of §6 (E10):
// status and scope, today, findings, posture, memory, coverage, legacy. Every text
// comes from cat; paths, rules, fragments and ids are data.
func RenderV2Markdown(v V2, cat catalog.Catalog, loc *time.Location) string {
	if loc == nil {
		loc = time.Local
	}
	var sb strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&sb, format, a...) }
	t := func(key string, params map[string]string) string { return cat.Text(key, params) }

	at, err := time.Parse(time.RFC3339, v.GeneratedAt)
	if err != nil {
		at = time.Now()
	}
	local := at.In(loc)
	w("# %s\n\n", t("report.title", map[string]string{"date": local.Format("2006-01-02")}))

	// 1. Status and scope.
	gaps := len(v.Coverage.SourcesFailed) + len(v.Coverage.AgentsUnsupported) + len(v.Coverage.ScanErrors)
	if !v.Coverage.GraphBuilt {
		gaps++
	}
	w("%s %s\n\n", statusEmoji(v.Status), t("report.status."+v.Status, nil))
	scope := t("report.scope", map[string]string{
		"time":    local.Format("2006-01-02 15:04 MST"),
		"sources": strconv.Itoa(v.Coverage.SourcesRead),
		"repos":   strconv.Itoa(v.repos),
	})
	if gaps > 0 {
		scope += " " + t("report.coverage_gap", map[string]string{"count": strconv.Itoa(gaps)})
	}
	w("%s\n\n", scope)

	byID := map[string]rules.Finding{}
	for _, f := range v.Findings {
		byID[f.ID] = f
	}

	// 2. If you do only one thing today.
	w("## %s\n\n", t("section.today", nil))
	if f, ok := byID[v.Today.FindingID]; ok {
		p := findingParams(f)
		w("**%s**\n\n%s\n\n", t("finding."+f.Rule+".title", p), t("finding."+f.Rule+".what", p))
		for _, a := range f.Actions {
			if a.ID != v.Today.ActionID {
				continue
			}
			writeAction(&sb, cat, f, a, false)
			w("%s\n\n", t("today.why_this", actionParams(f, a)))
			if r := v.Today.Remains; r != nil {
				rp := actionParams(f, a)
				if r.Radius != "" {
					rp["radius"] = t("radius."+r.Radius, p)
				}
				w("%s\n\n", t("today.what_remains", rp))
				if r.Radius != "" {
					w("- %s\n", t("radius."+r.Radius, p))
				}
				w("\n<details><summary>%s</summary>\n\n", t("evidence.edges", p))
				for _, id := range r.Ways {
					w("- `%s`\n", id)
				}
				w("\n</details>\n\n")
			}
			w("%s\n\n", t("today.verify", actionParams(f, a)))
		}
	} else {
		w("%s\n\n", t(rules.TodayNothingUrgent, nil))
	}

	// What every finding assumes (BV-39): the common assumptions once, per
	// agent when there are several.
	writeSharedAssumptions(&sb, cat, v.Findings)

	// 3. Findings, in the order of §2; one per credential and rule in
	// several agents (v3 §12.4), with the agents joined.
	w("## %s\n\n", t("section.findings", nil))
	if len(v.Findings) == 0 {
		w("%s\n\n", t("findings.none", nil))
	}
	rows, done := credRows(v), map[*credRow]bool{}
	for _, f := range v.Findings {
		r := rows[f.ID]
		if done[r] {
			continue
		}
		done[r], f = true, r.rep
		p := findingParams(f)
		members := []rules.Finding{f}
		// A merged row (v3 §14.2): the .multi texts where they exist, and
		// each agent's actions and assumptions under its name.
		multi := func(key string) string { return key }
		if len(r.agents) > 1 {
			p["agent"] = listNames(cat, r.agents)
			members = members[:0]
			for _, id := range r.ids {
				members = append(members, byID[id])
			}
			multi = func(key string) string {
				if cat.Has(key + ".multi") {
					return key + ".multi"
				}
				return key
			}
		}
		w("### %s %s\n\n", severityEmoji(finding.Severity(f.Severity)), t(multi("finding."+f.Rule+".title"), p))
		meta := []string{t("severity."+f.Severity, p), t("confidence."+f.Confidence, p)}
		if f.Radius != "" {
			meta = append(meta, t("radius."+f.Radius, p))
		}
		w("%s\n\n", strings.Join(meta, " · "))
		w("#### %s\n\n%s\n\n", t("finding.part.what", p), t(multi("finding."+f.Rule+".what"), p))
		w("#### %s\n\n%s\n\n", t("finding.part.why", p), t(multi(whyKey(f)), p))
		head := func(m rules.Finding) { // the agent's name, in a merged row
			if len(members) > 1 {
				w("##### %s\n\n", m.Params.Agent)
			}
		}
		acts := 0
		for _, m := range members {
			acts += len(m.Actions)
		}
		if acts > 0 { // no empty «What to do» (v3 §14.5)
			w("#### %s\n\n", t("finding.part.todo", p))
			for _, m := range members {
				if len(m.Actions) > 0 {
					head(m)
				}
				for _, a := range m.Actions {
					writeAction(&sb, cat, m, a, true)
				}
			}
		}
		w("#### %s\n\n", t("finding.part.unknown", p))
		for _, m := range members {
			mp := findingParams(m)
			var own []string
			for _, a := range m.Evidence.Assumptions {
				if !sharedAssumption[a] {
					own = append(own, t("unknown."+a, mp))
				}
			}
			if len(own) > 0 {
				head(m)
				for _, l := range own {
					w("- %s\n", l)
				}
				if len(members) > 1 {
					w("\n")
				}
			}
		}
		w("\n#### %s\n\n", t("finding.part.evidence", p))
		for _, m := range f.Group.Members {
			if m.Family == "" {
				w("- %s\n", t("group.outside_repos", map[string]string{"count": strconv.Itoa(m.Count)}))
			} else {
				w("- %s\n", cat.Plural("group.family", m.Count, map[string]string{"family": path.Base(strings.ReplaceAll(m.Family, `\`, "/")), "count": strconv.Itoa(m.Count)}))
			}
			for _, item := range m.Items {
				w("  - `%s`\n", item)
			}
		}
		w("\n<details><summary>%s</summary>\n\n", t("evidence.edges", p))
		var edges []string
		for _, m := range members {
			edges = append(edges, m.Evidence.Edges...)
		}
		for _, e := range uniqueSorted(edges) {
			w("- `%s`\n", e)
		}
		w("\n</details>\n\n")
	}

	// 4. Posture.
	w("## %s\n\n", t("section.posture", nil))
	shown := false
	for _, p := range v.Posture {
		if p.Count == 0 || p.Code == rules.PostureInTemplatesOnly && listedAsTestData(p, v.TestData) {
			continue
		}
		shown = true
		params := map[string]string{"count": strconv.Itoa(p.Count)}
		for c, n := range p.ByClass {
			params[c] = strconv.Itoa(n)
		}
		w("- %s\n", cat.Plural(p.Code, p.Count, params))
	}
	if !shown {
		w("%s\n", t("posture.none", nil))
	}
	w("\n")

	// Keys treated as test data by default (v3 §12.4): always shown, with
	// their files.
	if td := v.TestData; td.Count > 0 {
		w("%s\n\n", cat.Plural("report.testdata", td.Count, map[string]string{"count": strconv.Itoa(td.Count)}))
		for _, f := range td.Files {
			w("- %s\n", t("report.testdata.entry", map[string]string{"repo": f.Repo, "path": f.Path}))
		}
		w("\n")
	}

	// Test data silenced by the user (M10–M11): always shown.
	if sl := v.Silenced; len(sl.Entries) > 0 {
		w("%s\n\n", cat.Plural("report.silenced", sl.Count, map[string]string{"count": strconv.Itoa(sl.Count)}))
		for _, e := range sl.Entries {
			w("- %s\n", t("report.silenced.entry", map[string]string{"repo": e.Repo, "path": e.Path, "note": e.Note, "expires_on": e.ExpiresOn}))
		}
		w("\n")
		if sl.Expired > 0 {
			w("%s\n\n", cat.Plural("report.silenced.expired", sl.Expired, map[string]string{"count": strconv.Itoa(sl.Expired)}))
		}
		for _, g := range sl.Guards {
			w("- %s\n", t("test_fixture."+g.Guard, map[string]string{"repo": g.Repo, "path": g.Path}))
		}
		if len(sl.Guards) > 0 {
			w("\n")
		}
	}

	// Memory (BV-43): only when there is an earlier memory to compare with,
	// or a new agent (M5).
	if !v.memoryPrev && len(v.NewAgents)+len(v.NewMCP) > 0 {
		w("## %s\n\n", t("section.memory", nil))
		for _, a := range v.NewAgents {
			w("%s\n\n", t("memory.new_agent", map[string]string{"agent": a}))
		}
		for _, s := range v.NewMCP { // v3 §13.3
			w("%s\n\n", t("memory.new_mcp", map[string]string{"name": s}))
		}
	}
	if v.memoryPrev {
		m := v.Memory
		w("## %s\n\n", t("section.memory", nil))
		for _, a := range v.NewAgents {
			w("%s\n\n", t("memory.new_agent", map[string]string{"agent": a}))
		}
		for _, s := range v.NewMCP { // v3 §13.3
			w("%s\n\n", t("memory.new_mcp", map[string]string{"name": s}))
		}
		w("%s\n", t("memory.counts", map[string]string{
			"new": strconv.Itoa(m.Counts[memory.VerdictNew]), "still": strconv.Itoa(m.Counts[memory.VerdictStillObserved]),
			"gone": strconv.Itoa(m.Counts[memory.VerdictNoLongerObserved]), "again": strconv.Itoa(m.Counts[memory.VerdictObservedAgain]),
		}))
		if m.Reason != "" {
			w("\n%s\n", t("memory.not_comparable", map[string]string{"reason": t("memory.reason."+m.Reason, nil)}))
		}
		for _, mv := range m.Moved {
			w("\n%s\n", t("memory.moved", map[string]string{"rule": mv.Rule}))
		}
		for _, g := range m.NoLongerObserved {
			w("\n%s\n", t("memory.no_longer_observed.line", map[string]string{
				"severity": t("severity."+g.Severity, nil), "rule": g.Rule, "open_since": g.OpenSince, "last_report": g.LastReport + ".md",
			}))
		}
		w("\n")
	}

	// 5. Coverage.
	w("## %s\n\n", t("section.coverage", nil))
	if gaps == 0 {
		w("%s\n", t("coverage.none", nil))
	}
	for _, f := range v.Coverage.SourcesFailed {
		w("- `%s` — `%s`\n", f.Path, f.Reason)
	}
	for _, a := range v.Coverage.AgentsUnsupported {
		w("- `%s` — `%s`\n", a.Platform, a.ConfigPath)
	}
	for _, s := range v.Coverage.ScanErrors {
		w("- `%s`\n", s)
	}
	if !v.Coverage.GraphBuilt {
		w("- `graph.v2.json`\n")
	}
	w("\n")

	// 6. Other checks (legacy).
	w("## %s\n\n", t("section.legacy", nil))
	if len(v.Legacy.Groups) == 0 {
		w("%s\n\n", t("legacy.none", nil))
	}
	for _, g := range v.Legacy.Groups {
		params := map[string]string{
			"category": g.Category, "detail": g.Detail, "count": strconv.Itoa(g.Count),
			"family": path.Base(strings.ReplaceAll(g.Family, `\`, "/")), "repos": strconv.Itoa(len(g.Repos)),
		}
		w("<details><summary>%s %s · <code>%s</code> · <code>%s</code></summary>\n\n",
			severityEmoji(finding.Severity(g.Severity)), cat.Plural("legacy.group", g.Count, params), g.Category, g.Detail)
		if g.AlsoInV2 {
			w("%s\n\n", t("legacy.also_in_v2", params))
		}
		for _, m := range g.members {
			w("- `%s`\n", m)
		}
		w("\n</details>\n\n")
	}
	return sb.String()
}

// sharedAssumptions are in nearly every edge and change nothing (§1, base
// group): shown once, before the findings, not in each one (BV-39).
var sharedAssumptions = []string{
	graph.AssumeProcessRunsAsUser, graph.AssumeVersionAsDocumented,
	graph.AssumeNoPreToolUseHookBlocks, graph.AssumeBashToolAvailable,
}

var sharedAssumption = func() map[string]bool {
	m := map[string]bool{}
	for _, a := range sharedAssumptions {
		m[a] = true
	}
	return m
}()

// writeSharedAssumptions renders section.assumptions: for each agent with
// findings, the shared assumptions its findings carry, one line each, with
// {agent} that agent; an agent subtitle when there are several agents.
func writeSharedAssumptions(sb *strings.Builder, cat catalog.Catalog, fs []rules.Finding) {
	var agents []string
	carried := map[string]map[string]bool{}
	for _, f := range fs {
		if rules.IsMachineFinding(f) {
			continue // no agent
		}
		a := f.Params.Agent
		if carried[a] == nil {
			carried[a] = map[string]bool{}
			agents = append(agents, a)
		}
		for _, x := range f.Evidence.Assumptions {
			carried[a][x] = true
		}
	}
	if len(agents) == 0 {
		return
	}
	fmt.Fprintf(sb, "## %s\n\n", cat.Text("section.assumptions", nil))
	for _, a := range agents {
		if len(agents) > 1 {
			fmt.Fprintf(sb, "### %s\n\n", a)
		}
		for _, x := range sharedAssumptions {
			if carried[a][x] {
				fmt.Fprintf(sb, "- %s\n", cat.Text("unknown."+x, map[string]string{"agent": a}))
			}
		}
		sb.WriteString("\n")
	}
}

// writeAction renders one action: label and completeness (in a finding's
// list), how, the file and the literal fragment, or the locations.
func writeAction(sb *strings.Builder, cat catalog.Catalog, f rules.Finding, a rules.Action, listed bool) {
	p := actionParams(f, a)
	if listed {
		fmt.Fprintf(sb, "**%s** · %s\n\n", cat.Text("action."+a.Code+".label", p), cat.Text("completeness."+a.Completeness, p))
	}
	fmt.Fprintf(sb, "%s\n\n", cat.Text("action."+a.Code+".how", p))
	if a.File != "" {
		fmt.Fprintf(sb, "`%s`\n\n", a.File)
		if !a.FileExists {
			fmt.Fprintf(sb, "%s\n\n", cat.Text("action.file_missing", p))
		}
	}
	if a.Value != "" {
		fmt.Fprintf(sb, "```json\n%s\n```\n\n", a.Value)
	}
	for _, l := range a.Locations {
		fmt.Fprintf(sb, "- `%s`\n", l)
	}
	if len(a.Locations) > 0 {
		sb.WriteString("\n")
	}
}

// findingParams are the catalog params of a finding.
func findingParams(f rules.Finding) map[string]string {
	p := map[string]string{
		"agent": f.Params.Agent, "service": f.Params.Service, "scope": f.Params.Scope,
		"class": f.Params.Class, "target_agent": f.Params.TargetAgent,
		"resource_type": f.Params.ResourceType, "count": strconv.Itoa(f.Params.Count),
	}
	p["service_name"], p["cred_label"] = serviceName(f.Params.Service), credLabel(f)
	p["cred_id"] = f.Params.CredID
	if len(f.Group.Members) > 0 && f.Group.Members[0].Family != "" {
		p["family"] = path.Base(strings.ReplaceAll(f.Group.Members[0].Family, `\`, "/"))
	}
	return p
}

// serviceNames are the display names of services (BV-38, literal table).
var serviceNames = map[string]string{
	"supabase": "Supabase", "openai": "OpenAI", "openrouter": "OpenRouter", "anthropic": "Anthropic",
	"github": "GitHub", "npm": "npm", "railway": "Railway", "stripe": "Stripe", "aws": "AWS", "slack": "Slack",
}

// serviceName is the display name of a service; any other, unknown
// included, is "the service it belongs to".
func serviceName(service string) string {
	if n, ok := serviceNames[service]; ok {
		return n
	}
	return "the service it belongs to"
}

// credLabel is the label of a finding's credential (BV-38): "database
// credential" for a db_credential, "secret" for an unknown service, else
// the service name, the scope when there is one, and "key".
func credLabel(f rules.Finding) string {
	switch {
	case f.CredType() == graph.CredentialDBCredential:
		return "database credential"
	case f.Params.Service == graph.ServiceUnknown:
		return "secret"
	case f.Params.Scope != "":
		return serviceName(f.Params.Service) + " " + f.Params.Scope + " key"
	}
	return serviceName(f.Params.Service) + " key"
}

// actionParams are the finding's params and the action's file and key.
func actionParams(f rules.Finding, a rules.Action) map[string]string {
	p := findingParams(f)
	p["file"], p["key"], p["magnitude"] = a.File, a.Key, strconv.Itoa(a.Magnitude)
	return p
}

// whyKey is a finding's why text: cred_in_git's says whether a repository
// declares a remote (§9.2).
// listedAsTestData reports a posture line every credential of which is in
// the report.testdata list (v3 §14.5, as amended in BV-58): the list then
// says it all. One credential missing, and the line stays as it was.
func listedAsTestData(p rules.Posture, td rules.TestData) bool {
	if td.Count == 0 || len(p.Creds()) == 0 {
		return false
	}
	listed := map[string]bool{}
	for _, c := range td.Creds {
		listed[c] = true
	}
	for _, c := range p.Creds() {
		if !listed[c] {
			return false
		}
	}
	return true
}

func whyKey(f rules.Finding) string {
	if f.Rule == rules.RuleCredInGit {
		if f.Params.HasRemote != nil && *f.Params.HasRemote {
			return "finding.cred_in_git.why.remote"
		}
		return "finding.cred_in_git.why.local"
	}
	return "finding." + f.Rule + ".why"
}
