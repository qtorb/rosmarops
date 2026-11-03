package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/qtorb/rosmarops/internal/scan/scheduledtasks"
)

// runtimeGOOS is runtime.GOOS; a variable so tests can set it.
var runtimeGOOS = runtime.GOOS

// linuxWeekly reads this user's systemd timer state for the weekly review
// (BV-165), never running systemctl. It returns the weekly state and whether
// user systemd is absent (then the report shows html.weekly.no_systemd). The
// config base is $XDG_CONFIG_HOME or ~/.config; availability also looks at
// /run/user/<uid>/systemd.
func linuxWeekly(exe string) (scheduledtasks.Weekly, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return scheduledtasks.Weekly{State: scheduledtasks.WeeklyNone}, false
	}
	configBase := os.Getenv("XDG_CONFIG_HOME")
	if configBase == "" {
		configBase = filepath.Join(home, ".config")
	}
	runUser := filepath.Join("/run/user", strconv.Itoa(os.Getuid()), "systemd")
	if !scheduledtasks.SystemdUserAvailable(configBase, runUser) {
		return scheduledtasks.Weekly{State: scheduledtasks.WeeklyNone}, true
	}
	return scheduledtasks.WeeklyFromSystemd(scheduledtasks.WeeklyUnitDir(configBase), exe), false
}
