package report

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/rules"
)

// AIMaxBytes is the hard cap of the export for your AI (informe HTML v2,
// §6): past it, the first findings go in full up to the cap and the rest as
// one line per title, with ai.more.
const AIMaxBytes = 48 * 1024

// AIFileName is the export's file name next to the run's report.
func AIFileName(base string) string { return base + ".ai.md" }

// RenderAIExport renders the export for your AI (§6), in the catalog's
// language, from the html.* texts the HTML shows for each finding. It
// carries no value, preview, fingerprint, host id or credential id, and
// every path in the user's folder starts with ~.
func RenderAIExport(v V2, cat catalog.Catalog, home string) string {
	c := htmlCtx{v: v, cat: cat, home: home, score: map[string]*index.AgentIndex{}}
	for i := range v.Index.Agents {
		c.score[v.Index.Agents[i].AgentID] = &v.Index.Agents[i]
	}

	var head strings.Builder
	fmt.Fprintf(&head, "%s\n\n%s\n\n%s\n\n", cat.Text("ai.instruction", nil), cat.Text("ai.no_secrets", nil), cat.Text("ai.what", nil))
	for _, a := range v.Index.Agents {
		if a.Value == nil {
			continue
		}
		counts := cat.Text("html.card.counts", map[string]string{"red": strconv.Itoa(a.Red), "yellow": strconv.Itoa(a.Yellow)})
		fmt.Fprintf(&head, "%s · %s · %d/100 (%s) · %s\n\n", cat.Text("html.index.name", nil), a.Agent, *a.Value, cat.Text("band."+a.Band, nil), counts)
	}
	if len(v.Index.Scope.Evaluated) > 1 { // the step 1 counts, across agents (v3 §15.9)
		red, yellow := RowCounts(v)
		fmt.Fprintf(&head, "%s\n\n", cat.Text("ai.counts.total", map[string]string{"red": strconv.Itoa(red), "yellow": strconv.Itoa(yellow)}))
	}
	today := false // «If you do only one thing today» is written
	for _, f := range v.Findings {
		if f.ID != v.Today.FindingID {
			continue
		}
		for _, a := range f.Actions {
			if a.ID != v.Today.ActionID {
				continue
			}
			today = true
			p := c.params(f)
			fmt.Fprintf(&head, "## %s\n\n### %s\n\n%s", cat.Text("ai.today", nil), c.titleText(f), c.impactText(f))
			if f.Rule == rules.RuleCredReadAndSend && f.Radius == rules.RadiusAnyProject {
				fmt.Fprintf(&head, " %s", cat.Text("html.today.reach", p))
			}
			head.WriteString("\n\n" + c.aiAction(f, a))
		}
	}

	var tail strings.Builder
	// What we don't know: a paragraph per evaluated agent, in the score's
	// order, with the HTML's key for it (v3 §14.4).
	// An evaluated agent with no score (coverage) goes after, by name.
	var order []string
	seen := map[string]bool{}
	for _, a := range c.scoredAgents() {
		order, seen[a.Agent] = append(order, a.Agent), true
	}
	for _, a := range v.Index.Scope.Evaluated {
		if !seen[a] {
			order = append(order, a)
		}
	}
	for _, a := range order {
		fmt.Fprintf(&tail, "%s\n\n", cat.Text(unknownBody(a), map[string]string{"agent": a}))
	}
	// BV-91 (D5): the new and changed MCP servers, with the host for remote
	// servers. Never the URL path, query, arguments or keys.
	for _, s := range v.MCPChanged {
		fmt.Fprintf(&tail, "%s\n\n", cat.Text("memory.mcp_changed", mcpParams(cat, s)))
	}
	for _, s := range v.NewMCP {
		fmt.Fprintf(&tail, "%s\n\n", cat.Text("memory.new_mcp", mcpParams(cat, s)))
	}
	tail.WriteString(cat.Text("ai.limits", nil) + "\n")

	full := make([]string, len(v.Findings))
	line := make([]string, len(v.Findings))
	for i, f := range v.Findings {
		var b strings.Builder
		switch {
		case f.ID == v.Today.FindingID && today:
			// Already above, in «If you do only one thing today» (v3 §15.9).
			fmt.Fprintf(&b, "### %s\n\n%s\n\n", c.titleText(f), cat.Text("ai.same_as_today", nil))
		default:
			fmt.Fprintf(&b, "### %s\n\n%s\n\n", c.titleText(f), c.impactText(f))
			if a, ok := rules.BestAction(f, v.Findings); ok {
				b.WriteString(c.aiAction(f, a))
			}
		}
		for _, l := range f.Evidence.Locations {
			fmt.Fprintf(&b, "- `%s`\n", userPath(l, home, "~"))
		}
		if len(f.Evidence.Locations) > 0 {
			b.WriteString("\n")
		}
		full[i] = b.String()
		line[i] = "- " + c.titleText(f) + "\n"
	}

	// The cut (§6): the most findings in full that fit, the rest by title;
	// deterministic, the same input gives the same cut.
	section := ""
	if len(v.Findings) > 0 {
		section = "## " + cat.Text("ai.findings", nil) + "\n\n"
	}
	build := func(k, lines int) string {
		var b strings.Builder
		b.WriteString(head.String() + section)
		for _, s := range full[:k] {
			b.WriteString(s)
		}
		if rest := len(full) - k; rest > 0 {
			for _, s := range line[k : k+lines] {
				b.WriteString(s)
			}
			fmt.Fprintf(&b, "\n%s\n\n", cat.Text("ai.more", map[string]string{"count": strconv.Itoa(rest)}))
		}
		b.WriteString(tail.String())
		return b.String()
	}
	for k := len(full); k > 0; k-- {
		if out := build(k, len(full)-k); len(out) <= AIMaxBytes {
			return out
		}
	}
	// Not even every title fits: the titles that do, in order.
	for n := len(full); n > 0; n-- {
		if out := build(0, n); len(out) <= AIMaxBytes {
			return out
		}
	}
	return build(0, 0)
}

// aiAction is a finding's action as its card shows it: the text the copy
// button copies, with ~ for the user's folder, and the value in a json
// block; or html.fix.remove and the locations. Then what remains.
func (c htmlCtx) aiAction(f rules.Finding, a rules.Action) string {
	p := c.params(f)
	var b strings.Builder
	after := "html.after.full"
	if a.Code == rules.ActionRemoveFromDisk {
		fmt.Fprintf(&b, "%s\n\n", c.cat.Text("html.fix.remove", p))
		for _, l := range a.Locations {
			fmt.Fprintf(&b, "- `%s`\n", userPath(l, c.home, "~"))
		}
		b.WriteString("\n")
	} else {
		prompt := "html.agent.prompt.merge"
		if !a.FileExists {
			prompt = "html.agent.prompt.create"
		}
		if a.Code == rules.ActionRotateCredential {
			prompt = "html.agent.prompt.rotated" // as the HTML's button (v3 §14.3)
		}
		pp := copyParams(p)
		pp["file"] = userPath(a.File, c.home, "~")
		fmt.Fprintf(&b, "%s\n\n```json\n%s\n```\n\n", c.cat.Text(prompt, pp), homeInRules(a.Value, c.home))
		after = map[string]string{
			rules.CompletenessFull: "html.after.full", rules.CompletenessRadiusOnly: "html.after.radius", rules.CompletenessPartial: "html.after.partial",
		}[a.Completeness]
		if a.Code == rules.ActionRotateCredential {
			after = "html.after.rotate" // v3 §13.2
		}
	}
	fmt.Fprintf(&b, "%s\n\n", c.cat.Text(after, p))
	return b.String()
}

// homeInRules replaces the user's folder in the permission rules of a value
// ("//c/Users/name/…", the form of the rules layer) with "~/", the home
// form Claude Code reads the same, so that no path names the user.
func homeInRules(value, home string) string {
	if home == "" {
		return value
	}
	p := strings.TrimRight(strings.ReplaceAll(home, `\`, "/"), "/")
	if len(p) >= 2 && p[1] == ':' {
		p = "/" + strings.ToLower(p[:1]) + p[2:]
	}
	return strings.ReplaceAll(value, "/"+p+"/", "~/")
}

// WriteAIExport writes the export to dir as base.ai.md (§6), in the
// reports directory.
func WriteAIExport(v V2, cat catalog.Catalog, home, dir, base string) (string, error) {
	path := filepath.Join(dir, AIFileName(base))
	if err := os.WriteFile(path, []byte(RenderAIExport(v, cat, home)), 0o600); err != nil {
		return "", fmt.Errorf("write ai export: %w", err)
	}
	return path, nil
}
