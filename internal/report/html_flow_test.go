package report_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/report"
)

// flowChildren returns the tag (plus its class, as "tag.class") of each
// direct child element of the single `<div class="flow">`, in document
// order. It fails if the page does not have exactly one `.flow`.
//
// A tiny depth tracker, no HTML dependency: every `<` inside text is escaped,
// so each `<tag …>` opens one level and each `</tag>` closes one. Void tags
// (<br> …) and self-closing `<… />` tags do not nest; any <svg>/<template>
// subtree is stripped first, so their self-closing children never count. The
// `</div>` seen at depth 0 is the flow's own close.
func flowChildren(t *testing.T, page string) []string {
	t.Helper()
	const open = `<div class="flow">`
	if n := strings.Count(page, open); n != 1 {
		t.Fatalf("want exactly one <div class=\"flow\">, got %d", n)
	}
	rest := page[strings.Index(page, open)+len(open):]
	for _, sub := range []*regexp.Regexp{
		regexp.MustCompile(`(?s)<svg\b.*?</svg>`), regexp.MustCompile(`(?s)<template\b.*?</template>`),
	} {
		rest = sub.ReplaceAllString(rest, "")
	}
	void := map[string]bool{"br": true, "img": true, "input": true, "hr": true, "meta": true, "link": true, "source": true, "col": true, "area": true, "base": true, "wbr": true}
	tagRe := regexp.MustCompile(`</?([a-zA-Z0-9]+)([^>]*)>`)
	classRe := regexp.MustCompile(`class="([^"]*)"`)
	var kids []string
	depth := 0
	for _, m := range tagRe.FindAllStringSubmatchIndex(rest, -1) {
		full := rest[m[0]:m[1]]
		tag := rest[m[2]:m[3]]
		if strings.HasPrefix(full, "</") {
			if depth == 0 {
				break // closes the flow itself
			}
			depth--
			continue
		}
		if depth == 0 {
			desc := tag
			if cm := classRe.FindStringSubmatch(full); cm != nil {
				desc = tag + "." + cm[1]
			}
			kids = append(kids, desc)
		}
		if !void[tag] && !strings.HasSuffix(full, "/>") {
			depth++
		}
	}
	return kids
}

// TestRenderHTML_SingleFlowThreeColumns (BV-141): the three steps live in one
// `.flow` grid as direct children (s1, the step-2 section, the step-3
// section), so the three columns start at the same height. The today
// finding's jump anchor (span.fa) is inside the step-2 section, never a stray
// grid child. «Your space» is painted after the flow and before «How your
// agents work» (section.ov), at full width — never inside the flow. Checked
// with and without «Your space», in en and es.
func TestRenderHTML_SingleFlowThreeColumns(t *testing.T) {
	g := v2Graph(t) // yields a today finding, so span.fa is emitted
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	want := []string{"section.s1", "section.now", "section"}

	for _, lang := range []string{"en", "es"} {
		for _, withSpace := range []bool{false, true} {
			v.SpaceConnected = withSpace
			page := renderHTML(t, lang, v)

			got := flowChildren(t, page)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("%s (space=%v): flow direct children = %v, want %v", lang, withSpace, got, want)
			}
			// The anchor is nested, not a flow child.
			for _, k := range got {
				if strings.HasPrefix(k, "span") {
					t.Errorf("%s (space=%v): span %q is a direct child of .flow", lang, withSpace, k)
				}
			}
			if !strings.Contains(page, `<span class="fa"`) {
				t.Fatalf("%s: test is vacuous — no today anchor to place", lang)
			}

			// «Your space» placement: present only when connected, and then
			// after the flow and before section.ov, outside the flow.
			hasSpace := strings.Contains(page, `<section class="space"`)
			if hasSpace != withSpace {
				t.Errorf("%s (space=%v): section.space present=%v", lang, withSpace, hasSpace)
			}
			if withSpace {
				flowEnd := strings.Index(page, `<div class="flow">`)
				spacePos := strings.Index(page, `<section class="space"`)
				ovPos := strings.Index(page, `<section class="ov"`)
				if !(spacePos > flowEnd && (ovPos < 0 || spacePos < ovPos)) {
					t.Errorf("%s: section.space at %d is not after the flow (%d) and before section.ov (%d)", lang, spacePos, flowEnd, ovPos)
				}
			}
		}
	}
	v.SpaceConnected = false
}
