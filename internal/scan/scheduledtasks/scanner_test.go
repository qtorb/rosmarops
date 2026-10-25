package scheduledtasks_test

import (
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/scheduledtasks"
	"github.com/qtorb/rosmarops/internal/state"
)

// recentDate is a task registration date within the SeenWithinDays window.
var recentDate = time.Now().UTC().Add(-2 * 24 * time.Hour) // 2 days ago

// oldDate is a task registration date outside the SeenWithinDays window.
var oldDate = time.Now().UTC().Add(-10 * 24 * time.Hour) // 10 days ago

func TestScanScheduledTasks_NoTasks_NoFindings(t *testing.T) {
	result := scheduledtasks.ScanScheduledTasks(nil, nil, nil)
	if len(result.Findings) != 0 {
		t.Errorf("no tasks: want 0 findings, got %d", len(result.Findings))
	}
}

func TestScanScheduledTasks_NewTask_YellowFinding(t *testing.T) {
	tasks := []platform.ScheduledTask{
		{Name: "MyNewTask", Path: `\`, SHA256: fakeSHA("task1"), HasLogonTrigger: false, Date: recentDate},
	}
	result := scheduledtasks.ScanScheduledTasks(tasks, nil, nil)

	if len(result.Findings) != 1 {
		t.Fatalf("new task: want 1 finding, got %d", len(result.Findings))
	}
	f := result.Findings[0]
	if f.Severity != "yellow" {
		t.Errorf("severity = %q, want yellow", f.Severity)
	}
	if f.Category != "scheduled_task_new" {
		t.Errorf("category = %q, want scheduled_task_new", f.Category)
	}
}

func TestScanScheduledTasks_NewTaskWithLogon_RedFinding(t *testing.T) {
	tasks := []platform.ScheduledTask{
		{Name: "EvilLogonTask", Path: `\`, SHA256: fakeSHA("evil"), HasLogonTrigger: true, Date: recentDate},
	}
	result := scheduledtasks.ScanScheduledTasks(tasks, nil, nil)

	if len(result.Findings) != 1 {
		t.Fatalf("logon task: want 1 finding, got %d", len(result.Findings))
	}
	f := result.Findings[0]
	if f.Severity != "red" {
		t.Errorf("severity = %q, want red", f.Severity)
	}
	if f.Category != "scheduled_task_logon_trigger" {
		t.Errorf("category = %q, want scheduled_task_logon_trigger", f.Category)
	}
	if f.WhyItMatters == "" {
		t.Error("WhyItMatters must not be empty")
	}
}

func TestScanScheduledTasks_KnownTask_NoFinding(t *testing.T) {
	sha := fakeSHA("known")
	tasks := []platform.ScheduledTask{
		{Name: "KnownTask", Path: `\`, SHA256: sha, HasLogonTrigger: false, Date: recentDate},
	}
	baseline := []state.ScheduledTaskBase{
		{Name: "KnownTask", SHA256: sha},
	}
	result := scheduledtasks.ScanScheduledTasks(tasks, baseline, nil)
	if len(result.Findings) != 0 {
		t.Errorf("known task: want 0 findings, got %d", len(result.Findings))
	}
}

func TestScanScheduledTasks_AllowListed_Suppressed(t *testing.T) {
	tasks := []platform.ScheduledTask{
		{Name: "AllowedTask", Path: `\`, SHA256: fakeSHA("al"), HasLogonTrigger: true, Date: recentDate},
	}
	allow := []config.AllowEntryID{
		{ID: "AllowedTask", Note: "known dev tool logon trigger"},
	}
	result := scheduledtasks.ScanScheduledTasks(tasks, nil, allow)
	if len(result.Findings) != 0 {
		t.Errorf("allow-listed logon task: want 0 findings, got %d", len(result.Findings))
	}
	if result.AllowListedCount != 1 {
		t.Errorf("AllowListedCount = %d, want 1", result.AllowListedCount)
	}
}

func TestScanScheduledTasks_OldTask_Ignored(t *testing.T) {
	tasks := []platform.ScheduledTask{
		{Name: "OldTask", Path: `\`, SHA256: fakeSHA("old"), HasLogonTrigger: false, Date: oldDate},
	}
	result := scheduledtasks.ScanScheduledTasks(tasks, nil, nil)
	if len(result.Findings) != 0 {
		t.Errorf("old task: want 0 findings (outside window), got %d", len(result.Findings))
	}
}

func TestScanScheduledTasks_ModifiedTask_Finding(t *testing.T) {
	baseline := []state.ScheduledTaskBase{
		{Name: "ModTask", SHA256: fakeSHA("old-version")},
	}
	tasks := []platform.ScheduledTask{
		{Name: "ModTask", Path: `\`, SHA256: fakeSHA("new-version"), HasLogonTrigger: false, Date: recentDate},
	}
	result := scheduledtasks.ScanScheduledTasks(tasks, baseline, nil)
	if len(result.Findings) != 1 {
		t.Fatalf("modified task: want 1 finding, got %d", len(result.Findings))
	}
}

func TestScanScheduledTasks_NewBaselineBuilt(t *testing.T) {
	tasks := []platform.ScheduledTask{
		{Name: "TaskA", SHA256: fakeSHA("a"), Date: recentDate},
		{Name: "TaskB", SHA256: fakeSHA("b"), Date: recentDate},
		{Name: "OldTask", SHA256: fakeSHA("old"), Date: oldDate}, // outside window
	}
	result := scheduledtasks.ScanScheduledTasks(tasks, nil, nil)
	// Old task should not appear in new baseline (outside window).
	if len(result.NewBaseline) != 2 {
		t.Errorf("NewBaseline len = %d, want 2 (old task excluded)", len(result.NewBaseline))
	}
}

func TestScanScheduledTasks_ZeroDate_IncludedConservatively(t *testing.T) {
	// Tasks with unknown date (zero) are included in the scan (conservative).
	tasks := []platform.ScheduledTask{
		{Name: "UnknownDateTask", SHA256: fakeSHA("unk"), Date: time.Time{}},
	}
	result := scheduledtasks.ScanScheduledTasks(tasks, nil, nil)
	if len(result.Findings) != 1 {
		t.Errorf("zero-date task: want 1 finding, got %d", len(result.Findings))
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
