package report_test

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// informe HTML v2 §6: past 48 KB the first findings go in full and the rest
// as one line per title, with ai.more and the right count; the cut is
// deterministic.
func TestRenderAIExport_Cut(t *testing.T) {
	var entries []platform.CredentialEntry
	for i := 0; i < 400; i++ {
		entries = append(entries, platform.CredentialEntry{
			Path: fmt.Sprintf("/src/a/service-%03d/.env", i), Line: 1, Key: "OPENAI_API_KEY",
			Value: secrets.NewSecretValue("sk-proj-" + strings.Repeat(string(rune('A'+i%26)), 20) + fmt.Sprintf("%028d", i)),
		})
	}
	g := v2GraphWith(t, entries, nil)
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		out := report.RenderAIExport(v, cat, "/h")
		if len(out) > report.AIMaxBytes {
			t.Errorf("%s: %d bytes, over the cap", lang, len(out))
		}
		if out != report.RenderAIExport(v, cat, "/h") {
			t.Errorf("%s: the cut is not deterministic", lang)
		}
		// Findings in full are "### " headings after the findings section;
		// the rest are "- " title lines before ai.more.
		_, findings, ok := strings.Cut(out, "## "+cat.Text("ai.findings", nil)+"\n")
		if !ok {
			t.Fatalf("%s: no findings section", lang)
		}
		full := strings.Count(findings, "\n### ")
		rest := len(v.Findings) - full
		if rest <= 0 {
			t.Fatalf("%s: %d findings, all in full — test would be vacuous", lang, len(v.Findings))
		}
		more := cat.Text("ai.more", map[string]string{"count": strconv.Itoa(rest)})
		if !strings.Contains(out, "\n"+more+"\n") {
			t.Errorf("%s: no %q (%d findings, %d in full)", lang, more, len(v.Findings), full)
		}
		if n := len(regexp.MustCompile(`(?m)^- [^`+"`"+`].*$`).FindAllString(findings, -1)); n != rest {
			t.Errorf("%s: %d title lines, want %d", lang, n, rest)
		}
	}
}

// A small report: every finding in full, no ai.more; the parts in the order
// of §6.
func TestRenderAIExport_Order(t *testing.T) {
	g := v2Graph(t)
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		out := report.RenderAIExport(v, cat, "/h")
		last := -1
		for _, part := range []string{
			cat.Text("ai.instruction", nil), cat.Text("ai.what", nil), cat.Text("html.index.name", nil) + " · Claude Code · ",
			"## " + cat.Text("ai.today", nil), "## " + cat.Text("ai.findings", nil),
			cat.Text("html.unknown.body", map[string]string{"agent": "Claude Code"}), cat.Text("ai.limits", nil),
		} {
			i := strings.Index(out, part)
			if i <= last {
				t.Errorf("%s: %q at %d, after %d", lang, part, i, last)
			}
			last = i
		}
		if strings.Contains(out, strings.SplitN(cat.Text("ai.more", nil), "{", 2)[1]) {
			t.Errorf("%s: ai.more in a small export", lang)
		}
		if strings.Count(out, "```json\n") == 0 || !strings.Contains(out, "~/.claude/settings.json") {
			t.Errorf("%s: no action with its file under ~", lang)
		}
	}
}
