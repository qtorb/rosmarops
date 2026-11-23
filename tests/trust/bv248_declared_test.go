package trust_test

// BV-248 trust tests (CTO-56 §5.4, CTO-57; invariants 2 and 3):
//   - the answers to the questions are written only to ~/.rosmarops/state.json,
//     and the report a review rebuilds with them is written only to the
//     reports directory;
//   - a scheduled review, session-check and statusline never ask;
//   - Grok Bot's reader opens only ~/.grokbot/settings.json (strace), and no
//     token, rule text, account name or machine id of that file reaches the
//     report, the log or the state.

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/report"
)

// runInPTY runs the binary with args with a terminal as its standard input and
// output, types input, and returns what it printed.
func runInPTY(t *testing.T, s *sandbox, argv []string, input string) string {
	t.Helper()
	master, slave, err := openPTY()
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	defer master.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = s.work
	cmd.Env = s.env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	if err := cmd.Start(); err != nil {
		slave.Close()
		t.Fatal(err)
	}
	slave.Close()
	if _, err := master.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, master)
		close(done)
	}()
	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("%v: %v\n%s", argv, err, buf.String())
		}
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
	return buf.String()
}

// grokBotEntry is the first line the console prints before Grok Bot's
// questions (decl.entry), in English.
func grokBotEntry(t *testing.T) string {
	t.Helper()
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	full := cat.Text("decl.entry", map[string]string{"agent": "Grok Bot", "n": "5"})
	return full[:40]
}

// A review from a console asks Grok Bot's questions; the answers end up in
// state.json and in the rebuilt report, and nothing outside ~/.rosmarops
// changes. The answer codes appear in no file but state.json and the
// reports.
func TestDeclaredAnswersOnlyInState(t *testing.T) {
	s := newSandbox(t)
	s.runAll(t, nil)
	rosmarops := filepath.Join(s.home, ".rosmarops")
	state := filepath.Join(rosmarops, "state.json")
	reports := report.DefaultReportDirIn(s.home)

	before := snapshot(t, s.root)
	// Cursor: C1 Auto-review (with Sandbox), C3 Workspace, C4 yes (the
	// sandbox's permissions.json has a terminalAllowlist, so C2 is not asked);
	// Grok Bot: B0 yes, B1 Always allow, B2 on, B3 none, B4 no; then «no» to
	// whatever the review asks after.
	out := runInPTY(t, s, []string{s.binary, "run", "--lang", "en"}, "4\n1\n1\n1\n2\n1\n3\n2\n"+strings.Repeat("n\n", 8))
	after := snapshot(t, s.root)
	if !strings.Contains(out, grokBotEntry(t)) {
		t.Fatalf("the questions were not asked:\n%s", out)
	}
	for _, change := range changedPaths(before, after) {
		_, path, _ := strings.Cut(change, ": ")
		if !pathUnder(path, rosmarops) && !pathUnder(path, reports) {
			t.Errorf("answering wrote outside ~/.rosmarops and the reports folder: %s", change)
		}
	}
	raw, err := os.ReadFile(state)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"b0_uses": "yes"`) || !strings.Contains(string(raw), `"c1_run_mode"`) {
		t.Fatalf("state.json lacks the answers:\n%s\nOUTPUT:\n%s", raw, out)
	}
	v2, err := os.ReadFile(filepath.Join(reports, "data", "report.v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(v2), `"grok-bot"`) && !strings.Contains(string(v2), "Grok Bot") {
		t.Errorf("the rebuilt report has no Grok Bot")
	}
	if !strings.Contains(string(v2), `"b0_uses": "yes"`) {
		t.Errorf("the rebuilt report lacks the declared block")
	}
	_ = filepath.Walk(s.root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || p == state || pathUnder(p, reports) || info.Size() > 4<<20 {
			return nil
		}
		if b, e := os.ReadFile(p); e == nil && bytes.Contains(b, []byte(`"b1_local_execution"`)) {
			t.Errorf("answers written to %s", p)
		}
		return nil
	})
}

// A scheduled review, session-check and statusline never ask, even with a
// terminal: no question printed, and state.json keeps no answer.
func TestDeclaredNeverAskedUnattended(t *testing.T) {
	s := newSandbox(t)
	s.runAll(t, nil)
	entry := grokBotEntry(t)
	for _, args := range [][]string{{"run", "--unattended"}, {"session-check"}, {"statusline"}} {
		out := runInPTY(t, s, append([]string{s.binary}, args...), "1\n2\n1\n3\n2\n")
		if strings.Contains(out, entry) {
			t.Errorf("%v asked the questions:\n%s", args, out)
		}
	}
	raw, err := os.ReadFile(filepath.Join(s.home, ".rosmarops", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), `"declared"`) {
		t.Errorf("state.json has answers without a review from a console:\n%s", raw)
	}
}

// grokBotSentinels are values of a settings.json that must never leave the
// reader: a token, a rule text, an MCP instruction, an account name and a
// machine id.
var grokBotSentinels = []string{
	"tok_SENTINEL_hostToken_9f3e", "RULE_SENTINEL_never_send_payroll", "MCP_SENTINEL_instruction_text",
	"acct_SENTINEL_albert_account", "machine_SENTINEL_0c4a",
}

func writeGrokBot(t *testing.T, s *sandbox) {
	t.Helper()
	dir := filepath.Join(s.home, ".grokbot")
	mustMkdir(t, dir)
	mustWrite(t, filepath.Join(dir, "settings.json"), `{
  "version": 1,
  "hostToken": "`+grokBotSentinels[0]+`",
  "mcpCustomInstructions": "`+grokBotSentinels[2]+`",
  "localToolPermissionByMachineId": {"`+grokBotSentinels[4]+`": "always"},
  "accountScopes": {
    "`+grokBotSentinels[3]+`": {
      "localToolPermission": "ask",
      "autoReviewInstructions": {"isEnabled": false, "allowInstructions": ["`+grokBotSentinels[1]+`"], "blockInstructions": []}
    }
  }
}`)
	mustWrite(t, filepath.Join(dir, "host-token.json"), `{"token":"`+grokBotSentinels[0]+`"}`)
	mustWrite(t, filepath.Join(dir, "host-secrets.json"), `{"secret":"`+grokBotSentinels[0]+`"}`)
	appdata := filepath.Join(s.root, "appdata", "Grok Bot")
	mustMkdir(t, appdata)
	mustWrite(t, filepath.Join(appdata, "sand-secrets.json"), `{"cursor-machine-id":"`+grokBotSentinels[4]+`"}`)
	mustMkdir(t, filepath.Join(s.root, "localappdata", "Programs", "Grok Bot"))
	mustWrite(t, filepath.Join(s.root, "localappdata", "Programs", "Grok Bot", "Grok Bot.exe"), "MZ")
}

// The reader opens only ~/.grokbot/settings.json: nothing else of ~/.grokbot,
// of %APPDATA%\Grok Bot or of the app folder (strace, Linux).
func TestGrokBotReaderOpensOnlySettings(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the open-file trace is implemented for Linux")
	}
	strace, err := exec.LookPath("strace")
	if err != nil {
		t.Skip("strace not available")
	}
	s := newSandbox(t)
	s.runAll(t, nil)
	writeGrokBot(t, s)
	trace := filepath.Join(t.TempDir(), "open.strace")
	cmd := exec.Command(strace, "-f", "-qq", "-e", "trace=open,openat,openat2", "-o", trace, s.binary, "run", "--unattended")
	cmd.Dir = s.work
	cmd.Env = s.env
	if out, err := cmd.CombinedOutput(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run: %v\n%s", err, out)
		}
	}
	f, err := os.Open(trace)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	settings := filepath.Join(s.home, ".grokbot", "settings.json")
	opened := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		for _, dir := range []string{filepath.Join(s.home, ".grokbot"), filepath.Join(s.root, "appdata", "Grok Bot"), filepath.Join(s.root, "localappdata", "Programs", "Grok Bot")} {
			if !strings.Contains(line, `"`+dir) {
				continue
			}
			if strings.Contains(line, `"`+settings+`"`) {
				opened = true
				continue
			}
			t.Errorf("opened outside settings.json: %s", line)
		}
	}
	if !opened {
		t.Error("settings.json was never opened: the test would be vacuous")
	}
}

// No value of settings.json other than the three permission codes and the
// Auto-review switch reaches the report, the log or the state.
func TestGrokBotNoSettingsTextLeaks(t *testing.T) {
	s := newSandbox(t)
	s.runAll(t, nil)
	writeGrokBot(t, s)
	// Enter keeps the default for Cursor's two questions and Grok Bot's two;
	// then «n» to whatever the review asks after: the problem list's three
	// questions and the usage statistics question, where a bare Enter would
	// be «yes».
	runInPTY(t, s, []string{s.binary, "run", "--lang", "en"}, strings.Repeat("\n", 4)+strings.Repeat("n\n", 12))
	if raw, err := os.ReadFile(filepath.Join(s.home, ".rosmarops", "state.json")); err != nil || strings.Contains(string(raw), `"enabled": true`) {
		t.Fatalf("usage statistics on after the review (%v):\n%s", err, raw)
	}
	cmd := exec.Command(s.binary, "run", "--unattended")
	cmd.Dir, cmd.Env = s.work, s.env
	_, _ = cmd.CombinedOutput()
	found := false
	walk := func(root string, fn filepath.WalkFunc) {
		_ = filepath.Walk(root, fn)
		if reports := report.DefaultReportDirIn(s.home); !pathUnder(reports, root) {
			_ = filepath.Walk(reports, fn)
		}
	}
	walk(filepath.Join(s.home, ".rosmarops"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		b, e := os.ReadFile(p)
		if e != nil {
			return nil
		}
		if bytes.Contains(b, []byte("measured_local_setting")) {
			found = true
		}
		for _, v := range grokBotSentinels {
			if bytes.Contains(b, []byte(v)) {
				t.Errorf("%s holds %q", p, v)
			}
		}
		return nil
	})
	if !found {
		t.Error("no report mentions the measured setting: the test would be vacuous")
	}
}
