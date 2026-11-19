package linux_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/qtorb/rosmarops/adapters/linux"
	"github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/platform"
)

// cli-config.json is found where Cursor's CLI looks for it: CURSOR_CONFIG_DIR,
// then XDG_CONFIG_HOME/cursor, then ~/.cursor; a relative value is ignored.
func TestCursorCLIConfigDir(t *testing.T) {
	// Absolute paths on every system (a path from / is not absolute on
	// Windows).
	root := t.TempDir()
	home := filepath.Join(root, "home", "ana")
	conf := filepath.Join(home, ".config")
	opt := filepath.Join(root, "opt", "cur")
	for name, c := range map[string]struct {
		env  map[string]string
		want string
	}{
		"none":              {nil, filepath.Join(home, ".cursor")},
		"xdg":               {map[string]string{"XDG_CONFIG_HOME": conf}, filepath.Join(conf, "cursor")},
		"cursor wins":       {map[string]string{"XDG_CONFIG_HOME": conf, "CURSOR_CONFIG_DIR": opt}, opt},
		"relative ignored":  {map[string]string{"XDG_CONFIG_HOME": "conf", "CURSOR_CONFIG_DIR": "cur"}, filepath.Join(home, ".cursor")},
		"relative xdg only": {map[string]string{"XDG_CONFIG_HOME": "conf"}, filepath.Join(home, ".cursor")},
	} {
		got := windows.CursorCLIConfigDir(home, func(k string) string { return c.env[k] })
		if got != c.want {
			t.Errorf("%s: %q, want %q", name, got, c.want)
		}
	}
}

// On Linux the scan reads /etc/cursor/hooks.json as the enterprise hooks file
// and cli-config.json from XDG_CONFIG_HOME or CURSOR_CONFIG_DIR.
func TestLinuxCursorEnterpriseHooksAndCLIConfig(t *testing.T) {
	for _, envName := range []string{"XDG_CONFIG_HOME", "CURSOR_CONFIG_DIR"} {
		t.Run(envName, func(t *testing.T) {
			root := t.TempDir()
			home := filepath.Join(root, "home")
			cfgDir := filepath.Join(root, "conf")
			t.Setenv("HOME", home)
			t.Setenv("XDG_CONFIG_HOME", "")
			t.Setenv("CURSOR_CONFIG_DIR", "")
			cliDir := cfgDir
			if envName == "XDG_CONFIG_HOME" {
				cliDir = filepath.Join(cfgDir, "cursor")
			}
			t.Setenv(envName, cfgDir)
			hooks := filepath.Join(root, "etc", "cursor", "hooks.json")
			for path, body := range map[string]string{
				filepath.Join(home, ".cursor", "mcp.json"):        `{}`,
				filepath.Join(cliDir, "cli-config.json"):          `{"permissions":{"allow":["Shell(ls)"]}}`,
				filepath.Join(home, ".cursor", "cli-config.json"): `{"permissions":{"allow":["Shell(rm)"]}}`,
				hooks: `{"version":1,"hooks":{}}`,
			} {
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			scan, err := linux.NewWith(filepath.Join(root, "etc", "claude-code"), hooks).ListCursor(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			var cli, enterprise *platform.CursorFile
			for i, f := range scan.Files {
				switch {
				case f.Kind == platform.CursorCLI && f.Scope == platform.CursorScopeUser:
					cli = &scan.Files[i]
				case f.Kind == platform.CursorHooks && f.Scope == platform.CursorScopeEnterprise:
					enterprise = &scan.Files[i]
				}
			}
			if cli == nil || cli.Path != filepath.Join(cliDir, "cli-config.json") || cli.Status != platform.MCPSourceRead {
				t.Errorf("cli-config.json: %+v", cli)
			}
			if enterprise == nil || enterprise.Path != hooks || enterprise.Status != platform.MCPSourceRead {
				t.Errorf("enterprise hooks: %+v", enterprise)
			}
		})
	}
}
