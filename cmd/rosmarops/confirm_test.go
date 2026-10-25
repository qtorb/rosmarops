package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/state"
)

const acceptedConfig = `{"schema_version": 1, "repo_roots": ["/src/a"], "allow_list": {}}`

// changedConfig adds a rotated credential and a test-data entry (as the
// report's agent prompt would) and a repository.
const changedConfig = `{"schema_version": 1, "repo_roots": ["/src/a", "/src/b"], "allow_list": {
  "rotated_credentials": [{"id": "cred_0123456789abcdef", "date": "2026-10-25", "note": ""}],
  "test_fixtures": [{"repo": "/src/a", "path": "**/*_test.go", "note": "fake keys", "expires_on": "2027-10-01"}]}}`

// confirmSetup is a configuration accepted with its copy, then changed.
func confirmSetup(t *testing.T) (st state.State, stPath, cfgPath, reports string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath, stPath, reports = filepath.Join(dir, "config.json"), filepath.Join(dir, "state.json"), filepath.Join(dir, "reports")
	st, err := state.DefaultState("0.1.0-dev")
	if err != nil {
		t.Fatal(err)
	}
	st.Integrity.ConfigSHA256 = hash.Bytes([]byte(acceptedConfig))
	if err := writeAcceptedCopy(reports, []byte(acceptedConfig)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(changedConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	return st, stPath, cfgPath, reports
}

// v3 §9.6 and adjustment A: the changes, one line each; "s" or "y" (any
// case) accepts — config_sha256 and the copy rewritten together, the
// binary's hash untouched; "n" rejects; without a console it is not asked
// and counts as rejected.
func TestConfirmConfig(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		for _, c := range []struct {
			answer   string
			terminal bool
			ok       bool
			last     string
		}{
			{"s\n", true, true, "console.config_accepted"},
			{"Y\n", true, true, "console.config_accepted"},
			{"n\n", true, false, "console.config_rejected"},
			{"", true, false, "console.config_rejected"},
			{"s\n", false, false, "console.config_unconfirmed"},
		} {
			st, stPath, cfgPath, reports := confirmSetup(t)
			st.Integrity.VigilBinarySHA256 = strings.Repeat("b", 64)
			var out bytes.Buffer
			ok, err := confirmConfig(confirmation{in: strings.NewReader(c.answer), out: &out, terminal: c.terminal}, cat, &st, stPath, cfgPath, reports)
			if err != nil || ok != c.ok {
				t.Fatalf("%s %q: ok %v, err %v", lang, c.answer, ok, err)
			}
			text := out.String()
			for _, want := range []string{
				cat.Text("console.config_changed", nil),
				cat.Text("console.config_change.rotated", map[string]string{"cred_id": "cred_0123456789abcdef"}),
				cat.Text("console.config_change.fixture", map[string]string{"repo": "/src/a", "path": "**/*_test.go"}),
				cat.Text("console.config_change.other", map[string]string{"key": "repo_roots"}),
				cat.Text(c.last, nil),
			} {
				if !strings.Contains(text, want) {
					t.Errorf("%s %q: no %q in %q", lang, c.answer, want, text)
				}
			}
			if asked := strings.Contains(text, cat.Text("console.config_confirm", nil)); asked != c.terminal {
				t.Errorf("%s %q: asked %v", lang, c.answer, asked)
			}
			copyRaw, _ := os.ReadFile(filepath.Join(reports, acceptedCopyName))
			saved, _ := state.LoadState(stPath)
			if c.ok {
				if st.Integrity.ConfigSHA256 != hash.Bytes([]byte(changedConfig)) || string(copyRaw) != changedConfig ||
					saved.Integrity.ConfigSHA256 != st.Integrity.ConfigSHA256 || saved.Integrity.VigilBinarySHA256 != strings.Repeat("b", 64) {
					t.Errorf("%s %q: not accepted together", lang, c.answer)
				}
			} else if string(copyRaw) != acceptedConfig || st.Integrity.ConfigSHA256 != hash.Bytes([]byte(acceptedConfig)) {
				t.Errorf("%s %q: rejected change accepted", lang, c.answer)
			}
		}
	}
}

// The added rule: a copy whose hash is not config_sha256 (tampered, or
// missing) is never used: one «other» line, and the question is asked all
// the same — it follows the configuration's hash, never the copy.
func TestConfirmConfig_TamperedCopy(t *testing.T) {
	cat, _ := catalog.Load("en")
	for _, tamper := range []func(reports string){
		func(r string) { _ = os.WriteFile(filepath.Join(r, acceptedCopyName), []byte(changedConfig), 0o600) },
		func(r string) { _ = os.Remove(filepath.Join(r, acceptedCopyName)) },
	} {
		st, stPath, cfgPath, reports := confirmSetup(t)
		tamper(reports)
		var out bytes.Buffer
		if _, err := confirmConfig(confirmation{in: strings.NewReader("n\n"), out: &out, terminal: true}, cat, &st, stPath, cfgPath, reports); err != nil {
			t.Fatal(err)
		}
		lines := strings.Count(out.String(), "  - ")
		if lines != 1 || !strings.Contains(out.String(), cat.Text("console.config_change.other", map[string]string{"key": "config.json"})) ||
			!strings.Contains(out.String(), cat.Text("console.config_confirm", nil)) {
			t.Errorf("tampered copy: %q", out.String())
		}
	}
}

// v3 §9.6: progress lines from the catalog, never a repository name; on a
// console the repositories line is rewritten in place at most every 250 ms
// and when the last ends; elsewhere only at the start and the end.
func TestProgress(t *testing.T) {
	cat, _ := catalog.Load("en")
	var out bytes.Buffer
	p := newProgress(&out, false, cat)
	p.line("console.progress.agents", nil)
	for i := 0; i <= 5; i++ {
		p.repos(i, 5)
	}
	p.line("console.progress.report", nil)
	want := cat.Text("console.progress.agents", nil) + "\n" +
		cat.Text("console.progress.repos", map[string]string{"done": "0", "total": "5"}) + "\n" +
		cat.Text("console.progress.repos", map[string]string{"done": "5", "total": "5"}) + "\n" +
		cat.Text("console.progress.report", nil) + "\n"
	if out.String() != want {
		t.Errorf("no console:\n%q\nwant\n%q", out.String(), want)
	}

	out.Reset()
	clock := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	p = newProgress(&out, true, cat)
	p.now = func() time.Time { return clock }
	p.repos(0, 3)
	clock = clock.Add(100 * time.Millisecond)
	p.repos(1, 3) // within 250 ms: not rewritten
	clock = clock.Add(200 * time.Millisecond)
	p.repos(2, 3)
	clock = clock.Add(10 * time.Millisecond)
	p.repos(3, 3) // the last: always
	got := out.String()
	if strings.Contains(got, "1/3") || !strings.Contains(got, "\r"+cat.Text("console.progress.repos", map[string]string{"done": "2", "total": "3"})) ||
		!strings.HasSuffix(got, "\r"+cat.Text("console.progress.repos", map[string]string{"done": "3", "total": "3"})+"\n") {
		t.Errorf("console: %q", got)
	}
}

// BV-52: the question is asked only when the input and the output are both
// a console; a file or a pipe on either side is a rejection
// (console.config_unconfirmed).
func TestCanAsk(t *testing.T) {
	dir := t.TempDir()
	file, err := os.Create(filepath.Join(dir, "out.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	for _, c := range [][2]*os.File{{file, file}, {r, w}, {null, null}, {null, file}} {
		if canAsk(c[0], c[1]) {
			t.Errorf("can ask with %s / %s", c[0].Name(), c[1].Name())
		}
	}
	// A console on one side only is not enough.
	if tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0); err == nil {
		defer tty.Close()
		if canAsk(tty, file) || canAsk(file, tty) || canAsk(tty, w) {
			t.Error("can ask with a console on one side only")
		}
	}
}
