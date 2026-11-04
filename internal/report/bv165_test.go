package report_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/report"
)

// BV-165: the Linux weekly command creates the systemd user units for this
// binary's path and enables the timer; a path with a space is systemd-quoted.
func TestWeeklyCommandLinux(t *testing.T) {
	cmd := report.WeeklyCommandLinux("/home/a/.local/bin/rosmarops")
	for _, want := range []string{
		"mkdir -p ~/.config/systemd/user",
		"~/.config/systemd/user/rosmarops-weekly.service",
		"~/.config/systemd/user/rosmarops-weekly.timer",
		"ExecStart=/home/a/.local/bin/rosmarops run --unattended",
		"OnCalendar=Fri 09:00",
		"Persistent=true",
		"systemctl --user enable --now rosmarops-weekly.timer",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("command missing %q:\n%s", want, cmd)
		}
	}
	if q := report.WeeklyCommandLinux("/home/a/my apps/rosmarops"); !strings.Contains(q, `ExecStart="/home/a/my apps/rosmarops" run --unattended`) {
		t.Errorf("a path with a space is not systemd-quoted:\n%s", q)
	}
}
