package report_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/report"
)

// flowChildren returns the tag (plus its class, as "tag.class") of each
// direct child element of the single `<div id="todo">` (the problems'
// steps), in document order. It fails if the page does not have exactly one.
//
// A tiny depth tracker, no HTML dependency: every `<` inside text is escaped,
// so each `<tag …>` opens one level and each `</tag>` closes one. Void tags
// (<br> …) and self-closing `<… />` tags do not nest; any <svg>/<template>
// subtree is stripped first, so their self-closing children never count. The
// `</div>` seen at depth 0 is the container's own close.
func flowChildren(t *testing.T, page string) []string {
	t.Helper()
	const open = `<div id="todo">`
	if n := strings.Count(page, open); n != 1 {
		t.Fatalf("want exactly one <div id=\"todo\">, got %d", n)
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

// TestRenderHTML_SingleFlowThreeColumns: below the first screen, the
// problems' steps are one block (div.r3-steps) in `<div id="todo">`, every
// problem a section of it with its id, each finding's anchor inside its row,
// never a stray child. «Your space» is painted after them and before «How
// your agents work» (section.ov), at full width. Checked with and without
// «Your space», in en and es.
func TestRenderHTML_SingleFlowThreeColumns(t *testing.T) {
	g := v2Graph(t)
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	want := []string{"div.r3-steps"}

	for _, lang := range []string{"en", "es"} {
		for _, withSpace := range []bool{false, true} {
			v.SpaceConnected = withSpace
			page := renderHTML(t, lang, v)

			got := flowChildren(t, page)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("%s (space=%v): direct children = %v, want %v", lang, withSpace, got, want)
			}
			for _, f := range v.Findings {
				if !strings.Contains(page, `<div class="rb" id="`+f.ID+`">`) {
					t.Errorf("%s: finding %s has no anchor in its row", lang, f.ID)
				}
			}
			if !strings.Contains(page, `<section class="space"`) {
				t.Errorf("%s (space=%v): section.space missing", lang, withSpace)
			}
			todo := strings.Index(page, `<div id="todo">`)
			spacePos := strings.Index(page, `<section class="space"`)
			ovPos := strings.Index(page, `<section class="ov"`)
			if !(spacePos > todo && (ovPos < 0 || spacePos < ovPos)) {
				t.Errorf("%s: section.space at %d is not after the steps (%d) and before section.ov (%d)", lang, spacePos, todo, ovPos)
			}
		}
	}
	v.SpaceConnected = false
}
