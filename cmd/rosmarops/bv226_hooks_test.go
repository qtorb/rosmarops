package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/agenthooks"
	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/state"
)

// bv226Home is a fake home with a state whose binary reference is ref
// (the test binary's own, or another one), a project with a hook, and the
// hooks' streams captured. It returns the home, the project and the output.
func bv226Home(t *testing.T, ref string) (home, proj string, out *bytes.Buffer) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_ALL", "")
	stPath, err := state.StatePath()
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.DefaultState("0.0.9")
	if err != nil {
		t.Fatal(err)
	}
	st.Integrity.VigilBinarySHA256 = ref
	if err := os.MkdirAll(filepath.Dir(stPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveState(stPath, st); err != nil {
		t.Fatal(err)
	}
	proj = filepath.Join(home, "src", "app")
	if err := os.MkdirAll(filepath.Join(proj, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, ".claude", "settings.json"), []byte(`{"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"./run.sh"}]}]}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	out = &bytes.Buffer{}
	prevIn, prevOut, prevLang := sessionInput, sessionOutput, systemLang
	t.Cleanup(func() { sessionInput, sessionOutput, systemLang = prevIn, prevOut, prevLang })
	sessionOutput = out
	systemLang = func() string { return "en" }
	return home, proj, out
}

func selfRef(t *testing.T) string {
	t.Helper()
	h, err := hash.Self()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func hookInput(proj string) string {
	return fmt.Sprintf(`{"session_id":"s1","transcript_path":"/t.jsonl","cwd":%q,"hook_event_name":"SessionStart","source":"startup"}`, proj)
}

// files lists every file under dir (none when it does not exist).
func files(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, p)
		}
		return nil
	})
	return out
}

// TestSessionCheckCommand (BV-226 §1, §7): the first opening is told in one
// systemMessage object and nothing else; the snapshot is written.
func TestSessionCheckCommand(t *testing.T) {
	home, proj, out := bv226Home(t, selfRef(t))
	sessionInput = strings.NewReader(hookInput(proj))
	if err := runSessionCheck(cli.Invocation{}); err != nil {
		t.Fatal(err)
	}
	var m map[string]string
	if err := json.Unmarshal(out.Bytes(), &m); err != nil || len(m) != 1 {
		t.Fatalf("output is not one systemMessage object: %q", out.String())
	}
	if !strings.Contains(m["systemMessage"], "«PreToolUse»") {
		t.Errorf("message %q", m["systemMessage"])
	}
	if got := files(t, filepath.Join(home, ".rosmarops", "reports", "data", agenthooks.SessionDir)); len(got) != 1 {
		t.Errorf("snapshots %v", got)
	}
	// Cursor's input, and a clear: silence, nothing written anew.
	for _, raw := range []string{
		`{"conversation_id":"c","generation_id":"g","hook_event_name":"sessionStart","cursor_version":"2.1","workspace_roots":[` + fmt.Sprintf("%q", proj) + `]}`,
		strings.Replace(hookInput(proj), `"startup"`, `"clear"`, 1),
	} {
		out.Reset()
		sessionInput = strings.NewReader(raw)
		_ = runSessionCheck(cli.Invocation{})
		if out.Len() != 0 {
			t.Errorf("said %q for %s", out.String(), raw)
		}
	}
}

// TestSessionCheckIntegrityChanged (BV-226 §3, §7): a changed binary gives
// session.integrity_changed, exits 0 and writes nothing.
func TestSessionCheckIntegrityChanged(t *testing.T) {
	home, proj, out := bv226Home(t, strings.Repeat("e", 64))
	stPath, _ := state.StatePath()
	before, _ := os.ReadFile(stPath)
	sessionInput = strings.NewReader(hookInput(proj))
	if err := runSessionCheck(cli.Invocation{}); err != nil {
		t.Fatalf("exit not 0: %v", err)
	}
	en, _ := catalog.Load("en")
	var m map[string]string
	if json.Unmarshal(out.Bytes(), &m) != nil || m["systemMessage"] != en.Text("session.integrity_changed", nil) {
		t.Errorf("output %q", out.String())
	}
	if got := files(t, filepath.Join(home, ".rosmarops", "reports")); len(got) != 0 {
		t.Errorf("wrote %v", got)
	}
	if after, _ := os.ReadFile(stPath); !bytes.Equal(before, after) {
		t.Error("the state changed")
	}
}

// slowReader blocks until released: a hook input that never ends in time.
type slowReader struct{ release chan struct{} }

func (s slowReader) Read([]byte) (int, error) { <-s.release; return 0, io.EOF }

// TestSessionCheckOverBudget (BV-226 §1, §7): past the budget it says and
// writes nothing.
func TestSessionCheckOverBudget(t *testing.T) {
	home, _, out := bv226Home(t, selfRef(t))
	prev := sessionBudget
	t.Cleanup(func() { sessionBudget = prev })
	sessionBudget = 50 * time.Millisecond
	r := slowReader{release: make(chan struct{})}
	defer close(r.release)
	sessionInput = r
	start := time.Now()
	if err := runSessionCheck(cli.Invocation{}); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > time.Second {
		t.Errorf("took %v", time.Since(start))
	}
	if out.Len() != 0 {
		t.Errorf("said %q", out.String())
	}
	if got := files(t, filepath.Join(home, ".rosmarops", "reports")); len(got) != 0 {
		t.Errorf("wrote %v", got)
	}
}

// TestStatuslineCommand (BV-226 §2, §7): the run's line, the stale form past
// 8 days, the aborted line, and the integrity line with a changed binary.
func TestStatuslineCommand(t *testing.T) {
	home, _, out := bv226Home(t, selfRef(t))
	data := filepath.Join(home, ".rosmarops", "reports", "data")
	en, _ := catalog.Load("en")
	checked := time.Date(2026, 11, 13, 9, 0, 0, 0, time.UTC)
	prevNow := statuslineNow
	t.Cleanup(func() { statuslineNow = prevNow })
	show := func(now time.Time) string {
		out.Reset()
		statuslineNow = func() time.Time { return now }
		if err := runStatusline(cli.Invocation{}); err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out.String())
	}
	if err := agenthooks.WriteStatusline(data, checked, map[string]string{"en": agenthooks.StatuslineText(en, 1, 2, 64)}); err != nil {
		t.Fatal(err)
	}
	if got := show(checked.Add(time.Hour)); !strings.HasPrefix(got, "RosmarOps · 1 red · 2 yellow · 64/100 · ") {
		t.Errorf("fresh: %q", got)
	}
	if got := show(checked.Add(9*24*time.Hour + time.Minute)); !strings.HasSuffix(got, en.Text("statusline.stale", map[string]string{"days": "9"})) {
		t.Errorf("stale: %q", got)
	}
	writeAbortedStatusline(filepath.Join(home, ".rosmarops", "reports"), checked)
	if got := show(checked); got != en.Text("statusline.aborted", nil) {
		t.Errorf("aborted: %q", got)
	}
	// A changed binary: the integrity line, nothing written.
	home2, _, out2 := bv226Home(t, strings.Repeat("e", 64))
	before := files(t, home2)
	if err := runStatusline(cli.Invocation{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(out2.String()); got != en.Text("statusline.integrity_changed", nil) {
		t.Errorf("integrity: %q", got)
	}
	if after := files(t, home2); len(after) != len(before) {
		t.Errorf("statusline wrote: %v", after)
	}
}

// TestHiddenCommands: Claude Code runs them, so the help never lists them.
func TestHiddenCommands(t *testing.T) {
	for _, c := range newRootCmd().Subs {
		if (c.Name == "session-check" || c.Name == "statusline") && !c.Hidden {
			t.Errorf("%s is not hidden", c.Name)
		}
	}
}

// TestSessionCheckCursorSilent (BV-226 §1, the advisor's rule of 13-11):
// Cursor's sessionStart, the same with Claude Code's event name and no source,
// and a valid Claude Code input with cursor_version: silent, nothing written.
func TestSessionCheckCursorSilent(t *testing.T) {
	home, proj, out := bv226Home(t, selfRef(t))
	cursor := func(event string) string {
		return `{"conversation_id":"c1","generation_id":"g1","model":"gpt-5","model_id":"gpt-5","model_params":{},"hook_event_name":"` + event +
			`","cursor_version":"2.1.0","workspace_roots":[` + fmt.Sprintf("%q", proj) + `],"user_email":null,"transcript_path":null,"session_id":"c1","is_background_agent":false,"composer_mode":"agent"}`
	}
	for _, raw := range []string{
		cursor("sessionStart"),
		cursor("SessionStart"),
		strings.Replace(hookInput(proj), `{`, `{"cursor_version":"2.1.0",`, 1),
	} {
		out.Reset()
		sessionInput = strings.NewReader(raw)
		if err := runSessionCheck(cli.Invocation{}); err != nil {
			t.Fatal(err)
		}
		if out.Len() != 0 {
			t.Errorf("said %q for %s", out.String(), raw)
		}
		if got := files(t, filepath.Join(home, ".rosmarops", "reports")); len(got) != 0 {
			t.Errorf("wrote %v for %s", got, raw)
		}
	}
}
