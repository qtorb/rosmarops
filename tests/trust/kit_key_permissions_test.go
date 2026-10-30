package trust_test

// BV-118 (point 6; CTO-10 point 10): on macOS and Linux the kit key file is
// ~/.rosmarops/kit.key with mode 0600, in a 0700 folder, as the real binary
// writes it. The helpers drive the kit commands' console.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// kitPhrase is the own phrase the black-box tests type.
const kitPhrase = "kit phrase for the trust tests"

// codeLine finds the recovery code in kit create's output.
var codeLine = regexp.MustCompile(`Your recovery code is: ([0-9A-Z]{4}(?:-[0-9A-Z]{4}){5})\.`)

// withHome is env with HOME and USERPROFILE set to home.
func withHome(env []string, home string) []string {
	var out []string
	for _, kv := range env {
		if strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, "USERPROFILE=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "HOME="+home, "USERPROFILE="+home)
}

// kitRun runs rosmarops args with home as the user's folder. Each output line
// goes to answer; a line ending in ":" is a question, answered with what
// answer returns, or with the end of the input when it returns false.
func (s *sandbox) kitRun(t *testing.T, home string, args []string, answer func(line string) (string, bool)) (string, error) {
	t.Helper()
	cmd := exec.Command(s.binary, args...)
	cmd.Dir = s.work
	cmd.Env = withHome(s.env, home)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		line := sc.Text()
		out.WriteString(line + "\n")
		a, ok := answer(line)
		if !strings.HasSuffix(line, ":") {
			continue
		}
		if !ok {
			_ = stdin.Close()
			continue
		}
		if _, err := io.WriteString(stdin, a+"\n"); err != nil {
			t.Fatal(err)
		}
	}
	_ = stdin.Close()
	err = cmd.Wait()
	return out.String() + stderr.String(), err
}

// initHome runs rosmarops init with home as the user's folder.
func (s *sandbox) initHome(t *testing.T, home string) {
	t.Helper()
	mustMkdir(t, home)
	if out, err := s.kitRun(t, home, []string{"init"}, func(string) (string, bool) { return "", false }); err != nil {
		t.Fatalf("init: %v\n%s", err, out)
	}
}

// kitCreate runs kit create with kitPhrase and returns the recovery code
// (without hyphens) and the exported kit file's path.
func (s *sandbox) kitCreate(t *testing.T, home string) (code, file string) {
	t.Helper()
	out, err := s.kitRun(t, home, []string{"kit", "create"}, func(line string) (string, bool) {
		if m := codeLine.FindStringSubmatch(line); m != nil {
			code = strings.ReplaceAll(m[1], "-", "")
		}
		switch {
		case strings.HasPrefix(line, "Choose a secret phrase"), line == "Type it again:":
			return kitPhrase, true
		case strings.HasPrefix(line, "To check you saved it") && code != "":
			return code[len(code)-4:], true
		}
		return "", false
	})
	if err != nil || code == "" {
		t.Fatalf("kit create: %v\n%s", err, out)
	}
	m := regexp.MustCompile(`Your key file is saved in (\S+rosmarops-kit-[0-9a-f]{8}\.json)\.`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("kit create: no saved path\n%s", out)
	}
	return code, m[1]
}

// storedKey is the base64 key in the kit key file under home (Unix: the
// record is stored as is).
func storedKey(t *testing.T, home string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(home, ".rosmarops", "kit.key"))
	if err != nil {
		t.Fatal(err)
	}
	var rec struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &rec); err != nil || rec.Key == "" {
		t.Fatalf("kit.key: %v", err)
	}
	return rec.Key
}

func TestKitKeyFileModes(t *testing.T) {
	s := newSandbox(t)
	s.initHome(t, s.home)
	dir := filepath.Join(s.home, ".rosmarops")
	// A wider folder is narrowed when the key is written.
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s.kitCreate(t, s.home)
	for path, want := range map[string]os.FileMode{filepath.Join(dir, "kit.key"): 0o600, dir: 0o700} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s: mode %o, want %o", path, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "kit.dpapi")); !os.IsNotExist(err) {
		t.Errorf("kit.dpapi outside Windows: %v", err)
	}
}
