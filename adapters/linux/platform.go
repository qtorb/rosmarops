// Package linux provides the Linux implementation of platform.Platform
// (BV-165: RosmarOps reads on Linux what it reads on Windows and macOS). The
// agents' files are read by the same portable readers as on Windows
// (adapters/windows), on the Linux folder layout (agentdirs.LinuxLayout). What
// has no Linux equivalent through these readers (Windows scheduled tasks,
// Defender, PowerShell profiles, the Windows Python layout) returns
// platform.ErrNotApplicable; the Linux weekly review is a systemd user timer,
// detected separately (internal/scan/scheduledtasks). All I/O is local; no
// network calls are made anywhere in this package.
package linux

import (
	"context"
	"fmt"

	"github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/platform"
)

// ManagedSettingsDir holds Claude Code's managed settings files on Linux
// (https://code.claude.com/docs/en/managed-settings: /etc/claude-code/
// managed-settings.json).
const ManagedSettingsDir = "/etc/claude-code"

// CursorEnterpriseHooks is Cursor's enterprise hooks file on Linux and WSL
// (https://cursor.com/docs/hooks: /etc/cursor/hooks.json), read like Claude
// Code's managed settings.
const CursorEnterpriseHooks = "/etc/cursor/hooks.json"

// LinuxPlatform implements platform.Platform on Linux.
type LinuxPlatform struct {
	*windows.WindowsPlatform
}

var _ platform.Platform = (*LinuxPlatform)(nil)

// New returns the Linux platform: the agents' files on the Linux layout, and
// Claude Code's managed settings and Cursor's enterprise hooks at their Linux
// locations.
func New() *LinuxPlatform {
	return NewWith(ManagedSettingsDir, CursorEnterpriseHooks)
}

// NewWith is New with Claude Code's managed settings folder and Cursor's
// enterprise hooks file given. For tests.
func NewWith(managedDir, cursorEnterpriseHooks string) *LinuxPlatform {
	return &LinuxPlatform{windows.NewOn(windows.Options{
		Layout: agentdirs.LinuxLayout, ManagedDir: managedDir, CursorEnterpriseHooks: cursorEnterpriseHooks,
	})}
}

// ListShellProfiles: PowerShell profiles do not apply on Linux (POSIX shell
// profiles are a later milestone).
func (p *LinuxPlatform) ListShellProfiles(context.Context) ([]platform.ShellProfile, error) {
	return nil, fmt.Errorf("shell profiles: %w", platform.ErrNotApplicable)
}

// ListScheduledTasks: Windows Task Scheduler does not apply on Linux; the
// weekly review is a systemd user timer, detected separately.
func (p *LinuxPlatform) ListScheduledTasks(context.Context) ([]platform.ScheduledTask, error) {
	return nil, fmt.Errorf("scheduled tasks: %w", platform.ErrNotApplicable)
}

// ListDefenderExclusions: Microsoft Defender does not exist on Linux.
func (p *LinuxPlatform) ListDefenderExclusions(context.Context) ([]platform.DefenderExclusion, error) {
	return nil, fmt.Errorf("defender exclusions: %w", platform.ErrNotApplicable)
}

// PythonSitePackages (BV-100, F0a): the inherited resolver uses the Windows
// Python layout, which does not apply on Linux. The Linux layout is a later
// milestone; until then the Python cross does not run on Linux. Not a failure
// and not a coverage gap.
func (p *LinuxPlatform) PythonSitePackages([]string) ([]string, error) {
	return nil, fmt.Errorf("python site-packages: %w", platform.ErrNotApplicable)
}
