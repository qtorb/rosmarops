package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// integrityEnv builds the binary and an isolated home (decision
// 2026-10-26, integridad y revisión semanal, §3). It returns the binary, the
// working directory and the home.
func integrityEnv(t *testing.T) (bin, work, home string, env []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("black-box run is implemented for Unix-like hosts")
	}
	bin = filepath.Join(t.TempDir(), "rosmarops")
	build := exec.Command("go", "build", "-o", bin, "./cmd/rosmarops")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	root := t.TempDir()
	home, work = filepath.Join(root, "home"), filepath.Join(root, "work")
	env = []string{"PATH=" + os.Getenv("PATH")}
	for name, dir := range map[string]string{
		"HOME": home, "USERPROFILE": home, "APPDATA": filepath.Join(root, "appdata"), "LOCALAPPDATA": filepath.Join(root, "localappdata"),
		"XDG_CONFIG_HOME": filepath.Join(root, "xdg"), "XDG_CACHE_HOME": filepath.Join(root, "xdgc"), "TMPDIR": filepath.Join(root, "tmp"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		env = append(env, name+"="+dir)
	}
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	return bin, work, home, env
}

// rosmarops runs the binary without a console (standard input is not a
// terminal) and returns its exit code.
func rosmarops(t *testing.T, bin, work string, env []string, args ...string) int {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Dir, cmd.Env = work, env
	out, err := cmd.CombinedOutput()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &exitErr):
		t.Logf("%v: exit %d\n%s", args, exitErr.ExitCode(), out)
		return exitErr.ExitCode()
	}
	t.Fatalf("%v: %v\n%s", args, err, out)
	return -1
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

type integrityState struct {
	LastRun   string `json:"last_run"`
	Integrity struct {
		Binary string `json:"vigil_binary_sha256"`
		Config string `json:"config_sha256"`
	} `json:"integrity"`
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(b, v)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func catalogText(t *testing.T, lang, key string) string {
	t.Helper()
	m := map[string]string{}
	readJSON(t, filepath.Join("..", "..", "internal", "catalog", "messages", lang+".json"), &m)
	if m[key] == "" {
		t.Fatalf("no %s in %s", key, lang)
	}
	return html.EscapeString(m[key])
}

// J1: init records the binary's and the configuration's references and the
// accepted copy; the first run then needs no baseline.
func TestInitRecordsReferences(t *testing.T) {
	bin, work, home, env := integrityEnv(t)
	if code := rosmarops(t, bin, work, env, "init"); code != 0 {
		t.Fatalf("init exit %d", code)
	}
	dir := filepath.Join(home, ".rosmarops")
	var st integrityState
	readJSON(t, filepath.Join(dir, "state.json"), &st)
	exe, _ := os.ReadFile(bin)
	cfg, _ := os.ReadFile(filepath.Join(dir, "config.json"))
	if st.Integrity.Binary != sha256Hex(exe) || st.Integrity.Config != sha256Hex(cfg) {
		t.Errorf("references %+v", st.Integrity)
	}
	copyRaw, err := os.ReadFile(filepath.Join(dir, "reports", "config.accepted.json"))
	if err != nil || string(copyRaw) != string(cfg) {
		t.Errorf("accepted copy: %v", err)
	}
	if code := rosmarops(t, bin, work, env, "run"); code == 2 {
		t.Error("the first run after init aborted")
	}
}

// J2–J3: an unconfirmed change (here the configuration, with no console)
// aborts with a red report.html next to the .md and the .json; a scheduled
// review's says it must be run by hand.
func TestAbortWritesRedHTML(t *testing.T) {
	for _, unattended := range []bool{false, true} {
		bin, work, home, env := integrityEnv(t)
		rosmarops(t, bin, work, env, "init")
		cfgPath := filepath.Join(home, ".rosmarops", "config.json")
		cfg, _ := os.ReadFile(cfgPath)
		if err := os.WriteFile(cfgPath, []byte(strings.Replace(string(cfg), `"repo_roots": []`, `"repo_roots": ["/elsewhere"]`, 1)), 0o600); err != nil {
			t.Fatal(err)
		}
		args := []string{"run"}
		if unattended {
			args = append(args, "--unattended")
		}
		if code := rosmarops(t, bin, work, env, args...); code != 2 {
			t.Fatalf("unattended %v: exit %d, want 2", unattended, code)
		}
		reports := filepath.Join(home, ".rosmarops", "reports")
		page, err := os.ReadFile(filepath.Join(reports, "report.html"))
		if err != nil {
			t.Fatalf("unattended %v: no report.html: %v", unattended, err)
		}
		for _, key := range []string{"html.status.red", "html.config.rejected"} {
			if !strings.Contains(string(page), catalogText(t, "en", key)) {
				t.Errorf("unattended %v: no %s", unattended, key)
			}
		}
		if pending := strings.Contains(string(page), catalogText(t, "en", "html.unattended.pending")); pending != unattended {
			t.Errorf("unattended %v: html.unattended.pending %v", unattended, pending)
		}
		mds, _ := filepath.Glob(filepath.Join(reports, "*.md"))
		jsons, _ := filepath.Glob(filepath.Join(reports, "*.json"))
		if len(mds) == 0 || len(jsons) == 0 {
			t.Errorf("unattended %v: .md %v, .json %v", unattended, mds, jsons)
		}
		var st integrityState
		readJSON(t, filepath.Join(home, ".rosmarops", "state.json"), &st)
		if st.Integrity.Config != sha256Hex(cfg) {
			t.Errorf("unattended %v: the rejected configuration was accepted", unattended)
		}
	}
}

// J2: a binary other than the recorded one, unattended: red, no question.
func TestBinaryChangedUnattended(t *testing.T) {
	bin, work, home, env := integrityEnv(t)
	rosmarops(t, bin, work, env, "init")
	stPath := filepath.Join(home, ".rosmarops", "state.json")
	var raw map[string]any
	readJSON(t, stPath, &raw)
	raw["integrity"].(map[string]any)["vigil_binary_sha256"] = strings.Repeat("a", 64)
	b, _ := json.Marshal(raw)
	if err := os.WriteFile(stPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := rosmarops(t, bin, work, env, "run", "--unattended"); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	page, err := os.ReadFile(filepath.Join(home, ".rosmarops", "reports", "report.html"))
	if err != nil || !strings.Contains(string(page), catalogText(t, "en", "html.unattended.pending")) {
		t.Errorf("red report: %v", err)
	}
}

// J1: a state that already ran, with its references at zero, is a change:
// unattended, red; the references are not fixed.
func TestStateLostUnattended(t *testing.T) {
	bin, work, home, env := integrityEnv(t)
	rosmarops(t, bin, work, env, "init")
	rosmarops(t, bin, work, env, "run")
	stPath := filepath.Join(home, ".rosmarops", "state.json")
	var raw map[string]any
	readJSON(t, stPath, &raw)
	zero := strings.Repeat("0", 64)
	raw["integrity"] = map[string]any{"vigil_binary_sha256": zero, "config_sha256": zero}
	b, _ := json.Marshal(raw)
	if err := os.WriteFile(stPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	if code := rosmarops(t, bin, work, env, "run", "--unattended"); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	var st integrityState
	readJSON(t, stPath, &st)
	if st.Integrity.Binary != zero || st.Integrity.Config != zero {
		t.Errorf("references fixed in silence: %+v", st.Integrity)
	}
}

// J1: a state deleted, or made unreadable, between two runs: without a
// console, or unattended, the next one aborts red and writes no new
// reference.
func TestStateDeletedOrUnreadable(t *testing.T) {
	for _, c := range []struct{ name, content string }{{"deleted", ""}, {"unreadable", "{not json"}} {
		for _, args := range [][]string{{"run"}, {"run", "--unattended"}} {
			bin, work, home, env := integrityEnv(t)
			rosmarops(t, bin, work, env, "init")
			rosmarops(t, bin, work, env, "run")
			stPath := filepath.Join(home, ".rosmarops", "state.json")
			var err error
			if c.content == "" {
				err = os.Remove(stPath)
			} else {
				err = os.WriteFile(stPath, []byte(c.content), 0o600)
			}
			if err != nil {
				t.Fatal(err)
			}
			if code := rosmarops(t, bin, work, env, args...); code != 2 {
				t.Errorf("%s %v: exit %d, want 2", c.name, args, code)
			}
			raw, err := os.ReadFile(stPath)
			if c.content == "" && err == nil || c.content != "" && string(raw) != c.content {
				t.Errorf("%s %v: the state was rewritten", c.name, args)
			}
			page, err := os.ReadFile(filepath.Join(home, ".rosmarops", "reports", "report.html"))
			if err != nil || !strings.Contains(string(page), catalogText(t, "en", "html.status.red")) {
				t.Errorf("%s %v: no red report.html: %v", c.name, args, err)
			}
		}
	}
}

// J3: a scheduled review reads only ~/.rosmarops/config.json — a broken
// rosmarops.config.json in its working directory is not read — and writes
// its log to <reports>/last-scheduled-run-log.txt.
func TestUnattendedConfigAndLog(t *testing.T) {
	bin, work, home, env := integrityEnv(t)
	rosmarops(t, bin, work, env, "init")
	if err := os.WriteFile(filepath.Join(work, "rosmarops.config.json"), []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := rosmarops(t, bin, work, env, "run"); code != 1 {
		t.Fatalf("by hand: exit %d, want 1 (the local configuration is read)", code)
	}
	if code := rosmarops(t, bin, work, env, "run", "--unattended"); code == 1 || code == 2 {
		t.Fatalf("unattended: exit %d", code)
	}
	reports := filepath.Join(home, ".rosmarops", "reports")
	if _, err := os.Stat(filepath.Join(reports, "report.html")); err != nil {
		t.Fatal(err)
	}
	log, err := os.ReadFile(filepath.Join(reports, "last-scheduled-run-log.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(log), "rosmarops.config.json") {
		t.Errorf("the log names the local configuration:\n%s", log)
	}
}
