package windows

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/platform"
)

// clineHookEvents are the hook file base names Cline runs (CTO-22 Cline §5,
// core/hooks/hook-file-config.ts). clineHookExts are the script extensions.
var (
	clineHookEvents = []string{"PreToolUse", "PostToolUse", "TaskStart", "TaskComplete", "UserPromptSubmit"}
	clineHookExts   = []string{".ps1", ".py", ".js", ".ts", ".sh", ".cmd", ".bat"}
)

// ListCline reads the Cline VS Code extension's configuration (BV-193, CTO-22
// Cline §2), read only: ~/.cline/data/globalState.json, the installed extension
// version and the repository hook files. Its credentials (secrets.json,
// providers.json) come through credfiles; its MCP through ListMCPServers.
func (w *WindowsPlatform) ListCline(_ context.Context, repoRoots []string) (platform.ClineScan, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return platform.ClineScan{}, fmt.Errorf("resolve home dir: %w", err)
	}
	env := agentdirs.Env(os.LookupEnv)
	return ReadCline(w.layout, home, env, repoRoots), nil
}

// ReadCline is ListCline with the layout, home, environment and repo roots
// given, for tests. It is a pure function.
func ReadCline(layout agentdirs.Layout, home string, env agentdirs.Env, repoRoots []string) platform.ClineScan {
	dataDir := agentdirs.ClineDataDir(home, env)
	scan := platform.ClineScan{ConfigDir: dataDir, HooksEnabled: true}

	readClineGlobalState(&scan, filepath.Join(dataDir, "globalState.json"))
	scan.Version = clineExtensionVersion(layout, home, env)

	// Repository hooks Cline runs as processes (CTO-22 Cline §5): <repo>\.clinerules\hooks
	// and <repo>\.cline\hooks, named by event with a script extension.
	for _, root := range repoRoots {
		for _, sub := range [][]string{{".clinerules", "hooks"}, {".cline", "hooks"}} {
			dir := filepath.Join(root, filepath.Join(sub...))
			for _, p := range clineHookFiles(dir) {
				var size int64
				if fi, err := os.Stat(p); err == nil {
					size = fi.Size()
				}
				scan.Hooks = append(scan.Hooks, platform.ClineHook{Path: p, Root: root, Size: size})
			}
		}
	}

	// Present: the data folder, the globalState file, or the installed version.
	scan.Present = isDirExists(dataDir) || scan.GlobalStateStatus == platform.MCPSourceRead || scan.Version != ""
	return scan
}

// readClineGlobalState reads the auto-approval actions, the migration marker,
// hooksEnabled and the legacy yolo toggles (CTO-22 Cline §4).
func readClineGlobalState(scan *platform.ClineScan, path string) {
	raw, status, _ := readBounded(path)
	scan.GlobalStateStatus = status
	if status != platform.MCPSourceRead {
		return
	}
	var doc struct {
		AutoApprovalSettings *struct {
			Actions struct {
				ReadFiles           bool `json:"readFiles"`
				EditFiles           bool `json:"editFiles"`
				ExecuteSafeCommands bool `json:"executeSafeCommands"`
				UseBrowser          bool `json:"useBrowser"`
				UseMcp              bool `json:"useMcp"`
			} `json:"actions"`
		} `json:"autoApprovalSettings"`
		MigrationVersion     int   `json:"__vscodeMigrationVersion"`
		HooksEnabled         *bool `json:"hooksEnabled"`
		YoloModeToggled      bool  `json:"yoloModeToggled"`
		AutoApproveAllToggle bool  `json:"autoApproveAllToggled"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		scan.GlobalStateStatus = platform.MCPSourceFailed
		return
	}
	scan.MigrationVersion = doc.MigrationVersion
	scan.Yolo = doc.YoloModeToggled || doc.AutoApproveAllToggle
	switch {
	case doc.YoloModeToggled:
		scan.YoloKey = "yoloModeToggled"
	case doc.AutoApproveAllToggle:
		scan.YoloKey = "autoApproveAllToggled"
	}
	if doc.HooksEnabled != nil {
		scan.HooksEnabled = *doc.HooksEnabled
	}
	if doc.AutoApprovalSettings != nil {
		scan.HasAutoApproval = true
		a := doc.AutoApprovalSettings.Actions
		scan.ReadFiles, scan.EditFiles = a.ReadFiles, a.EditFiles
		scan.ExecuteSafeCommands, scan.UseBrowser, scan.UseMcp = a.ExecuteSafeCommands, a.UseBrowser, a.UseMcp
	}
}

// clineExtensionVersion reads the version from the extension folder name
// %USERPROFILE%\.vscode\extensions\saoudrizwan.claude-dev-<version> (CTO-22
// Cline §4). The highest version string found wins.
func clineExtensionVersion(layout agentdirs.Layout, home string, env agentdirs.Env) string {
	_ = layout
	_ = env
	dir := filepath.Join(home, ".vscode", "extensions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	const prefix = "saoudrizwan.claude-dev-"
	var best string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), prefix) {
			continue
		}
		if v := strings.TrimPrefix(e.Name(), prefix); best == "" || clineVersionNewer(v, best) {
			best = v
		}
	}
	return best
}

// clineVersionNewer reports whether version a is newer than b, comparing the
// dot-separated components numerically so 4.10.0 is newer than 4.9.0 — a string
// comparison gets that wrong, picking 4.9.0 as the highest (CTO-25 A3). Each
// component's leading digits are its number; a non-numeric component counts as
// 0. Equal through the shorter version's length compares equal.
func clineVersionNewer(a, b string) bool {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var x, y int
		if i < len(as) {
			x = leadingInt(as[i])
		}
		if i < len(bs) {
			y = leadingInt(bs[i])
		}
		if x != y {
			return x > y
		}
	}
	return false
}

// leadingInt is the value of the leading decimal digits of s, or 0 if none.
func leadingInt(s string) int {
	n := 0
	for i := 0; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		n = n*10 + int(s[i]-'0')
	}
	return n
}

// clineHookFiles lists the hook files directly under dir (CTO-22 Cline §5): a
// file whose base name is a hook event and whose extension is a script one.
func clineHookFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	eventSet := map[string]bool{}
	for _, e := range clineHookEvents {
		eventSet[e] = true
	}
	extSet := map[string]bool{}
	for _, x := range clineHookExts {
		extSet[x] = true
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		base := strings.TrimSuffix(e.Name(), ext)
		if eventSet[base] && extSet[strings.ToLower(ext)] {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}
