package windows_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/declared"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
)

func grokBotHome(t *testing.T, settings string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".grokbot"), 0o700); err != nil {
		t.Fatal(err)
	}
	if settings != "" {
		if err := os.WriteFile(filepath.Join(home, ".grokbot", "settings.json"), []byte(settings), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

// CTO-57 §4, BV-248 B4: B1 is the least controlled of every local tool
// permission (always > ask > never), ask without one; B2 off when any account
// has Auto-review off.
func TestReadGrokBot_Values(t *testing.T) {
	cases := []struct {
		name, settings string
		b1, b2         string
		accounts       int
	}{
		{"no key", `{"version":1}`, declared.LocalAsk, "", 0},
		{"machine always under an ask ceiling", `{"localToolPermissionByMachineId":{"m1":"never","m2":"always"},"localToolPermissionCeiling":"ask"}`, declared.LocalAlways, "", 0},
		{"account never", `{"accountScopes":{"a":{"localToolPermission":"never","autoReviewInstructions":{"isEnabled":true}}}}`, declared.LocalNever, declared.On, 1},
		{"one account off", `{"accountScopes":{"a":{"autoReviewInstructions":{"isEnabled":true}},"b":{"autoReviewInstructions":{"isEnabled":false}}}}`, declared.LocalAsk, declared.Off, 2},
		{"isEnabled absent is on", `{"accountScopes":{"a":{}}}`, declared.LocalAsk, declared.On, 1},
	}
	for _, c := range cases {
		scan := windows.ReadGrokBot(grokBotHome(t, c.settings), "")
		if scan.Status != platform.MCPSourceRead || scan.Accounts != c.accounts {
			t.Errorf("%s: %+v", c.name, scan)
			continue
		}
		f := graph.GrokBotFacts(scan)
		if f.Local != c.b1 || f.AutoReview != c.b2 {
			t.Errorf("%s: B1 %q B2 %q, want %q %q", c.name, f.Local, f.AutoReview, c.b1, c.b2)
		}
	}
}

// BV-248 B5: an unknown value, a file that is not JSON, a link and a file over
// 2 MB are failed sources (the questions are asked as declared).
func TestReadGrokBot_Failed(t *testing.T) {
	for name, settings := range map[string]string{
		"unknown value": `{"localToolPermission":"sometimes"}`,
		"not json":      `{"localToolPermission":`,
		"too large":     `{"pad":"` + strings.Repeat("x", windows.MaxConfigBytes) + `"}`,
	} {
		scan := windows.ReadGrokBot(grokBotHome(t, settings), "")
		if scan.Status != platform.MCPSourceFailed || len(scan.LocalPermissions) != 0 {
			t.Errorf("%s: %+v", name, scan.Status)
		}
		if f := graph.GrokBotFacts(scan); f.LocalRead || f.AutoReviewRead {
			t.Errorf("%s: measured from a failed source", name)
		}
	}
	home := grokBotHome(t, "")
	target := filepath.Join(t.TempDir(), "elsewhere.json")
	if err := os.WriteFile(target, []byte(`{"localToolPermission":"never"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(home, ".grokbot", "settings.json")); err != nil {
		t.Skipf("no symlink: %v", err)
	}
	if scan := windows.ReadGrokBot(home, ""); scan.Status != platform.MCPSourceFailed {
		t.Errorf("link: %+v", scan.Status)
	}
}

// BV-248 B1: presence is the app folder; nothing in it is opened.
func TestReadGrokBot_Presence(t *testing.T) {
	local := t.TempDir()
	if scan := windows.ReadGrokBot(t.TempDir(), local); scan.Present || scan.Status != platform.MCPSourceMissing {
		t.Errorf("no app folder: %+v", scan)
	}
	if err := os.MkdirAll(filepath.Join(local, "Programs", "Grok Bot"), 0o700); err != nil {
		t.Fatal(err)
	}
	if scan := windows.ReadGrokBot(t.TempDir(), local); !scan.Present {
		t.Errorf("app folder not detected")
	}
}

// BV-274: on macOS the app is Grok Bot.app in /Applications or
// ~/Applications.
func TestReadGrokBotMac_Presence(t *testing.T) {
	home, apps := t.TempDir(), t.TempDir()
	if scan := windows.ReadGrokBotMac(home, apps); scan.Present || scan.Status != platform.MCPSourceMissing {
		t.Errorf("no app: %+v", scan)
	}
	if err := os.MkdirAll(filepath.Join(apps, "Grok Bot.app", "Contents"), 0o700); err != nil {
		t.Fatal(err)
	}
	if scan := windows.ReadGrokBotMac(home, apps); !scan.Present {
		t.Error("/Applications/Grok Bot.app not detected")
	}
	user := t.TempDir()
	if err := os.MkdirAll(filepath.Join(user, "Applications", "Grok Bot.app"), 0o700); err != nil {
		t.Fatal(err)
	}
	if scan := windows.ReadGrokBotMac(user, t.TempDir()); !scan.Present {
		t.Error("~/Applications/Grok Bot.app not detected")
	}
	// A file with the app's name is not the app.
	odd := t.TempDir()
	if err := os.WriteFile(filepath.Join(odd, "Grok Bot.app"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if scan := windows.ReadGrokBotMac(t.TempDir(), odd); scan.Present {
		t.Error("a file named Grok Bot.app counted as the app")
	}
}
