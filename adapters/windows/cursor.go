package windows

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/jsonc"
	"github.com/qtorb/rosmarops/internal/jsonkeys"
	"github.com/qtorb/rosmarops/internal/platform"
)

// cursorEnterpriseHooks is Cursor's enterprise hooks file on Windows
// (https://cursor.com/docs/hooks).
const cursorEnterpriseHooks = `C:\ProgramData\Cursor\hooks.json`

// ListCursor reads Cursor's files (decision 2026-10-25, Cursor, K10), read
// only, at most MaxConfigBytes each: ~/.cursor/{permissions,sandbox,
// cli-config,hooks}.json, <repo>/.cursor/{permissions,sandbox,cli,hooks}.json
// and the enterprise hooks file, plus the existence of
// <repo>/.cursor/.workspace-trusted. Which files exist is logged (K11, as
// adjusted).
func (w *WindowsPlatform) ListCursor(_ context.Context, repoRoots []string) (platform.CursorScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.CursorScan{}, fmt.Errorf("resolve home dir: %w", err)
	}
	enterprise := ""
	cliDir := filepath.Join(home, ".cursor")
	switch {
	case w.layout == agentdirs.MacOSLayout:
		enterprise = w.cursorEnterprise
	case w.layout == agentdirs.LinuxLayout:
		enterprise = w.cursorEnterprise
		cliDir = CursorCLIConfigDir(home, os.Getenv)
	case runtime.GOOS == "windows":
		enterprise = cursorEnterpriseHooks // where it exists, as managed settings
	}
	return ReadCursorWith(home, enterprise, cliDir, repoRoots), nil
}

// CursorCLIConfigDir is the folder of Cursor's CLI configuration
// (cli-config.json) on Linux, resolved as the CLI does
// (https://cursor.com/docs/cli/reference/configuration): $CURSOR_CONFIG_DIR,
// else $XDG_CONFIG_HOME/cursor, else ~/.cursor. A relative value is ignored,
// as the XDG specification asks.
func CursorCLIConfigDir(home string, getenv func(string) string) string {
	if d := getenv("CURSOR_CONFIG_DIR"); filepath.IsAbs(d) {
		return filepath.Clean(d)
	}
	if d := getenv("XDG_CONFIG_HOME"); filepath.IsAbs(d) {
		return filepath.Join(d, "cursor")
	}
	return filepath.Join(home, ".cursor")
}

// ReadCursor is ListCursor with the home folder and the enterprise hooks
// file given, and cli-config.json in ~/.cursor. For tests.
func ReadCursor(home, enterpriseHooks string, repoRoots []string) platform.CursorScan {
	return ReadCursorWith(home, enterpriseHooks, filepath.Join(home, ".cursor"), repoRoots)
}

// ReadCursorWith is ReadCursor with the folder of cli-config.json given.
func ReadCursorWith(home, enterpriseHooks, cliDir string, repoRoots []string) platform.CursorScan {
	dir := filepath.Join(home, ".cursor")
	scan := platform.CursorScan{ConfigDir: dir}
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		scan.Exists = true
	}
	slog.Info("cursor: config folder", "path", dir, "exists", scan.Exists)
	add := func(path, kind, scope, root string) {
		f := readCursorFile(path, kind, scope, root)
		slog.Info("cursor: file", "path", path, "exists", f.Status != platform.MCPSourceMissing)
		scan.Files = append(scan.Files, f)
	}
	add(filepath.Join(dir, "permissions.json"), platform.CursorPermissions, platform.CursorScopeUser, "")
	add(filepath.Join(dir, "sandbox.json"), platform.CursorSandbox, platform.CursorScopeUser, "")
	add(filepath.Join(cliDir, "cli-config.json"), platform.CursorCLI, platform.CursorScopeUser, "")
	add(filepath.Join(dir, "hooks.json"), platform.CursorHooks, platform.CursorScopeUser, "")
	if enterpriseHooks != "" {
		add(enterpriseHooks, platform.CursorHooks, platform.CursorScopeEnterprise, "")
	}
	scan.TeamHooks = cursorTeamHooks(filepath.Join(dir, "managed"))
	for _, root := range repoRoots {
		ws := filepath.Join(root, ".cursor")
		add(filepath.Join(ws, "permissions.json"), platform.CursorPermissions, platform.CursorScopeProject, root)
		add(filepath.Join(ws, "sandbox.json"), platform.CursorSandbox, platform.CursorScopeProject, root)
		add(filepath.Join(ws, "cli.json"), platform.CursorCLI, platform.CursorScopeProject, root)
		add(filepath.Join(ws, "hooks.json"), platform.CursorHooks, platform.CursorScopeProject, root)
		trusted := filepath.Join(ws, ".workspace-trusted")
		if _, err := os.Lstat(trusted); err == nil {
			scan.WorkspaceTrusted = append(scan.WorkspaceTrusted, root)
			slog.Info("cursor: workspace trusted marker", "path", trusted)
		}
	}
	return scan
}

// The subset of each Cursor file RosmarOps keeps, one struct per file type
// (BV-94b): the strict reading of keys (jsonkeys, B5) compares a file's keys
// only with the keys RosmarOps reads from that file. Unknown keys are
// ignored: each file has more keys than RosmarOps reads.
type (
	// cursorPermissionsDoc is permissions.json.
	cursorPermissionsDoc struct {
		TerminalAllowlist *[]string `json:"terminalAllowlist"`
		MCPAllowlist      *[]string `json:"mcpAllowlist"`
		ApprovalMode      string    `json:"approvalMode"`
	}
	// cursorSandboxDoc is sandbox.json.
	cursorSandboxDoc struct {
		Type                     string   `json:"type"`
		ReadBoundary             string   `json:"readBoundary"`
		AdditionalReadwritePaths []string `json:"additionalReadwritePaths"`
		NetworkPolicy            struct {
			Default any `json:"default"`
		} `json:"networkPolicy"`
	}
	// cursorCLIDoc is cli-config.json and cli.json.
	cursorCLIDoc struct {
		Permissions struct {
			Allow []string `json:"allow"`
			Deny  []string `json:"deny"`
		} `json:"permissions"`
		// The CLI's own settings: any JSON type is accepted, so a value of
		// another type is ignored rather than failing the whole file.
		ApprovalMode any `json:"approvalMode"`
		Sandbox      struct {
			Mode          any `json:"mode"`
			NetworkAccess any `json:"networkAccess"`
		} `json:"sandbox"`
	}
	// cursorHooksDoc is hooks.json: RosmarOps reads none of its keys (the
	// file itself is an executable resource of Cursor).
	cursorHooksDoc struct{}
)

// readCursorFile reads one Cursor file. permissions.json is JSONC
// (https://cursor.com/docs/reference/permissions.md): its comments are
// removed before decoding; the other files are JSON.
func readCursorFile(path, kind, scope, root string) platform.CursorFile {
	f := platform.CursorFile{Path: path, Kind: kind, Scope: scope, Root: root}
	raw, status, reason := readBounded(path)
	f.Status, f.Reason = status, reason
	if status != platform.MCPSourceRead {
		return f
	}
	if kind == platform.CursorPermissions {
		raw = jsonc.Strip(raw)
	}
	var (
		perm    cursorPermissionsDoc
		sandbox cursorSandboxDoc
		cli     cursorCLIDoc
		hooks   cursorHooksDoc
		doc     any = &hooks
	)
	switch kind {
	case platform.CursorPermissions:
		doc = &perm
	case platform.CursorSandbox:
		doc = &sandbox
	case platform.CursorCLI:
		doc = &cli
	}
	conflicts, err := jsonkeys.Decode(raw, doc)
	if err != nil {
		slog.Warn("cursor: could not parse", "path", path, "err", err)
		f.Status, f.Reason = platform.MCPSourceFailed, graph.Reason(graph.ReasonParseError, err.Error())
		return f
	}
	f.KeyConflicts = conflicts
	switch kind {
	case platform.CursorPermissions:
		if perm.TerminalAllowlist != nil {
			f.TerminalAllowlist, f.HasTerminalAllowlist = append([]string{}, *perm.TerminalAllowlist...), true
		}
		if perm.MCPAllowlist != nil {
			f.MCPAllowlist, f.HasMCPAllowlist = append([]string{}, *perm.MCPAllowlist...), true
		}
		// CTO-56 §1.1: approvalMode, when it is one of its three values, sets
		// the run mode; any other value is not kept.
		switch perm.ApprovalMode {
		case "allowlist", "unrestricted", "manual":
			f.ApprovalMode = perm.ApprovalMode
		}
	case platform.CursorSandbox:
		f.SandboxType, f.ReadBoundary, f.AdditionalReadwritePaths = sandbox.Type, sandbox.ReadBoundary, sandbox.AdditionalReadwritePaths
		// networkPolicy.default: "allow" or "deny" (default "deny").
		switch d, _ := sandbox.NetworkPolicy.Default.(string); d {
		case "allow", "deny":
			f.NetworkDefault = d
		}
	case platform.CursorCLI:
		f.CLIAllow, f.CLIDeny = cli.Permissions.Allow, cli.Permissions.Deny
		// Only the user's cli-config.json holds the CLI's own settings; a
		// project's cli.json holds only permissions.
		if scope != platform.CursorScopeUser {
			break
		}
		switch m, _ := cli.ApprovalMode.(string); m {
		case "allowlist", "auto-review", "unrestricted":
			f.CLIApprovalMode = m
		}
		switch m, _ := cli.Sandbox.Mode.(string); m {
		case "enabled", "disabled":
			f.CLISandboxMode = m
		}
		// sandbox.networkAccess: its values are not documented
		// (https://cursor.com/docs/cli/reference/configuration.md, read
		// 22-11-2026: «Network access setting for sandbox mode»), so it is
		// kept as written, bounded, and never scored.
		if v, ok := cli.Sandbox.NetworkAccess.(string); ok && len(v) <= 64 {
			f.CLINetworkAccess = v
			slog.Info("cursor: cli sandbox.networkAccess read, not scored", "path", path)
		}
	}
	return f
}

// maxCursorTeamHooks bounds the team hook files listed.
const maxCursorTeamHooks = 200

// cursorTeamHooks lists the files in <managed>/team_<id>/hooks/: the command
// hooks Cursor's desktop app writes there on every sync from a team's
// dashboard (https://cursor.com/docs/hooks, read 22-11-2026: «The desktop
// app writes each command hook to ~/.cursor/managed/team_<team ID>/hooks/»).
// It only lists names: no file is opened, and no link is followed.
func cursorTeamHooks(managed string) []string {
	teams, err := os.ReadDir(managed)
	if err != nil {
		return nil
	}
	var out []string
	for _, t := range teams {
		if !t.IsDir() || !strings.HasPrefix(t.Name(), "team_") {
			continue
		}
		dir := filepath.Join(managed, t.Name(), "hooks")
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.Type().IsRegular() || len(out) >= maxCursorTeamHooks {
				continue
			}
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	slog.Info("cursor: team hooks", "folder", managed, "files", len(out))
	return out
}
