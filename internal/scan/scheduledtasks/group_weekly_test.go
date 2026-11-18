package scheduledtasks_test

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/scheduledtasks"
)

// companyTask is the weekly review a company deployment registers for the
// built-in Users group: another name, a group principal, no working folder,
// and "run --unattended".
func companyTask() platform.ScheduledTask {
	return platform.ScheduledTask{Name: "RosmarOps weekly review (company)", Path: `\`, SHA256: fakeSHA("company"), Date: recentDate, RosmarOps: &platform.RosmarOpsTask{
		Actions: 1, Execute: selfExe, ExecuteExpanded: selfExe, Arguments: "run --unattended",
		GroupID: "S-1-5-32-545", LogonType: "Group", RunLevel: "Limited",
		Triggers: []platform.TaskTrigger{{Class: "MSFT_TaskWeeklyTrigger", DaysOfWeek: 8, Start: "2026-10-28T13:30:00"}},
	}}
}

// A group task that runs this same exe is this computer's weekly review; one
// that runs another exe is another copy, as for a person's task.
func TestWeeklyOf_GroupTask(t *testing.T) {
	company := scheduledtasks.Weekly{State: scheduledtasks.WeeklyOwn, Day: "wednesday", Time: "13:30"}

	byName := companyTask()
	byName.RosmarOps.GroupID = "Users"
	withLang := companyTask()
	withLang.RosmarOps.Arguments = "run --lang es --unattended"
	otherExe := companyTask()
	otherExe.RosmarOps.Execute, otherExe.RosmarOps.ExecuteExpanded = `D:\old\rosmarops.exe`, `D:\old\rosmarops.exe`
	elevated := companyTask()
	elevated.RosmarOps.RunLevel = "Highest"
	notUnattended := companyTask()
	notUnattended.RosmarOps.Arguments = "run"
	daily := companyTask()
	daily.RosmarOps.Triggers[0].Class = "MSFT_TaskDailyTrigger"
	noGroup := companyTask()
	noGroup.RosmarOps.GroupID = ""

	for name, c := range map[string]struct {
		tasks []platform.ScheduledTask
		want  scheduledtasks.Weekly
	}{
		"company task":            {[]platform.ScheduledTask{companyTask()}, company},
		"group by name":           {[]platform.ScheduledTask{byName}, company},
		"with a catalog language": {[]platform.ScheduledTask{withLang}, company},
		"own wins over company":   {[]platform.ScheduledTask{companyTask(), ownTask()}, scheduledtasks.Weekly{State: "own", Day: "friday", Time: "09:00"}},
		"company over other copy": {[]platform.ScheduledTask{otherExe, companyTask()}, company},
		"group, other exe":        {[]platform.ScheduledTask{otherExe}, scheduledtasks.Weekly{State: "other", Other: `D:\old\rosmarops.exe`}},
		"group, elevated":         {[]platform.ScheduledTask{elevated}, scheduledtasks.Weekly{State: "none"}},
		"group, not unattended":   {[]platform.ScheduledTask{notUnattended}, scheduledtasks.Weekly{State: "none"}},
		"group, daily":            {[]platform.ScheduledTask{daily}, scheduledtasks.Weekly{State: "none"}},
		"no principal at all":     {[]platform.ScheduledTask{noGroup}, scheduledtasks.Weekly{State: "none"}},
	} {
		if got := scheduledtasks.WeeklyOf(c.tasks, self); got != c.want {
			t.Errorf("%s: %+v, want %+v", name, got, c.want)
		}
	}
}

// A user task with a group id as well is still judged as the person's own:
// the group path only applies to a task with no user principal.
func TestWeeklyOf_UserTaskUnchangedByGroup(t *testing.T) {
	task := ownTask()
	task.RosmarOps.GroupID = "S-1-5-32-545"
	want := scheduledtasks.Weekly{State: "own", Day: "friday", Time: "09:00"}
	if got := scheduledtasks.WeeklyOf([]platform.ScheduledTask{task}, self); got != want {
		t.Errorf("%+v, want %+v", got, want)
	}
	if self.Owns(companyTask()) {
		t.Error("a group task taken as the person's own task")
	}
}

// The company's group weekly review raises own_task_changed only when no
// trusted company configuration is in use; with one, it is expected, like the
// person's own task. Every other task is reported as before.
func TestScanScheduledTasks_CompanyTask(t *testing.T) {
	company := self
	company.Company = true
	scan := func(s scheduledtasks.Self, tasks ...platform.ScheduledTask) []string {
		var got []string
		for _, f := range scheduledtasks.ScanScheduledTasks(tasks, sentinel, nil, s, false).Findings {
			got = append(got, f.Category)
		}
		return got
	}

	if got := scan(company, companyTask()); len(got) != 0 {
		t.Errorf("company task, trusted company configuration: %v", got)
	}
	// Without a company configuration in use (none, or not trusted, which
	// the review never uses): reported as today.
	if got := scan(self, companyTask()); len(got) != 1 || got[0] != "own_task_changed" {
		t.Errorf("company task, no company configuration: %v", got)
	}
	// A group task that runs another executable is not the company's review
	// of this RosmarOps: reported as a new task, as today.
	otherExe := companyTask()
	otherExe.RosmarOps.Execute, otherExe.RosmarOps.ExecuteExpanded = `D:\old\rosmarops.exe`, `D:\old\rosmarops.exe`
	if got, want := scan(company, otherExe), scan(self, otherExe); len(got) != 1 || len(want) != 1 || got[0] != want[0] {
		t.Errorf("group task, other exe: %v with the company configuration, %v without", got, want)
	}
	// A group task of this exe that is not the company's review (elevated):
	// own_task_changed even with the company configuration.
	elevated := companyTask()
	elevated.RosmarOps.RunLevel = "Highest"
	if got := scan(company, elevated); len(got) != 1 || got[0] != "own_task_changed" {
		t.Errorf("elevated group task, trusted company configuration: %v", got)
	}
	// The person's own task altered: own_task_changed, with or without the
	// company configuration.
	changed := ownTask()
	changed.RosmarOps.RunLevel = "Highest"
	for _, s := range []scheduledtasks.Self{self, company} {
		if got := scan(s, changed); len(got) != 1 || got[0] != "own_task_changed" {
			t.Errorf("own task changed (company %v): %v", s.Company, got)
		}
	}
}
