package scheduledtasks_test

import (
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/scheduledtasks"
	"github.com/qtorb/rosmarops/internal/state"
)

// recent and old registration dates. Since BV-93 (B2) the date no longer
// filters anything — it is set by whoever creates the task — so both are
// reported; the dates are kept only to prove an old one is not hidden.
var (
	recentDate = time.Now().UTC().Add(-2 * 24 * time.Hour)
	oldDate    = time.Now().UTC().Add(-400 * 24 * time.Hour)
)

// sentinel is an unrelated baselined task, so a run is not "fresh" (empty
// baseline) and the new/changed detection is exercised.
var sentinel = []state.ScheduledTaskBase{{Name: "_sentinel_", SHA256: fakeSHA("sentinel")}}

func TestScanScheduledTasks_NoTasks_NoFindings(t *testing.T) {
	result := scheduledtasks.ScanScheduledTasks(nil, sentinel, nil, scheduledtasks.Self{}, false)
	if len(result.Findings) != 0 {
		t.Errorf("no tasks: want 0 findings, got %d", len(result.Findings))
	}
}

func TestScanScheduledTasks_NewTask_YellowFinding(t *testing.T) {
	tasks := []platform.ScheduledTask{
		{Name: "MyNewTask", Path: `\`, SHA256: fakeSHA("task1"), HasLogonTrigger: false, Date: recentDate},
	}
	result := scheduledtasks.ScanScheduledTasks(tasks, sentinel, nil, scheduledtasks.Self{}, false)
	if len(result.Findings) != 1 {
		t.Fatalf("new task: want 1 finding, got %d", len(result.Findings))
	}
	f := result.Findings[0]
	if f.Severity != "yellow" || f.Category != "scheduled_task_new" {
		t.Errorf("finding = %q/%q, want yellow/scheduled_task_new", f.Severity, f.Category)
	}
}

func TestScanScheduledTasks_NewTaskWithLogon_RedFinding(t *testing.T) {
	tasks := []platform.ScheduledTask{
		{Name: "EvilLogonTask", Path: `\`, SHA256: fakeSHA("evil"), HasLogonTrigger: true, Date: recentDate},
	}
	result := scheduledtasks.ScanScheduledTasks(tasks, sentinel, nil, scheduledtasks.Self{}, false)
	if len(result.Findings) != 1 {
		t.Fatalf("logon task: want 1 finding, got %d", len(result.Findings))
	}
	f := result.Findings[0]
	if f.Severity != "red" || f.Category != "scheduled_task_logon_trigger" {
		t.Errorf("finding = %q/%q, want red/scheduled_task_logon_trigger", f.Severity, f.Category)
	}
	if f.WhyItMatters == "" {
		t.Error("WhyItMatters must not be empty")
	}
}

func TestScanScheduledTasks_KnownTask_NoFinding(t *testing.T) {
	sha := fakeSHA("known")
	tasks := []platform.ScheduledTask{{Name: "KnownTask", Path: `\`, SHA256: sha, Date: recentDate}}
	baseline := []state.ScheduledTaskBase{{Name: "KnownTask", SHA256: sha}}
	result := scheduledtasks.ScanScheduledTasks(tasks, baseline, nil, scheduledtasks.Self{}, false)
	if len(result.Findings) != 0 {
		t.Errorf("known task: want 0 findings, got %d", len(result.Findings))
	}
}

func TestScanScheduledTasks_AllowListed_Suppressed(t *testing.T) {
	tasks := []platform.ScheduledTask{
		{Name: "AllowedTask", Path: `\`, SHA256: fakeSHA("al"), HasLogonTrigger: true, Date: recentDate},
	}
	allow := []config.AllowEntryID{{ID: "AllowedTask", Note: "known dev tool logon trigger"}}
	result := scheduledtasks.ScanScheduledTasks(tasks, sentinel, allow, scheduledtasks.Self{}, false)
	if len(result.Findings) != 0 {
		t.Errorf("allow-listed logon task: want 0 findings, got %d", len(result.Findings))
	}
	if result.AllowListedCount != 1 {
		t.Errorf("AllowListedCount = %d, want 1", result.AllowListedCount)
	}
	if want := (config.AllowHit{List: config.ListScheduledTasks, ID: "AllowedTask", Note: "known dev tool logon trigger"}); len(result.Hits) != 1 || result.Hits[0] != want { // B9
		t.Errorf("hits %+v", result.Hits)
	}
}

// B2/B8: an old registration date no longer hides a task.
func TestScanScheduledTasks_OldDate_StillReported(t *testing.T) {
	tasks := []platform.ScheduledTask{
		{Name: "OldButNew", Path: `\`, SHA256: fakeSHA("old"), Date: oldDate},
	}
	result := scheduledtasks.ScanScheduledTasks(tasks, sentinel, nil, scheduledtasks.Self{}, false)
	if len(result.Findings) != 1 {
		t.Errorf("old-dated new task: want 1 finding (no date filter), got %d", len(result.Findings))
	}
}

// B2: a known task whose arguments changed (so its hash changed) is reported.
func TestScanScheduledTasks_ModifiedTask_Finding(t *testing.T) {
	baseline := []state.ScheduledTaskBase{{Name: "ModTask", SHA256: fakeSHA("old-version")}}
	tasks := []platform.ScheduledTask{
		{Name: "ModTask", Path: `\`, SHA256: fakeSHA("new-version"), Date: recentDate},
	}
	result := scheduledtasks.ScanScheduledTasks(tasks, baseline, nil, scheduledtasks.Self{}, false)
	if len(result.Findings) != 1 {
		t.Fatalf("modified task: want 1 finding, got %d", len(result.Findings))
	}
}

// B2: the first run (empty baseline) records every task and warns about none.
func TestScanScheduledTasks_FirstRun_SilentBaseline(t *testing.T) {
	tasks := []platform.ScheduledTask{
		{Name: "TaskA", SHA256: fakeSHA("a"), Date: recentDate},
		{Name: "TaskB", SHA256: fakeSHA("b"), HasLogonTrigger: true, Date: oldDate},
	}
	result := scheduledtasks.ScanScheduledTasks(tasks, nil, nil, scheduledtasks.Self{}, false)
	if len(result.Findings) != 0 {
		t.Errorf("first run: want 0 findings, got %d", len(result.Findings))
	}
	if len(result.NewBaseline) != 2 {
		t.Errorf("NewBaseline len = %d, want 2", len(result.NewBaseline))
	}
}

// B2: the first run after the binary changed (fresh hash) records the baseline
// and does not warn, even though every task's hash differs from the old one.
func TestScanScheduledTasks_FreshHash_NoWarnings(t *testing.T) {
	baseline := []state.ScheduledTaskBase{
		{Name: "TaskA", SHA256: fakeSHA("a-old-hash")},
		{Name: "TaskB", SHA256: fakeSHA("b-old-hash")},
	}
	tasks := []platform.ScheduledTask{
		{Name: "TaskA", SHA256: fakeSHA("a-new-hash"), Date: recentDate},
		{Name: "TaskB", SHA256: fakeSHA("b-new-hash"), Date: recentDate},
	}
	result := scheduledtasks.ScanScheduledTasks(tasks, baseline, nil, scheduledtasks.Self{}, true)
	if len(result.Findings) != 0 {
		t.Errorf("fresh hash: want 0 findings, got %d", len(result.Findings))
	}
	if len(result.NewBaseline) != 2 {
		t.Errorf("NewBaseline len = %d, want 2", len(result.NewBaseline))
	}
}

func TestScanScheduledTasks_NewBaselineBuilt(t *testing.T) {
	tasks := []platform.ScheduledTask{
		{Name: "TaskA", SHA256: fakeSHA("a"), Date: recentDate},
		{Name: "TaskB", SHA256: fakeSHA("b"), Date: recentDate},
		{Name: "OldTask", SHA256: fakeSHA("old"), Date: oldDate},
	}
	result := scheduledtasks.ScanScheduledTasks(tasks, sentinel, nil, scheduledtasks.Self{}, false)
	// Every task is in the new baseline now, old date included.
	if len(result.NewBaseline) != 3 {
		t.Errorf("NewBaseline len = %d, want 3", len(result.NewBaseline))
	}
}

// fakeSHA builds a plausible 64-char lowercase hex SHA-256 from a seed string.
func fakeSHA(seed string) string {
	const hexChars = "abcdef0123456789"
	out := make([]byte, 64)
	for i := range out {
		out[i] = hexChars[(int(seed[i%len(seed)])+i)%len(hexChars)]
	}
	return string(out)
}
