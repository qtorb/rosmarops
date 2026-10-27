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
