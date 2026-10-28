package report

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/rules"
)

// B6 (decision 2026-10-27, blindaje): a source of the graph that could not
// be read leaves the status at least yellow; it never hides a red.
func TestReduceV2SourcesFailed(t *testing.T) {
	failed := []graph.SourceFailed{{Path: "/r/.mcp.json", Reason: graph.ReasonSizeLimitExceeded + ": 3000000 bytes > 2097152 (invariant 6)"}}
	red := []rules.Finding{{Severity: string(finding.SeverityRed)}}
	for _, c := range []struct {
		fs     []rules.Finding
		failed []graph.SourceFailed
		want   finding.Status
	}{
		{nil, nil, finding.StatusGreen},
		{nil, failed, finding.StatusYellow},
		{red, failed, finding.StatusRed},
		{red, nil, finding.StatusRed},
	} {
		if got := reduceV2(c.fs, c.failed); got != c.want {
			t.Errorf("findings %d, failed %d: %s, want %s", len(c.fs), len(c.failed), got, c.want)
		}
	}
}
