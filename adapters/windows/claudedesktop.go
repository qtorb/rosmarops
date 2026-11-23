package windows

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/company"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/plistsubset"
)

// claudeDesktopPolicyKey is Claude Desktop's policy key on Windows
// (support.claude.com/en/articles/12622667): HKLM, else HKCU; when HKLM
// holds any value, HKCU is ignored.
const claudeDesktopPolicyKey = `SOFTWARE\Policies\Claude`

// Claude Desktop's managed files on macOS (the com.anthropic.claudefordesktop
// managed preferences, for the computer and then for the user) and Linux
// (claude.com/docs/third-party/claude-desktop/configuration-format).
const (
	claudeDesktopMacDomain  = "com.anthropic.claudefordesktop.plist"
	claudeDesktopMacManaged = "/Library/Managed Preferences"
	claudeDesktopLinuxFile  = "/etc/claude-desktop/managed-settings.json"
)

// claudeDesktopExtensionsDir is the folder whose file names are the
// installed desktop extensions' ids.
const claudeDesktopExtensionsDir = "Claude Extensions Settings"

// claudeDesktopPolicies are the policy values RosmarOps reads, by name.
var claudeDesktopPolicies = []string{"isLocalDevMcpEnabled", "isDesktopExtensionEnabled", "mcpPersistentAlwaysAllowEnabled"}

// ListClaudeDesktop implements platform.Platform.
func (w *WindowsPlatform) ListClaudeDesktop(ctx context.Context) (platform.ClaudeDesktopScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.ClaudeDesktopScan{}, err
	}
	var reg RegistryReader
	if w.managed != nil {
		reg = w.managed.registry
	}
	files := w.desktopManaged
	if files == nil {
		files = claudeDesktopManagedFiles(w.layout)
	}
	return ReadClaudeDesktop(ctx, w.layout, home, nil, reg, files, company.ReadAdminFile), nil
}

// claudeDesktopManagedFiles are the managed files of a layout, in the
// order they are looked for: the first that exists is the policy.
func claudeDesktopManagedFiles(l agentdirs.Layout) []string {
	switch l {
	case agentdirs.MacOSLayout:
		files := []string{filepath.Join(claudeDesktopMacManaged, claudeDesktopMacDomain)}
		if u, err := user.Current(); err == nil && u.Username != "" && !strings.ContainsAny(u.Username, `/\`) {
			files = append(files, filepath.Join(claudeDesktopMacManaged, u.Username, claudeDesktopMacDomain))
		}
		return files
	case agentdirs.LinuxLayout:
		return []string{claudeDesktopLinuxFile}
	}
	return nil
}

// ReadClaudeDesktop is ListClaudeDesktop with its inputs given, for tests:
// the layout, home and environment, the registry reader (nil off Windows),
// the managed files to look for, and the reader that checks a managed
// file's ownership before reading it.
func ReadClaudeDesktop(ctx context.Context, l agentdirs.Layout, home string, env agentdirs.Env, reg RegistryReader, managedFiles []string, readAdmin func(string) ([]byte, error)) platform.ClaudeDesktopScan {
	var scan platform.ClaudeDesktopScan
	for _, p := range l.Detect(home, env) {
		if p.Product == agentdirs.ClaudeDesktop {
			scan.Present, scan.ConfigDir = true, p.ConfigDir
		}
	}
	if !scan.Present {
		return scan
	}
	dirs := []string{l.ClaudeDesktopDir(home, env)}
	if l == agentdirs.WindowsLayout {
		dirs = append(dirs, l.ClaudeDesktopMSIXDir(home, env))
	}
	scan.Extensions = claudeDesktopExtensions(dirs)
	if reg != nil {
		scan.Managed = readClaudeDesktopRegistry(ctx, reg)
	}
	if scan.Managed.Path == "" {
		for _, f := range managedFiles {
			if _, err := os.Lstat(f); err != nil {
				continue
			}
			scan.Managed = readClaudeDesktopFile(f, readAdmin)
			break
		}
	}
	return scan
}

// claudeDesktopExtensions lists the extension ids from the file names in
// each folder's Claude Extensions Settings. The files are never opened:
// they can hold an extension's secrets.
func claudeDesktopExtensions(dirs []string) []string {
	seen := map[string]bool{}
	for _, d := range dirs {
		entries, err := os.ReadDir(filepath.Join(d, claudeDesktopExtensionsDir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.EqualFold(filepath.Ext(name), ".json") || strings.HasPrefix(name, ".") {
				continue
			}
			seen[strings.TrimSuffix(name, filepath.Ext(name))] = true
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// readClaudeDesktopRegistry reads the policy values of HKLM, else HKCU.
func readClaudeDesktopRegistry(ctx context.Context, reg RegistryReader) platform.ClaudeDesktopManaged {
	for _, hive := range []string{HiveHKLM, HiveHKCU} {
		m := platform.ClaudeDesktopManaged{Path: "registry:" + hive + `\` + claudeDesktopPolicyKey, Status: platform.MCPSourceRead}
		set := false
		for _, name := range claudeDesktopPolicies {
			v, ok, err := reg.Uint32Value(ctx, hive, claudeDesktopPolicyKey, name)
			if err != nil || !ok {
				continue
			}
			setClaudeDesktopPolicy(&m, name, v != 0)
			set = true
		}
		if set {
			return m
		}
	}
	return platform.ClaudeDesktopManaged{}
}

// readClaudeDesktopFile reads a managed plist (macOS) or JSON (Linux) file
// after the ownership check. A file that fails it is not read and says why.
func readClaudeDesktopFile(path string, readAdmin func(string) ([]byte, error)) platform.ClaudeDesktopManaged {
	m := platform.ClaudeDesktopManaged{Path: path}
	raw, err := readAdmin(path)
	if err != nil {
		m.Status = platform.MCPSourceFailed
		m.Reason = graph.Reason(graph.ReasonReadError, err.Error())
		if errors.Is(err, company.ErrUntrusted) {
			m.Reason = graph.Reason(graph.ReasonReadError, "not owned by an administrator: "+err.Error())
		}
		return m
	}
	values := map[string]bool{}
	if strings.HasSuffix(path, ".plist") {
		doc, err := plistsubset.Parse(raw)
		if err != nil {
			m.Status, m.Reason = platform.MCPSourceFailed, graph.Reason(graph.ReasonParseError, err.Error())
			return m
		}
		for _, name := range claudeDesktopPolicies {
			if v, ok := doc[name]; ok && v.Kind == plistsubset.KindBool {
				values[name] = v.Bool
			}
		}
	} else {
		var doc map[string]json.RawMessage
		if err := json.Unmarshal(raw, &doc); err != nil {
			m.Status, m.Reason = platform.MCPSourceFailed, graph.Reason(graph.ReasonParseError, err.Error())
			return m
		}
		for _, name := range claudeDesktopPolicies {
			var b bool
			if msg, ok := doc[name]; ok && json.Unmarshal(msg, &b) == nil {
				values[name] = b
			}
		}
	}
	m.Status = platform.MCPSourceRead
	for name, b := range values {
		setClaudeDesktopPolicy(&m, name, b)
	}
	return m
}

func setClaudeDesktopPolicy(m *platform.ClaudeDesktopManaged, name string, b bool) {
	v := b
	switch name {
	case "isLocalDevMcpEnabled":
		m.LocalMCP = &v
	case "isDesktopExtensionEnabled":
		m.Extensions = &v
	case "mcpPersistentAlwaysAllowEnabled":
		m.PersistentAlwaysAllow = &v
	}
}
