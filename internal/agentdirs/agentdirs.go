// Package agentdirs resolves the configuration folders of the agents that
// RosmarOps detects without evaluating them (decision 2026-10-27, agentes
// nuevos, N1), on the Windows layout or, through Layout, on macOS's
// (decision 2026-10-28, macOS, M1a). The adapter, the instructions lister
// and the credential files collector use the same folders, so an agent's
// ai_client, its instruction files and its credentials agree. Existence
// only: nothing here opens a file.
package agentdirs

import (
	"os"
	"path/filepath"
	"runtime"
)

// Layout is the folder layout of an operating system. The zero value is
// Windows's, the one the package-level functions use.
type Layout int

const (
	// WindowsLayout: %APPDATA%, %LOCALAPPDATA%.
	WindowsLayout Layout = iota
	// MacOSLayout: ~/Library/Application Support and the agents' own
	// dot folders (LECTURA-101 §2).
	MacOSLayout
)

// Current is this system's layout: macOS's on darwin, Windows's elsewhere
// (Linux keeps the Windows layout, as before M1a).
func Current() Layout {
	if runtime.GOOS == "darwin" {
		return MacOSLayout
	}
	return WindowsLayout
}

// appSupport is ~/Library/Application Support (macOS).
func appSupport(home string) string {
	return filepath.Join(home, "Library", "Application Support")
}

// Product codes (graph.clientDisplayNames).
const (
	Codex         = "codex"
	Antigravity   = "antigravity"
	Hermes        = "hermes"
	ClaudeDesktop = "claude-desktop"
)

// Env reads environment variables; os.LookupEnv outside tests.
type Env func(string) (string, bool)

func lookup(env Env) Env {
	if env == nil {
		return os.LookupEnv
	}
	return env
}

// isDir reports a folder (os.Stat: a link to a folder counts, as it does
// for the agent).
func isDir(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.IsDir()
}

// exists reports any entry at p (os.Lstat, not followed).
func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// varDir is the folder an environment variable names, when it is set and
// is a folder.
func varDir(env Env, name string) (string, bool) {
	if v, ok := lookup(env)(name); ok && v != "" && isDir(v) {
		return v, true
	}
	return "", false
}

// appData is %APPDATA%, or <home>\AppData\Roaming when unset.
func appData(home string, env Env) string {
	if v, ok := lookup(env)("APPDATA"); ok && v != "" {
		return v
	}
	return filepath.Join(home, "AppData", "Roaming")
}

// localAppData is %LOCALAPPDATA%, or <home>\AppData\Local when unset.
func localAppData(home string, env Env) string {
	if v, ok := lookup(env)("LOCALAPPDATA"); ok && v != "" {
		return v
	}
	return filepath.Join(home, "AppData", "Local")
}

// CodexHome is Codex's folder: CODEX_HOME when set and a folder, otherwise
// <home>\.codex (openai/codex, codex-rs/utils/home-dir/src/lib.rs).
func CodexHome(home string, env Env) string {
	if v, ok := varDir(env, "CODEX_HOME"); ok {
		return v
	}
	return filepath.Join(home, ".codex")
}

// HermesHome is Hermes Agent's folder on the Windows layout.
func HermesHome(home string, env Env) string { return WindowsLayout.HermesHome(home, env) }

// HermesHome is Hermes Agent's folder: HERMES_HOME when set and a folder,
// otherwise %LOCALAPPDATA%\hermes on Windows (Hermes docs,
// user-guide/windows-native) and ~/.hermes on macOS
// (hermes_constants.py:55-64 @ 0431192d, _platform_default_hermes_home).
func (l Layout) HermesHome(home string, env Env) string {
	if v, ok := varDir(env, "HERMES_HOME"); ok {
		return v
	}
	if l == MacOSLayout {
		return filepath.Join(home, ".hermes")
	}
	return filepath.Join(localAppData(home, env), "hermes")
}

// AntigravityDir is Antigravity's configuration folder, <home>\.gemini,
// shared by the IDE, the app and its CLI, on both layouts.
func AntigravityDir(home string) string { return filepath.Join(home, ".gemini") }

// AntigravityUserDir is the IDE's VS Code layer on the Windows layout.
func AntigravityUserDir(home string, env Env) string {
	return WindowsLayout.AntigravityUserDir(home, env)
}

// AntigravityUserDir is the IDE's VS Code layer: %APPDATA%\Antigravity\User
// on Windows, ~/Library/Application Support/Antigravity/User on macOS (VS
// Code's convention; no official source, LECTURA-101 §2).
func (l Layout) AntigravityUserDir(home string, env Env) string {
	if l == MacOSLayout {
		return filepath.Join(appSupport(home), "Antigravity", "User")
	}
	return filepath.Join(appData(home, env), "Antigravity", "User")
}

// ClaudeDesktopDir is Claude Desktop's folder on the Windows layout.
func ClaudeDesktopDir(home string, env Env) string { return WindowsLayout.ClaudeDesktopDir(home, env) }

// ClaudeDesktopDir is Claude Desktop's folder: %APPDATA%\Claude on Windows
// (the installer's path; the MSIX one is not covered),
// ~/Library/Application Support/Claude on macOS (modelcontextprotocol docs,
// LECTURA-101 §2).
func (l Layout) ClaudeDesktopDir(home string, env Env) string {
	if l == MacOSLayout {
		return filepath.Join(appSupport(home), "Claude")
	}
	return filepath.Join(appData(home, env), "Claude")
}

// VSCodeUserDir is VS Code's user folder: %APPDATA%\Code\User on Windows,
// ~/Library/Application Support/Code/User on macOS (vscode-docs, "Settings
// file locations"; LECTURA-101 §2).
func (l Layout) VSCodeUserDir(home string, env Env) string {
	if l == MacOSLayout {
		return filepath.Join(appSupport(home), "Code", "User")
	}
	return filepath.Join(appData(home, env), "Code", "User")
}

// ClineDir is Cline's settings folder, under VS Code's global storage of its
// extension saoudrizwan.claude-dev (LECTURA-101 §2: the id is Cline's; the
// macOS folder is VS Code's convention).
func (l Layout) ClineDir(home string, env Env) string {
	return filepath.Join(l.VSCodeUserDir(home, env), "globalStorage", "saoudrizwan.claude-dev", "settings")
}

// Present is an agent found on disk: its product, its configuration folder
// (the identity of its ai_client) and the entry that showed it.
type Present struct {
	Product   string
	ConfigDir string
	Marker    string
}

// Detect lists the agents present on the Windows layout.
func Detect(home string, env Env) []Present { return WindowsLayout.Detect(home, env) }

// Detect lists the agents present on layout l, in a fixed order:
//   - Codex: CodexHome\config.toml or CodexHome\auth.json;
//   - Antigravity: .gemini\config\mcp_config.json, the folder
//     .gemini\antigravity or the folder AntigravityUserDir; a
//     .gemini folder alone is Gemini CLI's and does not count;
//   - Hermes Agent: HermesHome\config.yaml;
//   - Claude Desktop: <ClaudeDesktopDir>\claude_desktop_config.json.
func (l Layout) Detect(home string, env Env) []Present {
	if home == "" {
		return nil
	}
	var out []Present
	codex := CodexHome(home, env)
	for _, name := range []string{"config.toml", "auth.json"} {
		if p := filepath.Join(codex, name); exists(p) {
			out = append(out, Present{Codex, codex, p})
			break
		}
	}
	gemini := AntigravityDir(home)
	switch {
	case exists(filepath.Join(gemini, "config", "mcp_config.json")):
		out = append(out, Present{Antigravity, gemini, filepath.Join(gemini, "config", "mcp_config.json")})
	case isDir(filepath.Join(gemini, "antigravity")):
		out = append(out, Present{Antigravity, gemini, filepath.Join(gemini, "antigravity")})
	case isDir(l.AntigravityUserDir(home, env)):
		out = append(out, Present{Antigravity, gemini, l.AntigravityUserDir(home, env)})
	}
	hermes := l.HermesHome(home, env)
	if p := filepath.Join(hermes, "config.yaml"); exists(p) {
		out = append(out, Present{Hermes, hermes, p})
	}
	desktop := l.ClaudeDesktopDir(home, env)
	if p := filepath.Join(desktop, "claude_desktop_config.json"); exists(p) {
		out = append(out, Present{ClaudeDesktop, desktop, p})
	}
	return out
}

// Products is the set of products in present.
func Products(present []Present) map[string]bool {
	out := map[string]bool{}
	for _, p := range present {
		out[p.Product] = true
	}
	return out
}
