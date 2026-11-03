package scheduledtasks

import (
	"os"
	"path/filepath"
	"strings"
)

// BV-165: the Linux weekly review is a systemd user timer. RosmarOps never
// writes these files or runs systemctl; it only reads the two unit files to
// report the weekly state (own, another copy, or none), the same states as on
// Windows. The agent or the person creates and enables the timer with the
// command the report gives.

// WeeklyUnitDir is the user systemd unit directory under a config base:
// <config>/systemd/user.
func WeeklyUnitDir(configBase string) string {
	return filepath.Join(configBase, "systemd", "user")
}

// SystemdUserAvailable reports whether this machine has user systemd at all:
// <config>/systemd exists, or the runtime dir /run/user/<uid>/systemd exists.
// When neither does, the weekly review is not available and the report says so
// (html.weekly.no_systemd).
func SystemdUserAvailable(configBase, runUserSystemdDir string) bool {
	return isDirPath(filepath.Join(configBase, "systemd")) || isDirPath(runUserSystemdDir)
}

func isDirPath(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// WeeklyFromSystemd reads rosmarops-weekly.service and .timer in unitDir and
// returns the weekly state for selfExe: WeeklyOwn (with its day and time) when
// the service runs this same executable, WeeklyOther (with the other path) when
// it runs a different one, WeeklyNone when there is no service file. It never
// executes anything.
func WeeklyFromSystemd(unitDir, selfExe string) Weekly {
	data, err := os.ReadFile(filepath.Join(unitDir, "rosmarops-weekly.service"))
	if err != nil {
		return Weekly{State: WeeklyNone}
	}
	exe := execStartExe(string(data))
	if exe == "" {
		return Weekly{State: WeeklyNone}
	}
	if selfExe != "" && filepath.Clean(exe) == filepath.Clean(selfExe) {
		day, at := timerWhen(filepath.Join(unitDir, "rosmarops-weekly.timer"))
		return Weekly{State: WeeklyOwn, Day: day, Time: at}
	}
	return Weekly{State: WeeklyOther, Other: exe}
}

// execStartExe is the executable of the first ExecStart= line: the first
// argument, with systemd double-quotes removed. "" when there is none.
func execStartExe(service string) string {
	for _, line := range strings.Split(service, "\n") {
		line = strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(line, "ExecStart=")
		if !ok {
			continue
		}
		rest = strings.TrimLeft(rest, " \t")
		if rest == "" {
			return ""
		}
		if rest[0] == '"' {
			if end := strings.IndexByte(rest[1:], '"'); end >= 0 {
				return systemdUnescape(rest[1 : 1+end])
			}
			return ""
		}
		if sp := strings.IndexAny(rest, " \t"); sp >= 0 {
			return rest[:sp]
		}
		return rest
	}
	return ""
}

// systemdUnescape reverses the backslash escaping used inside a double-quoted
// systemd argument for the two characters execStartExe's writer escapes.
func systemdUnescape(s string) string {
	return strings.NewReplacer(`\\`, `\`, `\"`, `"`).Replace(s)
}

// timerWhen is the English lowercase day and the HH:MM of an OnCalendar=Day
// HH:MM line (e.g. «Fri 09:00»); friday/09:00 when it cannot be parsed, the
// values the generated timer uses.
func timerWhen(timerPath string) (day, at string) {
	day, at = "friday", "09:00"
	data, err := os.ReadFile(timerPath)
	if err != nil {
		return day, at
	}
	for _, line := range strings.Split(string(data), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "OnCalendar=")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) >= 1 {
			if d, ok := systemdDay[strings.ToLower(fields[0])]; ok {
				day = d
			}
		}
		if len(fields) >= 2 && len(fields[1]) >= 5 {
			at = fields[1][:5]
		}
	}
	return day, at
}

var systemdDay = map[string]string{
	"sun": "sunday", "mon": "monday", "tue": "tuesday", "wed": "wednesday",
	"thu": "thursday", "fri": "friday", "sat": "saturday",
}
