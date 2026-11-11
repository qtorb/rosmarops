package scheduledtasks

import (
	"strconv"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan"
)

// weeklyLimitSlack is how much longer than the review's maximum global timeout
// the weekly task's own ExecutionTimeLimit must allow, to leave room for
// writing the report after the scan (BV-212). The task's command sets 25 min =
// scan.MaxGlobalTimeout (20 min, BV-155) + this.
const weeklyLimitSlack = 5 * time.Minute

// MinWeeklyExecutionLimit is the shortest ExecutionTimeLimit an own weekly task
// may have without the report offering to update it: the review's maximum
// (scan.MaxGlobalTimeout) plus the slack for writing the report.
func MinWeeklyExecutionLimit() time.Duration { return scan.MaxGlobalTimeout + weeklyLimitSlack }

// parseISO8601Duration parses the time part of an ISO-8601 duration as Task
// Scheduler writes ExecutionTimeLimit (e.g. "PT25M", "PT1H30M", "PT0S"); ok is
// false for an empty or unrecognized value. Only hours, minutes and seconds are
// used (the limit is well under a day).
func parseISO8601Duration(s string) (d time.Duration, ok bool) {
	s = strings.TrimSpace(s)
	if len(s) < 3 || s[0] != 'P' {
		return 0, false
	}
	t := s[1:]
	days := time.Duration(0)
	if i := strings.IndexByte(t, 'T'); i >= 0 {
		// A day part before the T (e.g. P1DT2H): add the days.
		if dpart := t[:i]; dpart != "" {
			n, unit, perr := splitNumUnit(dpart)
			if perr != nil || unit != 'D' {
				return 0, false
			}
			days = time.Duration(n) * 24 * time.Hour
		}
		t = t[i+1:]
	} else {
		return 0, false // no time part
	}
	total := days
	for t != "" {
		n, unit, perr := splitNumUnit(t)
		if perr != nil {
			return 0, false
		}
		switch unit {
		case 'H':
			total += time.Duration(n) * time.Hour
		case 'M':
			total += time.Duration(n) * time.Minute
		case 'S':
			total += time.Duration(n) * time.Second
		default:
			return 0, false
		}
		t = t[strings.IndexByte(t, unit)+1:]
	}
	return total, true
}

// splitNumUnit reads the leading integer of s and the unit letter that follows.
func splitNumUnit(s string) (n int, unit byte, err error) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || i >= len(s) {
		return 0, 0, strconv.ErrSyntax
	}
	n, err = strconv.Atoi(s[:i])
	return n, s[i], err
}

// adequateLimit is whether the task's ExecutionTimeLimit is long enough for the
// weekly review (BV-212): no limit ("PT0S"/empty/unknown) is adequate; a
// positive limit must be at least MinWeeklyExecutionLimit.
func adequateLimit(r *platform.RosmarOpsTask) bool {
	d, ok := parseISO8601Duration(r.ExecutionTimeLimit)
	if !ok || d <= 0 {
		return true
	}
	return d >= MinWeeklyExecutionLimit()
}

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
	return s.ownsIgnoringLimit(t) && adequateLimit(t.RosmarOps)
}

// ownsIgnoringLimit is Owns without the ExecutionTimeLimit check: the task runs
// this exe and meets every other J6 condition. Split out so WeeklyOf can tell
// an own task whose only fault is a too-short limit (BV-212) from one of
// another copy.
func (s Self) ownsIgnoringLimit(t platform.ScheduledTask) bool {
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

// ownWithShortLimit is an own weekly task (J6 in every other way) whose
// ExecutionTimeLimit is shorter than the review can need (BV-212): the report
// offers to update it, as it does for another copy's task.
func (s Self) ownWithShortLimit(t platform.ScheduledTask) bool {
	return s.ownsIgnoringLimit(t) && !adequateLimit(t.RosmarOps)
}

// ownButChanged reports a task that runs this same rosmarops.exe but no
// longer meets J6 (BV-93, B2): its own weekly task was altered (arguments,
// working directory, user, level or trigger). A task of another rosmarops.exe
// is WeeklyOther, not this; a task that still meets J6 is Owns.
func (s Self) ownButChanged(t platform.ScheduledTask) bool {
	r := t.RosmarOps
	if s.Exe == "" || r == nil {
		return false
	}
	thisExe := r.Execute == s.Exe || strings.EqualFold(r.ExecuteExpanded, s.Exe)
	return thisExe && !s.Owns(t)
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
	var other, ownShort string
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
		// BV-212: an own weekly task whose only fault is a too-short
		// ExecutionTimeLimit is treated like another copy's task — the report
		// offers to update it with the new command (which sets 25 minutes).
		if ownShort == "" && s.ownWithShortLimit(t) {
			ownShort = r.ExecuteExpanded
			continue
		}
		if other == "" && !strings.EqualFold(r.ExecuteExpanded, s.Exe) {
			other = r.ExecuteExpanded
		}
	}
	switch {
	case ownShort != "":
		return Weekly{State: WeeklyOther, Other: ownShort}
	case other != "":
		return Weekly{State: WeeklyOther, Other: other}
	default:
		return Weekly{State: WeeklyNone}
	}
}
