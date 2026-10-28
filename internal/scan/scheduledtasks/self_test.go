package scheduledtasks_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/scheduledtasks"
)

const selfExe = `C:\Tools\RosmarOps\rosmarops.exe`

var self = scheduledtasks.Self{Exe: selfExe, Domain: "DESKTOP-1", User: "ana", Langs: []string{"en", "es"}}

// ownTask is the task the §17.3 command creates for selfExe, in Spanish.
func ownTask() platform.ScheduledTask {
	return platform.ScheduledTask{Name: "RosmarOps weekly review", Path: `\`, SHA256: fakeSHA("own"), Date: recentDate, RosmarOps: &platform.RosmarOpsTask{
		Actions: 1, Execute: selfExe, ExecuteExpanded: selfExe, Arguments: "run --lang es --unattended",
		WorkingDirectory: `C:\Tools\RosmarOps`, UserID: `DESKTOP-1\ana`, LogonType: "Interactive", RunLevel: "Limited",
		Triggers: []platform.TaskTrigger{{Class: "MSFT_TaskWeeklyTrigger", DaysOfWeek: 32, Start: "2026-10-30T09:00:00"}},
	}}
}

// J6: the own task is recognized only when everything matches; any
// difference is another task.
func TestOwns(t *testing.T) {
	if !self.Owns(ownTask()) {
		t.Fatal("the command's own task is not recognized")
	}
	for name, change := range map[string]func(r *platform.RosmarOpsTask){
		"user without domain": func(r *platform.RosmarOpsTask) { r.UserID = "ANA" },
		"lang en":             func(r *platform.RosmarOpsTask) { r.Arguments = "run --lang en --unattended" },
	} {
		task := ownTask()
		change(task.RosmarOps)
		if !self.Owns(task) {
			t.Errorf("%s: not recognized", name)
		}
	}
	for name, change := range map[string]func(r *platform.RosmarOpsTask){
		"second action":   func(r *platform.RosmarOpsTask) { r.Actions = 2 },
		"other folder":    func(r *platform.RosmarOpsTask) { r.WorkingDirectory = `C:\Users\ana` },
		"no folder":       func(r *platform.RosmarOpsTask) { r.WorkingDirectory = "" },
		"other arguments": func(r *platform.RosmarOpsTask) { r.Arguments = "run --lang es --unattended --extra" },
		"not unattended":  func(r *platform.RosmarOpsTask) { r.Arguments = "run --lang es" },
		"unknown lang":    func(r *platform.RosmarOpsTask) { r.Arguments = "run --lang fr --unattended" },
		"SYSTEM":          func(r *platform.RosmarOpsTask) { r.UserID = "SYSTEM" },
		"other user":      func(r *platform.RosmarOpsTask) { r.UserID = `DESKTOP-1\eve` },
		"other domain":    func(r *platform.RosmarOpsTask) { r.UserID = `OTHER\ana` },
		"S4U":             func(r *platform.RosmarOpsTask) { r.LogonType = "S4U" },
		"Highest":         func(r *platform.RosmarOpsTask) { r.RunLevel = "Highest" },
		"variable path":   func(r *platform.RosmarOpsTask) { r.Execute = `%LOCALAPPDATA%\..\..\..\Tools\RosmarOps\rosmarops.exe` },
		"quoted path":     func(r *platform.RosmarOpsTask) { r.Execute = `"` + selfExe + `"` },
		"other case":      func(r *platform.RosmarOpsTask) { r.Execute = `c:\tools\rosmarops\rosmarops.exe` },
		"other exe":       func(r *platform.RosmarOpsTask) { r.Execute = `D:\old\rosmarops.exe` },
	} {
		task := ownTask()
		change(task.RosmarOps)
		if self.Owns(task) {
			t.Errorf("%s: taken as its own", name)
		}
	}
	if self.Owns(platform.ScheduledTask{Name: "x"}) {
		t.Error("a task without rosmarops.exe taken as its own")
	}
	if (scheduledtasks.Self{}).Owns(ownTask()) {
		t.Error("an unknown exe owns a task")
	}
	if (scheduledtasks.Self{Exe: selfExe, Langs: []string{"es"}}).Owns(ownTask()) {
		t.Error("an unknown user owns a task")
	}
}

// J5: column 2's states.
func TestWeeklyOf(t *testing.T) {
	other := ownTask()
	other.RosmarOps.Execute, other.RosmarOps.ExecuteExpanded = `%USERPROFILE%\old\rosmarops.exe`, `C:\Users\ana\old\rosmarops.exe`
	twoDays := ownTask()
	twoDays.RosmarOps.Triggers[0].DaysOfWeek = 32 | 2
	daily := ownTask()
	daily.RosmarOps.Triggers[0].Class = "MSFT_TaskDailyTrigger"
	sameExeElevated := ownTask()
	sameExeElevated.RosmarOps.RunLevel = "Highest"
	plain := platform.ScheduledTask{Name: "Backup", SHA256: fakeSHA("b")}
	for name, c := range map[string]struct {
		tasks []platform.ScheduledTask
		want  scheduledtasks.Weekly
	}{
		"nothing":           {nil, scheduledtasks.Weekly{State: "none"}},
		"other tasks":       {[]platform.ScheduledTask{plain}, scheduledtasks.Weekly{State: "none"}},
		"own":               {[]platform.ScheduledTask{plain, ownTask()}, scheduledtasks.Weekly{State: "own", Day: "friday", Time: "09:00"}},
		"own over other":    {[]platform.ScheduledTask{other, ownTask()}, scheduledtasks.Weekly{State: "own", Day: "friday", Time: "09:00"}},
		"other exe":         {[]platform.ScheduledTask{other}, scheduledtasks.Weekly{State: "other", Other: `C:\Users\ana\old\rosmarops.exe`}},
		"own on two days":   {[]platform.ScheduledTask{twoDays}, scheduledtasks.Weekly{State: "none"}},
		"own daily":         {[]platform.ScheduledTask{daily}, scheduledtasks.Weekly{State: "none"}},
		"this exe, not own": {[]platform.ScheduledTask{sameExeElevated}, scheduledtasks.Weekly{State: "none"}},
	} {
		if got := scheduledtasks.WeeklyOf(c.tasks, self); got != c.want {
			t.Errorf("%s: %+v, want %+v", name, got, c.want)
		}
	}
}

// J6: the own task gives no finding; the same exe's task altered so it no
// longer meets J6 gives own_task_changed (BV-93, B2). A non-fresh baseline is
// used so the run exercises the comparison, not the first-run path.
func TestScanScheduledTasks_OwnTaskSilent(t *testing.T) {
	if got := scheduledtasks.ScanScheduledTasks([]platform.ScheduledTask{ownTask()}, sentinel, nil, self, false).Findings; len(got) != 0 {
		t.Errorf("own task: %d findings", len(got))
	}
	elevated := ownTask()
	elevated.RosmarOps.RunLevel = "Highest"
	got := scheduledtasks.ScanScheduledTasks([]platform.ScheduledTask{elevated}, sentinel, nil, self, false).Findings
	if len(got) != 1 || got[0].Category != "own_task_changed" {
		t.Errorf("tampered own task: %+v", got)
	}
	// Even allow-listed by name, a tampered own task stays visible.
	allow := []config.AllowEntryID{{ID: "RosmarOps weekly review", Note: "mine"}}
	got = scheduledtasks.ScanScheduledTasks([]platform.ScheduledTask{elevated}, sentinel, allow, self, false).Findings
	if len(got) != 1 || got[0].Category != "own_task_changed" {
		t.Errorf("tampered own task (allow-listed): %+v", got)
	}
}
