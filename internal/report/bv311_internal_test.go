package report

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
)

// BV-311 (T2): the line after the projects not finished is set 12 px apart
// from the list of what was not read, when both are there.
func TestBV311GapAfterNotFinished(t *testing.T) {
	cat, _ := catalog.Load("en")
	v := V2{Coverage: V2Coverage{GraphBuilt: true, SourcesFailed: []graph.SourceFailed{{Path: "/x", Reason: "read_error: x"}}}}
	v.Legacy.Groups = timeoutGroups("/a")
	c, sb := coverageCtx(cat, v)
	c.renderCoverage()
	after := `<p class="cov-after">` + escapeHTML(cat.Text("html.coverage.timeout.after", nil)) + `</p><p>` + escapeHTML(cat.Text("html.coverage.body", nil)) + `</p>`
	if !strings.Contains(sb.String(), after) {
		t.Errorf("no line after the projects, then the list of what was not read: %s", sb.String())
	}
	if !strings.Contains(htmlCSS(), ".cov .cov-after+p{margin-top:12px}") {
		t.Error("no 12 px between the line after the projects and the list of what was not read")
	}
}
