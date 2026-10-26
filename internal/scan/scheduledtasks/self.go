package scheduledtasks

import (
	"strings"

	"github.com/qtorb/rosmarops/internal/platform"
)

// Self is this RosmarOps (decision 2026-10-26, integridad y revisión
// semanal, J4–J6): the absolute path of the running rosmarops.exe, the
// current user (domain and name) and the catalog's languages.
type Self struct {
	Exe          string
	Domain, User string
	Langs        []string
}

// dir is the folder of a Windows (or POSIX) path, without the separator.
func dir(p string) string {
	if i := strings.LastIndexAny(p, `\/`); i >= 0 {
		return p[:i]
	}
	return ""
}

// Owns reports this RosmarOps's own weekly review (J6), only when all hold:
// one action; its executable is this exe, as registered (no variables, the
// same case); its arguments are exactly "run --lang <code> --unattended" for
// a catalog language; its working directory is the exe's folder; its
// principal is the current user, logged on and not elevated. Any difference:
// not its own, and scheduled_task_new as before.
func (s Self) Owns(t platform.ScheduledTask) bool {
	r := t.RosmarOps
	if r == nil || s.Exe == "" || r.Actions != 1 || r.Execute != s.Exe || r.WorkingDirectory != dir(s.Exe) {
		return false
	}
	args := false
	for _, l := range s.Langs {
		args = args || r.Arguments == "run --lang "+l+" --unattended"
	}
	user := strings.EqualFold(r.UserID, s.User) || s.Domain != "" && strings.EqualFold(r.UserID, s.Domain+`\`+s.User)
	return args && s.User != "" && user && r.LogonType == "Interactive" && r.RunLevel == "Limited"
}

// Weekly states (informe HTML v3 §17.3).
const (
	WeeklyNone  = "none"  // no own task and none of another exe
	WeeklyOwn   = "own"   // this RosmarOps's own task, with its day and time
	WeeklyOther = "other" // a task of another rosmarops.exe
)

// Weekly is what column 2 says about the weekly review (J5).
type Weekly struct {
	State string
	Day   string // English, lowercase: friday
	Time  string // HH:MM
	Other string // the other task's executable, normalized
}

var dayNames = []string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"}

// when is the day and time of a task with one weekly trigger on one day;
// false otherwise.
func when(r *platform.RosmarOpsTask) (day, at string, ok bool) {
	if len(r.Triggers) != 1 || r.Triggers[0].Class != "MSFT_TaskWeeklyTrigger" {
		return "", "", false
	}
	tr := r.Triggers[0]
	for i, name := range dayNames {
		if tr.DaysOfWeek == 1<<i {
			day = name
		}
	}
	if day == "" || len(tr.Start) < 16 || tr.Start[10] != 'T' {
		return "", "", false
	}
	return day, tr.Start[11:16], true
}

// WeeklyOf is the weekly review's state among tasks: this RosmarOps's own
// (with one weekly trigger on one day, to say when), else one of another
// rosmarops.exe (its normalized executable differs from this exe), else
// none. A task of this exe that is not its own (other arguments, folder or
// principal) is none: the command updates it.
func WeeklyOf(tasks []platform.ScheduledTask, s Self) Weekly {
	var other string
	for _, t := range tasks {
		r := t.RosmarOps
		if r == nil {
			continue
		}
		if s.Owns(t) {
			if day, at, ok := when(r); ok {
				return Weekly{State: WeeklyOwn, Day: day, Time: at}
			}
			continue
		}
		if other == "" && !strings.EqualFold(r.ExecuteExpanded, s.Exe) {
			other = r.ExecuteExpanded
		}
	}
	if other != "" {
		return Weekly{State: WeeklyOther, Other: other}
	}
	return Weekly{State: WeeklyNone}
}
