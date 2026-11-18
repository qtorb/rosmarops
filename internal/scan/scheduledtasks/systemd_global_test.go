package scheduledtasks

import (
	"os"
	"path/filepath"
	"testing"
)

// installGlobalWeekly writes the administrator's weekly review under
// <etc>/systemd/user and, when enabled, the link "systemctl --global enable"
// creates in timers.target.wants.
func installGlobalWeekly(t *testing.T, etc, exe string, enabled bool) string {
	t.Helper()
	dir := GlobalWeeklyUnitDir(etc)
	writeUnit(t, dir, "rosmarops-weekly.service", "[Service]\nType=oneshot\nExecStart="+exe+" run --unattended\n")
	writeUnit(t, dir, "rosmarops-weekly.timer", "[Timer]\nOnCalendar=Mon 08:15\nPersistent=true\n\n[Install]\nWantedBy=timers.target\n")
	if enabled {
		wants := filepath.Join(dir, "timers.target.wants")
		if err := os.MkdirAll(wants, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("../rosmarops-weekly.timer", filepath.Join(wants, "rosmarops-weekly.timer")); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestWeeklyFromSystemdDirs(t *testing.T) {
	const globalExe = "/usr/local/bin/rosmarops"
	t.Run("global enabled, this executable", func(t *testing.T) {
		g := installGlobalWeekly(t, t.TempDir(), globalExe, true)
		w := WeeklyFromSystemdDirs(filepath.Join(t.TempDir(), "none"), g, globalExe)
		if w != (Weekly{State: WeeklyOwn, Day: "monday", Time: "08:15"}) {
			t.Errorf("got %+v, want own monday 08:15", w)
		}
	})
	t.Run("global enabled, another copy", func(t *testing.T) {
		g := installGlobalWeekly(t, t.TempDir(), globalExe, true)
		w := WeeklyFromSystemdDirs(t.TempDir(), g, "/home/a/.local/bin/rosmarops")
		if w != (Weekly{State: WeeklyOther, Other: globalExe}) {
			t.Errorf("got %+v, want other %s", w, globalExe)
		}
	})
	t.Run("global not enabled", func(t *testing.T) {
		g := installGlobalWeekly(t, t.TempDir(), globalExe, false)
		if w := WeeklyFromSystemdDirs(t.TempDir(), g, globalExe); w.State != WeeklyNone {
			t.Errorf("got %+v, want none", w)
		}
		if GlobalWeeklyEnabled(g) {
			t.Error("a global unit without its wants link taken as enabled")
		}
	})
	t.Run("global without its timer", func(t *testing.T) {
		g := installGlobalWeekly(t, t.TempDir(), globalExe, true)
		if err := os.Remove(filepath.Join(g, "rosmarops-weekly.timer")); err != nil {
			t.Fatal(err)
		}
		if w := WeeklyFromSystemdDirs(t.TempDir(), g, globalExe); w.State != WeeklyNone {
			t.Errorf("got %+v, want none", w)
		}
	})
	t.Run("personal unit wins", func(t *testing.T) {
		g := installGlobalWeekly(t, t.TempDir(), globalExe, true)
		personal := t.TempDir()
		own := "/home/a/.local/bin/rosmarops"
		writeUnit(t, personal, "rosmarops-weekly.service", "[Service]\nExecStart="+own+" run --unattended\n")
		writeUnit(t, personal, "rosmarops-weekly.timer", "[Timer]\nOnCalendar=Fri 09:00\n")
		if w := WeeklyFromSystemdDirs(personal, g, globalExe); w != (Weekly{State: WeeklyOther, Other: own}) {
			t.Errorf("got %+v, want the personal unit (other %s)", w, own)
		}
		if w := WeeklyFromSystemdDirs(personal, g, own); w != (Weekly{State: WeeklyOwn, Day: "friday", Time: "09:00"}) {
			t.Errorf("got %+v, want the personal unit (own friday 09:00)", w)
		}
	})
	t.Run("no global dir", func(t *testing.T) {
		if w := WeeklyFromSystemdDirs(t.TempDir(), "", globalExe); w.State != WeeklyNone {
			t.Errorf("got %+v, want none", w)
		}
	})
}
