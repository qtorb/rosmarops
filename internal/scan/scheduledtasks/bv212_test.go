package scheduledtasks_test

import (
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan"
	"github.com/qtorb/rosmarops/internal/scan/scheduledtasks"
)

// TestBV212WeeklyLimit: an own weekly task whose ExecutionTimeLimit is shorter
// than the review can need (the review cap plus 5 minutes) is no longer taken
// as a settled own task; the report offers to update it, as for another copy.
// No limit, or one at least that long, stays its own.
func TestBV212WeeklyLimit(t *testing.T) {
	if got, want := scheduledtasks.MinWeeklyExecutionLimit(), scan.MaxGlobalTimeout+5*time.Minute; got != want {
		t.Fatalf("MinWeeklyExecutionLimit = %v, want review cap + 5 min = %v", got, want)
	}

	// Short: below the cap (20 min) + 5 min = 25 min.
	for _, lim := range []string{"PT10M", "PT24M", "PT24M59S", "PT1439S"} {
		task := ownTask()
		task.RosmarOps.ExecutionTimeLimit = lim
		if self.Owns(task) {
			t.Errorf("limit %q: too short, must not be taken as a settled own task", lim)
		}
		w := scheduledtasks.WeeklyOf([]platform.ScheduledTask{task}, self)
		if w.State != scheduledtasks.WeeklyOther || w.Other != selfExe {
			t.Errorf("limit %q: want WeeklyOther offering the update (Other=%q), got %+v", lim, selfExe, w)
		}
	}

	// Adequate: at least 25 min, or no limit ("PT0S"/empty), or unparseable.
	for _, lim := range []string{"PT25M", "PT26M", "PT1H", "PT1H30M", "PT0S", "", "garbage"} {
		task := ownTask()
		task.RosmarOps.ExecutionTimeLimit = lim
		if !self.Owns(task) {
			t.Errorf("limit %q: adequate, must stay its own", lim)
		}
		w := scheduledtasks.WeeklyOf([]platform.ScheduledTask{task}, self)
		if w.State != scheduledtasks.WeeklyOwn {
			t.Errorf("limit %q: want WeeklyOwn, got %+v", lim, w)
		}
	}

	// A short-limit own task and a settled own task together: the settled one
	// wins (no needless update offer).
	short := ownTask()
	short.RosmarOps.ExecutionTimeLimit = "PT10M"
	good := ownTask()
	good.RosmarOps.ExecutionTimeLimit = "PT25M"
	if w := scheduledtasks.WeeklyOf([]platform.ScheduledTask{short, good}, self); w.State != scheduledtasks.WeeklyOwn {
		t.Errorf("a settled own task present: want WeeklyOwn, got %+v", w)
	}
}
