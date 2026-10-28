package windows

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

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
	if runtime.GOOS == "windows" {
		enterprise = cursorEnterpriseHooks // where it exists, as managed settings
	}
	return ReadCursor(home, enterprise, repoRoots), nil
}

// ReadCursor is ListCursor with the home folder and the enterprise hooks
// file given. For tests.
func ReadCursor(home, enterpriseHooks string, repoRoots []string) platform.CursorScan {
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
	add(filepath.Join(dir, "cli-config.json"), platform.CursorCLI, platform.CursorScopeUser, "")
	add(filepath.Join(dir, "hooks.json"), platform.CursorHooks, platform.CursorScopeUser, "")
	if enterpriseHooks != "" {
		add(enterpriseHooks, platform.CursorHooks, platform.CursorScopeEnterprise, "")
	}
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

// cursorDoc is the subset of Cursor's files RosmarOps keeps. Unknown keys
// are ignored: each file has more keys than RosmarOps reads.
type cursorDoc struct {
	TerminalAllowlist        *[]string `json:"terminalAllowlist"`
	MCPAllowlist             *[]string `json:"mcpAllowlist"`
	Type                     string    `json:"type"`
	ReadBoundary             string    `json:"readBoundary"`
	AdditionalReadwritePaths []string  `json:"additionalReadwritePaths"`
	Permissions              struct {
		Allow []string `json:"allow"`
		Deny  []string `json:"deny"`
	} `json:"permissions"`
}

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
	var doc cursorDoc
	conflicts, err := jsonkeys.Decode(raw, &doc)
	if err != nil {
		slog.Warn("cursor: could not parse", "path", path, "err", err)
		f.Status, f.Reason = platform.MCPSourceFailed, graph.Reason(graph.ReasonParseError, err.Error())
		return f
	}
	f.KeyConflicts = conflicts
	switch kind {
	case platform.CursorPermissions:
		if doc.TerminalAllowlist != nil {
			f.TerminalAllowlist, f.HasTerminalAllowlist = append([]string{}, *doc.TerminalAllowlist...), true
		}
		if doc.MCPAllowlist != nil {
			f.MCPAllowlist, f.HasMCPAllowlist = append([]string{}, *doc.MCPAllowlist...), true
		}
	case platform.CursorSandbox:
		f.SandboxType, f.ReadBoundary, f.AdditionalReadwritePaths = doc.Type, doc.ReadBoundary, doc.AdditionalReadwritePaths
	case platform.CursorCLI:
		f.CLIAllow, f.CLIDeny = doc.Permissions.Allow, doc.Permissions.Deny
	}
	return f
}
