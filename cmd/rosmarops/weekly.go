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

// systemEtc is the root of the machine-wide configuration where an
// administrator installs the weekly review for every user
// (<systemEtc>/systemd/user); a variable so tests can point it at a temporary
// folder.
var systemEtc = "/etc"

// linuxWeekly reads this user's systemd timer state for the weekly review,
// never running systemctl or any other process. It returns the weekly state
// and whether user systemd is absent (then the report shows
// html.weekly.no_systemd). The config base is $XDG_CONFIG_HOME or ~/.config;
// availability also looks at /run/user/<uid>/systemd. Besides the person's own
// unit it reads the one an administrator installs for every user under
// <systemEtc>/systemd/user and enables with "systemctl --global enable"; the
// person's own unit of the same name wins, as it does in systemd.
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
	globalDir := scheduledtasks.GlobalWeeklyUnitDir(systemEtc)
	// An enabled global unit also shows that user systemd is in use, even
	// when the person has no systemd folder of their own.
	if !scheduledtasks.SystemdUserAvailable(configBase, runUser) && !scheduledtasks.GlobalWeeklyEnabled(globalDir) {
		return scheduledtasks.Weekly{State: scheduledtasks.WeeklyNone}, true
	}
	return scheduledtasks.WeeklyFromSystemdDirs(scheduledtasks.WeeklyUnitDir(configBase), globalDir, exe), false
}
