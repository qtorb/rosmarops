package integration_test

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// cliRun runs the binary with args in a sandboxed home and returns its exit
// code, standard output and standard error.
func cliRun(t *testing.T, bin, home string, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir = home
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "USERPROFILE=" + home, "APPDATA=" + filepath.Join(home, "appdata"),
		"LOCALAPPDATA=" + filepath.Join(home, "localappdata"), "XDG_CONFIG_HOME=" + filepath.Join(home, "xdg"), "TMPDIR=" + filepath.Join(home, "tmp"),
	}
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		code = exitErr.ExitCode()
	case err != nil:
		t.Fatalf("%v: %v", args, err)
	}
	return code, out.String(), errb.String()
}

// The CLI's surface (BV-49, G4): the commands, subcommands, flags and exit
// codes that stay the same when cobra is replaced by the standard flag
// package. Help texts are not pinned (they move to the catalog).
func TestCLISurface(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("black-box run is implemented for Unix-like hosts")
	}
	bin := filepath.Join(t.TempDir(), "rosmarops")
	build := exec.Command("go", "build", "-o", bin, "./cmd/rosmarops")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	home := t.TempDir()
	for _, d := range []string{"appdata", "localappdata", "xdg", "tmp", filepath.Join("src", "a", ".git")} {
		if err := os.MkdirAll(filepath.Join(home, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	commands := []string{"init", "run", "baseline", "status", "allow", "schedule"}

	for _, c := range []struct {
		args []string
		code int
		out  string // in standard output, when set
		err  string // in standard error, when set
	}{
		// No arguments is a double click, the whole flow (decision
		// 2026-10-27, primer uso con un solo .exe, U1): firstrun_test.go;
		// the help is --help, -h and help.
		{args: []string{"-h"}, code: 0},
		{args: []string{"--help"}, code: 0},
		{args: []string{"help"}, code: 0},
		{args: []string{"--version"}, code: 0, out: "rosmarops version 0.1.0-dev"},
		{args: []string{"-v"}, code: 0, out: "rosmarops version 0.1.0-dev"},
		{args: []string{"nope"}, code: 1, err: "error:"},
		{args: []string{"init", "--bogus"}, code: 1, err: "error:"},
		{args: []string{"init", "--repos-under", filepath.Join(home, "src")}, code: 0},
		{args: []string{"init"}, code: 1, err: "already exists"}, // never overwrites
		{args: []string{"status"}, code: 0},
		{args: []string{"status", "--bogus"}, code: 1, err: "error:"},
		{args: []string{"run", "--lang", "fr"}, code: 1, err: "lang_unsupported"},
		{args: []string{"baseline"}, code: 0},
		{args: []string{"allow"}, code: 0},
		{args: []string{"allow", "list"}, code: 1},
		{args: []string{"allow", "add", "x"}, code: 1},
		{args: []string{"allow", "add", "a", "b"}, code: 1},
		{args: []string{"allow", "remove", "a", "b"}, code: 1},
		{args: []string{"schedule"}, code: 1},
	} {
		code, out, errOut := cliRun(t, bin, home, c.args...)
		if code != c.code {
			t.Errorf("%v: exit %d, want %d\n%s%s", c.args, code, c.code, out, errOut)
		}
		if c.out != "" && !strings.Contains(out, c.out) {
			t.Errorf("%v: stdout %q lacks %q", c.args, out, c.out)
		}
		if c.err != "" && !strings.Contains(errOut, c.err) {
			t.Errorf("%v: stderr %q lacks %q", c.args, errOut, c.err)
		}
		if c.args[0] == "-h" || c.args[0] == "--help" || c.args[0] == "help" {
			for _, cmd := range commands {
				if !strings.Contains(out, cmd) {
					t.Errorf("%v: help lacks the command %s", c.args, cmd)
				}
			}
		}
	}
	// Each command's help, with its flags.
	for cmd, flag := range map[string]string{"init": "--repos-under", "run": "--lang", "baseline": "", "status": "", "schedule": "", "allow": ""} {
		code, out, errOut := cliRun(t, bin, home, cmd, "-h")
		if code != 0 || flag != "" && !strings.Contains(out+errOut, flag) {
			t.Errorf("%s -h: exit %d, flag %s in %q", cmd, code, flag, out+errOut)
		}
	}
	for _, sub := range []string{"add", "remove", "list"} {
		if code, out, errOut := cliRun(t, bin, home, "allow", sub, "-h"); code != 0 || !strings.Contains(out+errOut, sub) {
			t.Errorf("allow %s -h: exit %d", sub, code)
		}
	}
	// The help comes out in the --lang language (English by default), every
	// text from the catalog.
	_, en, _ := cliRun(t, bin, home, "-h")
	_, es, _ := cliRun(t, bin, home, "--lang", "es", "-h")
	if en == es || strings.Contains(en+es, "[cli.") {
		t.Errorf("help en %q\nes %q", en, es)
	}
	// A run after the baseline completes (exit 0); a changed config after it
	// is an integrity failure (exit 2).
	code, out, errOut := cliRun(t, bin, home, "run")
	if code != 0 {
		t.Errorf("run: exit %d\n%s%s", code, out, errOut)
	}
	// v3 §9.6: the progress lines, never a repository's name or path.
	for _, want := range []string{"Reading your agents' configuration…", "Checking repositories: 0/1", "Checking repositories: 1/1", "Writing the report…"} {
		if !strings.Contains(out, want) {
			t.Errorf("run: no progress line %q in %q", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "Checking repositories") && strings.Contains(line, filepath.Join(home, "src")) {
			t.Errorf("progress names a repository: %q", line)
		}
	}
	cfg := filepath.Join(home, ".rosmarops", "config.json")
	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, errOut := cliRun(t, bin, home, "status"); code != 2 || !strings.Contains(errOut, "ABORT") {
		t.Errorf("status after a config change: exit %d, %q", code, errOut)
	}
	// run asks first; without a console it is not asked, it is treated as a
	// rejection and invariant 8 applies unchanged (exit 2, minimal report).
	if code, out, errOut := cliRun(t, bin, home, "run"); code != 2 || !strings.Contains(errOut, "ABORT") ||
		!strings.Contains(out, "No one confirmed these changes") || strings.Contains(out, "Did you make these changes yourself?") {
		t.Errorf("run after a config change: exit %d\n%s%s", code, out, errOut)
	}
}
