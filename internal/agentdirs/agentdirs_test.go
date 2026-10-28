package agentdirs

import (
	"os"
	"path/filepath"
	"testing"
)

func touch(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

// envOf is an environment with only vars set.
func envOf(vars map[string]string) Env {
	return func(k string) (string, bool) {
		v, ok := vars[k]
		return v, ok
	}
}

// detected maps each product detected to its configuration folder.
func detected(home string, env Env) map[string]string {
	out := map[string]string{}
	for _, p := range Detect(home, env) {
		out[p.Product] = p.ConfigDir
	}
	return out
}

// BV-88 N1: each agent is present only by its own markers.
func TestDetect(t *testing.T) {
	none := envOf(nil)
	for name, tc := range map[string]struct {
		setup func(t *testing.T, home string) Env
		want  map[string]string // product → configuration folder, relative to home
	}{
		"nothing": {func(*testing.T, string) Env { return none }, map[string]string{}},
		"codex config.toml": {func(t *testing.T, h string) Env {
			touch(t, filepath.Join(h, ".codex", "config.toml"))
			return none
		}, map[string]string{Codex: ".codex"}},
		"codex auth.json": {func(t *testing.T, h string) Env {
			touch(t, filepath.Join(h, ".codex", "auth.json"))
			return none
		}, map[string]string{Codex: ".codex"}},
		"codex folder alone": {func(t *testing.T, h string) Env {
			touch(t, filepath.Join(h, ".codex", "history.jsonl"))
			return none
		}, map[string]string{}},
		"CODEX_HOME": {func(t *testing.T, h string) Env {
			touch(t, filepath.Join(h, "ch", "config.toml"))
			touch(t, filepath.Join(h, ".codex", "config.toml")) // not Codex's folder any more
			return envOf(map[string]string{"CODEX_HOME": filepath.Join(h, "ch")})
		}, map[string]string{Codex: "ch"}},
		"CODEX_HOME not a folder": {func(t *testing.T, h string) Env {
			touch(t, filepath.Join(h, "ch"))
			touch(t, filepath.Join(h, ".codex", "auth.json"))
			return envOf(map[string]string{"CODEX_HOME": filepath.Join(h, "ch")})
		}, map[string]string{Codex: ".codex"}},
		"gemini CLI alone": {func(t *testing.T, h string) Env {
			touch(t, filepath.Join(h, ".gemini", "settings.json"))
			touch(t, filepath.Join(h, ".gemini", "GEMINI.md"))
			mkdir(t, filepath.Join(h, ".gemini", "config"))
			return none
		}, map[string]string{}},
		"antigravity mcp_config.json": {func(t *testing.T, h string) Env {
			touch(t, filepath.Join(h, ".gemini", "config", "mcp_config.json"))
			return none
		}, map[string]string{Antigravity: ".gemini"}},
		"antigravity folder": {func(t *testing.T, h string) Env {
			mkdir(t, filepath.Join(h, ".gemini", "antigravity"))
			return none
		}, map[string]string{Antigravity: ".gemini"}},
		"antigravity a file, not a folder": {func(t *testing.T, h string) Env {
			touch(t, filepath.Join(h, ".gemini", "antigravity"))
			return none
		}, map[string]string{}},
		"antigravity IDE user folder": {func(t *testing.T, h string) Env {
			mkdir(t, filepath.Join(h, "roaming", "Antigravity", "User"))
			return envOf(map[string]string{"APPDATA": filepath.Join(h, "roaming")})
		}, map[string]string{Antigravity: ".gemini"}},
		"antigravity IDE folder without User": {func(t *testing.T, h string) Env {
			mkdir(t, filepath.Join(h, "roaming", "Antigravity"))
			return envOf(map[string]string{"APPDATA": filepath.Join(h, "roaming")})
		}, map[string]string{}},
		"hermes LOCALAPPDATA": {func(t *testing.T, h string) Env {
			touch(t, filepath.Join(h, "local", "hermes", "config.yaml"))
			return envOf(map[string]string{"LOCALAPPDATA": filepath.Join(h, "local")})
		}, map[string]string{Hermes: filepath.Join("local", "hermes")}},
		"hermes default local folder": {func(t *testing.T, h string) Env {
			touch(t, filepath.Join(h, "AppData", "Local", "hermes", "config.yaml"))
			return none
		}, map[string]string{Hermes: filepath.Join("AppData", "Local", "hermes")}},
		"hermes without config.yaml": {func(t *testing.T, h string) Env {
			touch(t, filepath.Join(h, "AppData", "Local", "hermes", ".env"))
			return none
		}, map[string]string{}},
		"HERMES_HOME": {func(t *testing.T, h string) Env {
			touch(t, filepath.Join(h, "hh", "config.yaml"))
			return envOf(map[string]string{"HERMES_HOME": filepath.Join(h, "hh")})
		}, map[string]string{Hermes: "hh"}},
		"HERMES_HOME not a folder": {func(t *testing.T, h string) Env {
			touch(t, filepath.Join(h, "hh"))
			touch(t, filepath.Join(h, "AppData", "Local", "hermes", "config.yaml"))
			return envOf(map[string]string{"HERMES_HOME": filepath.Join(h, "hh")})
		}, map[string]string{Hermes: filepath.Join("AppData", "Local", "hermes")}},
		"claude desktop": {func(t *testing.T, h string) Env {
			touch(t, filepath.Join(h, "roaming", "Claude", "claude_desktop_config.json"))
			return envOf(map[string]string{"APPDATA": filepath.Join(h, "roaming")})
		}, map[string]string{ClaudeDesktop: filepath.Join("roaming", "Claude")}},
		"claude desktop folder alone": {func(t *testing.T, h string) Env {
			mkdir(t, filepath.Join(h, "AppData", "Roaming", "Claude"))
			return none
		}, map[string]string{}},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			got := detected(home, tc.setup(t, home))
			if len(got) != len(tc.want) {
				t.Fatalf("detected %v, want %v", got, tc.want)
			}
			for product, rel := range tc.want {
				if got[product] != filepath.Join(home, rel) {
					t.Errorf("%s: folder %q, want %q", product, got[product], filepath.Join(home, rel))
				}
			}
		})
	}
	if Detect("", none) != nil {
		t.Error("no home: something detected")
	}
}

// BV-98 (decision 2026-10-28, macOS, M1a; LECTURA-101 §2): the macOS layout
// moves Hermes Agent to ~/.hermes and Claude Desktop, Antigravity's user
// folder, VS Code and Cline under ~/Library/Application Support; the
// variables are respected as on Windows, and %APPDATA% is not read.
func TestMacOSLayout(t *testing.T) {
	home := filepath.Join(t.TempDir(), "albert")
	lib := filepath.Join(home, "Library", "Application Support")
	env := envOf(map[string]string{"APPDATA": filepath.Join(home, "elsewhere"), "LOCALAPPDATA": filepath.Join(home, "elsewhere")})
	l := MacOSLayout
	for got, want := range map[string]string{
		l.HermesHome(home, env):         filepath.Join(home, ".hermes"),
		l.ClaudeDesktopDir(home, env):   filepath.Join(lib, "Claude"),
		l.AntigravityUserDir(home, env): filepath.Join(lib, "Antigravity", "User"),
		l.VSCodeUserDir(home, env):      filepath.Join(lib, "Code", "User"),
		l.ClineDir(home, env):           filepath.Join(lib, "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings"),
	} {
		if got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
	custom := filepath.Join(home, "hh")
	mkdir(t, custom)
	if got := l.HermesHome(home, envOf(map[string]string{"HERMES_HOME": custom})); got != custom {
		t.Errorf("HERMES_HOME ignored: %q", got)
	}
	touch(t, filepath.Join(home, ".hermes", "config.yaml"))
	touch(t, filepath.Join(lib, "Claude", "claude_desktop_config.json"))
	mkdir(t, filepath.Join(lib, "Antigravity", "User"))
	got := detected(home, envOf(nil))
	if len(got) != 0 {
		t.Errorf("Windows layout detected %v in a macOS home", got)
	}
	mac := map[string]string{}
	for _, p := range l.Detect(home, env) {
		mac[p.Product] = p.ConfigDir
	}
	for product, want := range map[string]string{
		Hermes:        filepath.Join(home, ".hermes"),
		ClaudeDesktop: filepath.Join(lib, "Claude"),
		Antigravity:   filepath.Join(home, ".gemini"),
	} {
		if mac[product] != want {
			t.Errorf("%s = %q, want %q", product, mac[product], want)
		}
	}
	// The package-level functions keep the Windows layout.
	if HermesHome(home, env) != filepath.Join(home, "elsewhere", "hermes") {
		t.Errorf("HermesHome = %q, want the Windows layout", HermesHome(home, env))
	}
}
