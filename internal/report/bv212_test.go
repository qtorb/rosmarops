package report_test

import (
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan"
)

// TestBV212WeeklyCommandLimit (BV-212): the weekly task the report's command
// creates must allow at least the review's maximum global timeout plus 5
// minutes for writing the report, so Windows does not terminate it before the
// review finishes (as it did with the old 10-minute limit on a 48-project
// machine).
func TestBV212WeeklyCommandLimit(t *testing.T) {
	cmd := report.WeeklyCommand(`C:\Tools\RosmarOps\rosmarops.exe`, "en")
	m := regexp.MustCompile(`-ExecutionTimeLimit \(New-TimeSpan -Minutes (\d+)\)`).FindStringSubmatch(cmd)
	if m == nil {
		t.Fatalf("no -ExecutionTimeLimit (New-TimeSpan -Minutes N) in the command:\n%s", cmd)
	}
	mins, err := strconv.Atoi(m[1])
	if err != nil {
		t.Fatalf("minutes %q: %v", m[1], err)
	}
	got := time.Duration(mins) * time.Minute
	want := scan.MaxGlobalTimeout + 5*time.Minute
	if got < want {
		t.Fatalf("weekly command ExecutionTimeLimit is %v, must be at least the review cap + 5 min = %v", got, want)
	}
}
