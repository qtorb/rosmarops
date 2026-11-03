package scheduledtasks

import (
	"os"
	"path/filepath"
	"testing"
)

func writeUnit(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestWeeklyFromSystemd(t *testing.T) {
	self := "/home/a/.local/bin/rosmarops"
	t.Run("none when no service", func(t *testing.T) {
		if w := WeeklyFromSystemd(t.TempDir(), self); w.State != WeeklyNone {
			t.Errorf("state=%q want none", w.State)
		}
	})
	t.Run("own", func(t *testing.T) {
		dir := t.TempDir()
		writeUnit(t, dir, "rosmarops-weekly.service", "[Service]\nExecStart="+self+" run --unattended\n")
		writeUnit(t, dir, "rosmarops-weekly.timer", "[Timer]\nOnCalendar=Fri 09:00\nPersistent=true\n")
		w := WeeklyFromSystemd(dir, self)
		if w.State != WeeklyOwn || w.Day != "friday" || w.Time != "09:00" {
			t.Errorf("got %+v, want own friday 09:00", w)
		}
	})
	t.Run("other copy", func(t *testing.T) {
		dir := t.TempDir()
		writeUnit(t, dir, "rosmarops-weekly.service", "[Service]\nExecStart=/opt/other/rosmarops run --unattended\n")
		w := WeeklyFromSystemd(dir, self)
		if w.State != WeeklyOther || w.Other != "/opt/other/rosmarops" {
			t.Errorf("got %+v, want other /opt/other/rosmarops", w)
		}
	})
	t.Run("quoted path with a space", func(t *testing.T) {
		dir := t.TempDir()
		exe := "/home/a/my apps/rosmarops"
		writeUnit(t, dir, "rosmarops-weekly.service", "[Service]\nExecStart=\""+exe+"\" run --unattended\n")
		w := WeeklyFromSystemd(dir, exe)
		if w.State != WeeklyOwn {
			t.Errorf("got %+v, want own for a quoted path", w)
		}
	})
}

func TestSystemdUserAvailable(t *testing.T) {
	base := t.TempDir()
	if SystemdUserAvailable(base, filepath.Join(base, "nope")) {
		t.Error("should be unavailable with no systemd dir")
	}
	if err := os.MkdirAll(filepath.Join(base, "systemd"), 0o700); err != nil {
		t.Fatal(err)
	}
	if !SystemdUserAvailable(base, filepath.Join(base, "nope")) {
		t.Error("should be available once <config>/systemd exists")
	}
}
