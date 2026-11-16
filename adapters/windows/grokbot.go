package windows

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

// Grok Bot at rest (BV-248, CTO-57 §4): the app folder for its presence, and
// one file, ~/.grokbot/settings.json, by its exact path. Never another file
// of ~/.grokbot (host-token.json, host-secrets.json…) nor anything of
// %APPDATA%\Grok Bot.

// ListGrokBot implements platform.Platform.
func (w *WindowsPlatform) ListGrokBot(_ context.Context) (platform.GrokBotScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.GrokBotScan{}, fmt.Errorf("resolve home dir: %w", err)
	}
	return ReadGrokBot(home, os.Getenv("LOCALAPPDATA")), nil
}

// grokBotPermissions are the three local tool permission values (CTO-57 §3).
var grokBotPermissions = map[string]bool{"always": true, "ask": true, "never": true}

// grokBotScope is one level of settings.json the reader decodes: only these
// keys; every other key (instructions, rules, MCP texts) is skipped by the
// decoder and never kept.
type grokBotScope struct {
	LocalToolPermission            *string           `json:"localToolPermission"`
	LocalToolPermissionCeiling     *string           `json:"localToolPermissionCeiling"`
	LocalToolPermissionByMachineID map[string]string `json:"localToolPermissionByMachineId"`
	AutoReviewInstructions         *struct {
		IsEnabled *bool `json:"isEnabled"`
	} `json:"autoReviewInstructions"`
}

type grokBotSettings struct {
	grokBotScope
	AccountScopes map[string]grokBotScope `json:"accountScopes"`
}

// ReadGrokBot is ListGrokBot with the home folder and %LOCALAPPDATA% given.
func ReadGrokBot(home, localAppData string) platform.GrokBotScan {
	var scan platform.GrokBotScan
	if localAppData != "" {
		if info, err := os.Stat(filepath.Join(localAppData, "Programs", "Grok Bot")); err == nil && info.IsDir() {
			scan.Present = true
		}
	}
	scan.SettingsPath = filepath.Join(home, ".grokbot", "settings.json")
	info, err := os.Lstat(scan.SettingsPath)
	switch {
	case os.IsNotExist(err):
		scan.Status = platform.MCPSourceMissing
		slog.Info("grok bot", "present", scan.Present, "settings", "missing")
		return scan
	case err != nil:
		return grokBotFailed(scan, graph.Reason(graph.ReasonReadError, err.Error()))
	case info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular():
		return grokBotFailed(scan, graph.Reason(graph.ReasonReadError, "not a regular file"))
	}
	raw, status, reason := readBounded(scan.SettingsPath)
	if status != platform.MCPSourceRead {
		return grokBotFailed(scan, reason)
	}
	var doc grokBotSettings
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := dec.Decode(&doc); err != nil {
		// The decoder's error names a position and a type, never a value.
		return grokBotFailed(scan, graph.Reason(graph.ReasonParseError, "settings.json is not the expected JSON"))
	}
	add := func(s grokBotScope) bool {
		vals := []*string{s.LocalToolPermission, s.LocalToolPermissionCeiling}
		for _, v := range s.LocalToolPermissionByMachineID {
			v := v
			vals = append(vals, &v)
		}
		for _, v := range vals {
			if v == nil {
				continue
			}
			if !grokBotPermissions[*v] {
				return false
			}
			scan.LocalPermissions = append(scan.LocalPermissions, *v)
		}
		return true
	}
	ok := add(doc.grokBotScope)
	for _, acc := range doc.AccountScopes {
		ok = add(acc) && ok
		scan.Accounts++
		if acc.AutoReviewInstructions != nil && acc.AutoReviewInstructions.IsEnabled != nil && !*acc.AutoReviewInstructions.IsEnabled {
			scan.AutoReviewOff = true
		}
	}
	if !ok {
		scan.LocalPermissions, scan.Accounts, scan.AutoReviewOff = nil, 0, false
		return grokBotFailed(scan, graph.Reason(graph.ReasonParseError, "unknown localToolPermission value"))
	}
	scan.Status = platform.MCPSourceRead
	slog.Info("grok bot", "present", scan.Present, "settings", "read", "permissions", len(scan.LocalPermissions), "accounts", scan.Accounts)
	return scan
}

func grokBotFailed(scan platform.GrokBotScan, reason string) platform.GrokBotScan {
	scan.Status, scan.Reason = platform.MCPSourceFailed, reason
	slog.Warn("grok bot: settings.json not read", "path", scan.SettingsPath, "reason", reason)
	return scan
}
