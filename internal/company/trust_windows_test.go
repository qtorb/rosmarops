//go:build windows

package company

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// icacls runs icacls on path. Setting the owner to Administrators needs an
// elevated test, which the Windows CI runner is: there a failure is fatal,
// elsewhere the test is skipped.
func icacls(t *testing.T, path string, args ...string) {
	t.Helper()
	out, err := exec.Command("icacls", append([]string{path}, args...)...).CombinedOutput()
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Fatalf("icacls %s %s: %v\n%s", path, strings.Join(args, " "), err, out)
		}
		t.Skipf("icacls needs an elevated test: %v", err)
	}
}

// companyFolder lays out a company folder in the order the install script
// does: the folder first gets its permissions (owner Administrators, full
// control for Administrators and SYSTEM, read for Users, nothing inherited
// from above), then the two files are written in it and inherit them, and the
// owner is set on everything.
func companyFolder(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "RosmarOps")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	icacls(t, dir, "/inheritance:r", "/grant:r", "*S-1-5-32-544:(OI)(CI)F", "*S-1-5-18:(OI)(CI)F", "*S-1-5-32-545:(OI)(CI)RX")
	for name, content := range map[string]string{
		ConfigName: `{"schema_version":1,"summary":{"enabled":true}}`,
		ListName:   sha + "  0.16.0\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Each file keeps only what it inherits from the folder, as after the
	// install script: a file created by the test's own account may also get an
	// entry of its own for that account.
	for _, name := range []string{ConfigName, ListName} {
		icacls(t, filepath.Join(dir, name), "/reset")
	}
	icacls(t, dir, "/setowner", "*S-1-5-32-544", "/T")
	return dir
}

func TestCompanyTrustWindows(t *testing.T) {
	dir := companyFolder(t)
	if l := LoadConfig(dir); l.State != Trusted || !l.SummaryEnabled() {
		t.Fatalf("admin-only folder: %+v (%v)", l, checkTrusted(filepath.Join(dir, ConfigName)))
	}
	if _, ok := LoadList(dir); !ok {
		t.Fatalf("admin-only list not read: %v", checkTrusted(filepath.Join(dir, ListName)))
	}

	cases := map[string]func(dir string){
		"Users may write the file": func(dir string) {
			icacls(t, filepath.Join(dir, ConfigName), "/grant", "*S-1-5-32-545:(W)")
		},
		"Users may add files to the folder": func(dir string) {
			icacls(t, dir, "/grant", "*S-1-5-32-545:(AD)")
		},
		"CREATOR OWNER on the folder": func(dir string) {
			icacls(t, dir, "/grant", "*S-1-3-0:(OI)(CI)(IO)F")
		},
		"Users may change permissions": func(dir string) {
			icacls(t, filepath.Join(dir, ConfigName), "/grant", "*S-1-5-32-545:(WDAC)")
		},
		"owned by Users": func(dir string) {
			icacls(t, filepath.Join(dir, ConfigName), "/setowner", "*S-1-5-32-545")
		},
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			dir := companyFolder(t)
			spoil(dir)
			if l := LoadConfig(dir); l.State != Untrusted || l.SummaryEnabled() {
				t.Errorf("config: %+v", l)
			}
		})
	}

	t.Run("a junction", func(t *testing.T) {
		real := companyFolder(t)
		link := filepath.Join(t.TempDir(), "RosmarOps")
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, real).CombinedOutput(); err != nil {
			t.Fatalf("mklink /J: %v\n%s", err, out)
		}
		if l := LoadConfig(link); l.State != Untrusted {
			t.Errorf("through a junction: %+v", l)
		}
	})
}

func TestMachineDirWindows(t *testing.T) {
	got := machineDir()
	if got == "" || !strings.EqualFold(filepath.Base(got), "RosmarOps") {
		t.Fatalf("machineDir = %q", got)
	}
	// Resolved from the Windows API, not from %ProgramData%.
	t.Setenv("ProgramData", `C:\elsewhere`)
	if again := machineDir(); again != got {
		t.Errorf("machineDir follows %%ProgramData%%: %q", again)
	}
}
