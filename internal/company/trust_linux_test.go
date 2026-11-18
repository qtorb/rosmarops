//go:build linux

package company

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// trustedDir is a company folder owned by the test's user, standing in for
// root, with the walk to / stopping at its parent.
func trustedDir(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	dir := filepath.Join(base, "rosmarops")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(base, 0o755); err != nil {
		t.Fatal(err)
	}
	prevUID, prevStop := testOwnerUID, testStop
	t.Cleanup(func() { testOwnerUID, testStop = prevUID, prevStop })
	testOwnerUID, testStop = strconv.Itoa(os.Getuid()), base
	return dir
}

func write(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

const companyJSON = `{"schema_version":1,"summary":{"enabled":true}}`

func TestCompanyTrustLinux(t *testing.T) {
	dir := trustedDir(t)
	cfg := filepath.Join(dir, ConfigName)
	write(t, cfg, companyJSON, 0o644)
	write(t, filepath.Join(dir, ListName), sha+"  0.16.0\n", 0o644)
	if l := LoadConfig(dir); l.State != Trusted || !l.SummaryEnabled() || l.SHA256 == "" {
		t.Fatalf("trusted: %+v", l)
	}
	if list, ok := LoadList(dir); !ok || !Lists(list, sha, "0.16.0") {
		t.Fatalf("trusted list: %v %v", list, ok)
	}

	// Each way the file stops being trusted: then it is not read at all.
	cases := map[string]func(){
		"file writable by others": func() { write(t, cfg, companyJSON, 0o666) },
		"file writable by group":  func() { write(t, cfg, companyJSON, 0o664) },
		"folder writable by group": func() {
			if err := os.Chmod(dir, 0o775); err != nil {
				t.Fatal(err)
			}
		},
		"parent writable by others": func() {
			if err := os.Chmod(filepath.Dir(dir), 0o757); err != nil {
				t.Fatal(err)
			}
		},
		"owned by another user": func() { testOwnerUID = strconv.Itoa(os.Getuid() + 1) },
		"a link": func() {
			real := filepath.Join(dir, "real.json")
			write(t, real, companyJSON, 0o644)
			if err := os.Remove(cfg); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, cfg); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			dir = trustedDir(t)
			cfg = filepath.Join(dir, ConfigName)
			write(t, cfg, companyJSON, 0o644)
			write(t, filepath.Join(dir, ListName), sha+"  0.16.0\n", 0o644)
			spoil()
			if l := LoadConfig(dir); l.State != Untrusted || l.SummaryEnabled() || l.SHA256 != "" {
				t.Errorf("config: %+v", l)
			}
		})
	}

	t.Run("list not trusted", func(t *testing.T) {
		dir := trustedDir(t)
		write(t, filepath.Join(dir, ListName), sha+"  0.16.0\n", 0o666)
		if _, ok := LoadList(dir); ok {
			t.Error("a list writable by others was read")
		}
	})
	t.Run("invalid", func(t *testing.T) {
		dir := trustedDir(t)
		write(t, filepath.Join(dir, ConfigName), `{"schema_version":1,"summary":{"enabled":true},"allow_list":{}}`, 0o644)
		if l := LoadConfig(dir); l.State != Invalid || l.SummaryEnabled() {
			t.Errorf("invalid: %+v", l)
		}
	})
}

// The real walk goes up to /: the test's temporary folder, under a
// world-writable /tmp, is never trusted without the test stop.
func TestCompanyTrustLinuxWalksToRoot(t *testing.T) {
	dir := trustedDir(t)
	write(t, filepath.Join(dir, ConfigName), companyJSON, 0o644)
	testStop = ""
	if l := LoadConfig(dir); l.State != Untrusted {
		t.Errorf("a folder under %s was trusted: %+v", os.TempDir(), l)
	}
}

func TestMachineDirLinux(t *testing.T) {
	if got := machineDir(); got != "/etc/rosmarops" {
		t.Errorf("machineDir = %q", got)
	}
}
