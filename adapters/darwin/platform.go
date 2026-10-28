// Package darwin provides the macOS implementation of platform.Platform
// (decision 2026-10-28, macOS, M1a: RosmarOps reads on macOS what it reads
// on Windows). The agents' files are read by the same portable readers as
// on Windows (adapters/windows), on the macOS folder layout
// (agentdirs.MacOSLayout, LECTURA-101 §2). What has no macOS equivalent yet
// (scheduled tasks, Defender, PowerShell profiles) returns
// platform.ErrNotApplicable; its macOS counterparts are M1b. All I/O is
// local; no network calls are made anywhere in this package.
package darwin

import (
	"context"
	"fmt"

	"github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/platform"
)

// Managed locations on macOS.
const (
	// ManagedSettingsDir holds Claude Code's managed settings files
	// (https://code.claude.com/docs/en/settings, managed-settings:
	// /Library/Application Support/ClaudeCode/managed-settings.json;
	// LECTURA-101 §2). The com.anthropic.claudecode managed preferences
	// domain is not read (a plist: M1b).
	ManagedSettingsDir = "/Library/Application Support/ClaudeCode"

	// CursorEnterpriseHooks is Cursor's enterprise hooks file on macOS
	// (LECTURA-101 §2: no official source could be opened).
	CursorEnterpriseHooks = "/Library/Application Support/Cursor/hooks.json"
)

// DarwinPlatform implements platform.Platform on macOS.
type DarwinPlatform struct {
	*windows.WindowsPlatform
}

var _ platform.Platform = (*DarwinPlatform)(nil)

// New returns the macOS platform: the agents' files on the macOS layout,
// Claude Code's managed settings and Cursor's enterprise hooks at their
// macOS locations.
func New() *DarwinPlatform {
	return NewWith(ManagedSettingsDir, CursorEnterpriseHooks)
}

// NewWith is New with Claude Code's managed settings folder and Cursor's
// enterprise hooks file given. For tests.
func NewWith(managedDir, cursorEnterpriseHooks string) *DarwinPlatform {
	return &DarwinPlatform{windows.NewOn(windows.Options{
		Layout: agentdirs.MacOSLayout, ManagedDir: managedDir, CursorEnterpriseHooks: cursorEnterpriseHooks,
	})}
}

// ListShellProfiles: PowerShell profiles do not apply on macOS (zsh and
// bash profiles are M1b).
func (d *DarwinPlatform) ListShellProfiles(context.Context) ([]platform.ShellProfile, error) {
	return nil, fmt.Errorf("shell profiles: %w", platform.ErrNotApplicable)
}

// ListScheduledTasks: Windows Task Scheduler does not apply on macOS
// (LaunchAgents and LaunchDaemons are M1b).
func (d *DarwinPlatform) ListScheduledTasks(context.Context) ([]platform.ScheduledTask, error) {
	return nil, fmt.Errorf("scheduled tasks: %w", platform.ErrNotApplicable)
}

// ListDefenderExclusions: Defender does not exist on macOS (declared as a
// limit, M1b).
func (d *DarwinPlatform) ListDefenderExclusions(context.Context) ([]platform.DefenderExclusion, error) {
	return nil, fmt.Errorf("defender exclusions: %w", platform.ErrNotApplicable)
}

// PythonSitePackages (BV-100, F0a): the inherited resolver uses the Windows
// Python layout (ProgramFiles, LOCALAPPDATA, Lib\site-packages), which does
// not apply on macOS. The macOS layout (framework/Homebrew/pyenv site-packages
// and lib/pythonX.Y/site-packages under .venv/venv) is M1b, so until then the
// Python cross does not run on macOS. The MCP and npm-global crosses do not go
// through this and keep working. It is not a failure and not a coverage gap.
func (d *DarwinPlatform) PythonSitePackages([]string) ([]string, error) {
	return nil, fmt.Errorf("python site-packages: %w", platform.ErrNotApplicable)
}
