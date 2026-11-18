package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qtorb/rosmarops/internal/scan/scheduledtasks"
)

// writeWeeklyUnits writes rosmarops-weekly.service and .timer in dir, the
// service running exe with "run --unattended".
func writeWeeklyUnits(t *testing.T, dir, exe, calendar string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"rosmarops-weekly.service": "[Service]\nType=oneshot\nExecStart=" + exe + " run --unattended\n",
		"rosmarops-weekly.timer":   "[Timer]\nOnCalendar=" + calendar + "\nPersistent=true\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// linuxWeekly reads the administrator's global unit under <systemEtc>, only
// when it is enabled, and the person's own unit wins over it. Nothing is run:
// only files and the wants link are read.
func TestLinuxWeekly_GlobalUnit(t *testing.T) {
	const globalExe = "/usr/local/bin/rosmarops"
	setup := func(t *testing.T, enabled bool) (configBase string) {
		t.Helper()
		home := t.TempDir()
		t.Setenv("HOME", home)
		configBase = filepath.Join(home, ".config")
		t.Setenv("XDG_CONFIG_HOME", configBase)
		etc := t.TempDir()
		old := systemEtc
		systemEtc = etc
		t.Cleanup(func() { systemEtc = old })
		global := filepath.Join(etc, "systemd", "user")
		writeWeeklyUnits(t, global, globalExe, "Mon 08:15")
		if enabled {
			wants := filepath.Join(global, "timers.target.wants")
			if err := os.MkdirAll(wants, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("../rosmarops-weekly.timer", filepath.Join(wants, "rosmarops-weekly.timer")); err != nil {
				t.Fatal(err)
			}
		}
		return configBase
	}

	t.Run("enabled global, no personal unit", func(t *testing.T) {
		setup(t, true)
		w, noSystemd := linuxWeekly(globalExe)
		want := scheduledtasks.Weekly{State: scheduledtasks.WeeklyOwn, Day: "monday", Time: "08:15"}
		if w != want || noSystemd {
			t.Errorf("got %+v noSystemd=%v, want %+v and systemd present", w, noSystemd, want)
		}
	})
	t.Run("global without the wants link", func(t *testing.T) {
		setup(t, false)
		if w, _ := linuxWeekly(globalExe); w.State != scheduledtasks.WeeklyNone {
			t.Errorf("got %+v, want none (not enabled)", w)
		}
	})
	t.Run("personal unit wins", func(t *testing.T) {
		configBase := setup(t, true)
		own := "/home/a/.local/bin/rosmarops"
		writeWeeklyUnits(t, scheduledtasks.WeeklyUnitDir(configBase), own, "Fri 09:00")
		w, _ := linuxWeekly(own)
		want := scheduledtasks.Weekly{State: scheduledtasks.WeeklyOwn, Day: "friday", Time: "09:00"}
		if w != want {
			t.Errorf("got %+v, want the personal unit %+v", w, want)
		}
		if w, _ := linuxWeekly(globalExe); w.State != scheduledtasks.WeeklyOther || w.Other != own {
			t.Errorf("got %+v, want the personal unit as another copy", w)
		}
	})
}
