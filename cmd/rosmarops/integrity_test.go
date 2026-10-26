package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/state"
)

// noRead fails the test if anything is read: without a console, or in a
// scheduled review, nothing is asked (J3).
type noRead struct{ t *testing.T }

func (r noRead) Read([]byte) (int, error) {
	r.t.Error("integrity: input read without a console")
	return 0, errors.New("no input")
}

// integritySetup is a state that ran once, with both references recorded
// for oldBinary and acceptedConfig, saved at stPath.
func integritySetup(t *testing.T) (st state.State, stPath, cfgPath, reports string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath, stPath, reports = filepath.Join(dir, "config.json"), filepath.Join(dir, "state.json"), filepath.Join(dir, "reports")
	st, err := state.DefaultState("0.0.9")
	if err != nil {
		t.Fatal(err)
	}
	st.EngineVersion = "0.0.9"
	st.LastRun = "2026-10-19T09:00:00Z"
	st.Integrity.VigilBinarySHA256 = oldBinary
	st.Integrity.ConfigSHA256 = hash.Bytes([]byte(acceptedConfig))
	if err := writeAcceptedCopy(reports, []byte(acceptedConfig)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte(acceptedConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveState(stPath, st); err != nil {
		t.Fatal(err)
	}
	return st, stPath, cfgPath, reports
}

var (
	oldBinary = strings.Repeat("a", 64)
	newBinary = "0123456789abcdef" + strings.Repeat("c", 48)
)

// aborted records the abort instead of exiting.
type aborted struct {
	category string
	o        abortOptions
	n        int
}

func newRun(t *testing.T, cat catalog.Catalog, st *state.State, stPath, cfgPath, reports, answer string, terminal, unattended bool, self string, ab *aborted, out *bytes.Buffer) integrityRun {
	var in io.Reader = strings.NewReader(answer)
	if !terminal {
		in = noRead{t}
	}
	return integrityRun{
		ask: asker{in: in, out: out, terminal: terminal, cat: cat},
		st:  st, stPath: stPath, cfgPath: cfgPath, reportDir: reports,
		unattended: unattended, lang: "en",
		selfHash: func() (string, error) { return self, nil },
		abort: func(category, _, _ string, o abortOptions) {
			ab.category, ab.o = category, o
			ab.n++
		},
	}
}

func savedState(t *testing.T, path string) state.State {
	t.Helper()
	st, err := state.LoadState(path)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// J2: a changed binary prints both versions and the first 16 characters of
// both fingerprints, then asks (default «no»). «y» accepts and records the
// new hash and version; anything else, no console or a scheduled review
// abort red, and the state keeps the old reference.
func TestIntegrityBinaryChanged(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		for _, c := range []struct {
			name                 string
			answer               string
			terminal, unattended bool
			accept               bool
		}{
			{"yes", "y\n", true, false, true},
			{"si", "S\n", true, false, true},
			{"no", "n\n", true, false, false},
			{"enter", "\n", true, false, false},
			{"no console", "", false, false, false},
			{"unattended", "", false, true, false},
		} {
			st, stPath, cfgPath, reports := integritySetup(t)
			var out bytes.Buffer
			var ab aborted
			_, err := newRun(t, cat, &st, stPath, cfgPath, reports, c.answer, c.terminal, c.unattended, newBinary, &ab, &out).check()
			saved := savedState(t, stPath)
			if c.accept {
				if err != nil || ab.n != 0 || saved.Integrity.VigilBinarySHA256 != newBinary || saved.EngineVersion != version {
					t.Errorf("%s %s: err %v, aborts %d, saved %s %s", lang, c.name, err, ab.n, saved.Integrity.VigilBinarySHA256, saved.EngineVersion)
				}
			} else {
				if !errors.Is(err, errAborted) || ab.n != 1 || ab.category != "binary_hash_changed" || ab.o.unattended != c.unattended {
					t.Errorf("%s %s: err %v, abort %+v", lang, c.name, err, ab)
				}
				if saved.Integrity.VigilBinarySHA256 != oldBinary {
					t.Errorf("%s %s: the reference changed without consent", lang, c.name)
				}
			}
			want := cat.Text("console.binary_changed", map[string]string{
				"old_version": "0.0.9", "old_hash": oldBinary[:16], "new_version": version, "new_hash": newBinary[:16],
			})
			if !strings.Contains(out.String(), want) {
				t.Errorf("%s %s: no %q in %q", lang, c.name, want, out.String())
			}
			if strings.Contains(out.String(), newBinary[16:]) {
				t.Errorf("%s %s: more than 16 characters of the fingerprint", lang, c.name)
			}
			asked := strings.Contains(out.String(), cat.Text("console.binary_confirm", nil))
			if asked != c.terminal {
				t.Errorf("%s %s: asked %v", lang, c.name, asked)
			}
		}
	}
}

// J1: a state that ran before with zero references is a change, never a
// fresh start: with a console it asks (state_lost + state_confirm); without
// one, or unattended, red. The references are never fixed in silence.
func TestIntegrityStateLost(t *testing.T) {
	cat, _ := catalog.Load("en")
	for _, c := range []struct {
		name                 string
		answer               string
		terminal, unattended bool
		accept               bool
	}{
		{"yes", "y\n", true, false, true},
		{"no", "n\n", true, false, false},
		{"no console", "", false, false, false},
		{"unattended", "", false, true, false},
	} {
		for _, which := range []string{"binary", "config", "both"} {
			st, stPath, cfgPath, reports := integritySetup(t)
			if which != "config" {
				st.Integrity.VigilBinarySHA256 = zeroHash
			}
			if which != "binary" {
				st.Integrity.ConfigSHA256 = zeroHash
			}
			if err := state.SaveState(stPath, st); err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			var ab aborted
			_, err := newRun(t, cat, &st, stPath, cfgPath, reports, c.answer, c.terminal, c.unattended, newBinary, &ab, &out).check()
			saved := savedState(t, stPath)
			if !strings.Contains(out.String(), cat.Text("console.state_lost", nil)) {
				t.Errorf("%s %s: no state_lost in %q", c.name, which, out.String())
			}
			if c.accept {
				if err != nil || ab.n != 0 || saved.Integrity.VigilBinarySHA256 != newBinary || saved.Integrity.ConfigSHA256 != hash.Bytes([]byte(acceptedConfig)) {
					t.Errorf("%s %s: err %v, aborts %d, saved %+v", c.name, which, err, ab.n, saved.Integrity)
				}
				continue
			}
			if !errors.Is(err, errAborted) || ab.n != 1 || ab.o.unattended != c.unattended {
				t.Errorf("%s %s: err %v, abort %+v", c.name, which, err, ab)
			}
			if saved.Integrity.VigilBinarySHA256 == newBinary || (which != "binary" && saved.Integrity.ConfigSHA256 != zeroHash) {
				t.Errorf("%s %s: references fixed in silence: %+v", c.name, which, saved.Integrity)
			}
		}
	}
}

// A state that never ran with zero references (an init before J1): nothing
// to compare with, nothing written.
func TestIntegrityNeverRan(t *testing.T) {
	cat, _ := catalog.Load("en")
	st, stPath, cfgPath, reports := integritySetup(t)
	st.LastRun = state.NeverRun
	st.Integrity.VigilBinarySHA256 = zeroHash
	var out bytes.Buffer
	var ab aborted
	if _, err := newRun(t, cat, &st, stPath, cfgPath, reports, "", false, true, newBinary, &ab, &out).check(); err != nil || ab.n != 0 {
		t.Fatalf("err %v, aborts %d", err, ab.n)
	}
	if savedState(t, stPath).Integrity.VigilBinarySHA256 != oldBinary {
		t.Error("state rewritten")
	}
}

// adjustment A under J3: a changed configuration with a console asks; «n»
// aborts with the configuration rejected; unattended aborts without reading.
func TestIntegrityConfigChanged(t *testing.T) {
	cat, _ := catalog.Load("en")
	for _, c := range []struct {
		name                 string
		answer               string
		terminal, unattended bool
		accept               bool
	}{
		{"yes", "s\n", true, false, true},
		{"no", "n\n", true, false, false},
		{"unattended", "", false, true, false},
	} {
		st, stPath, cfgPath, reports := integritySetup(t)
		if err := os.WriteFile(cfgPath, []byte(changedConfig), 0o600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		var ab aborted
		_, err := newRun(t, cat, &st, stPath, cfgPath, reports, c.answer, c.terminal, c.unattended, oldBinary, &ab, &out).check()
		if c.accept {
			if err != nil || ab.n != 0 || savedState(t, stPath).Integrity.ConfigSHA256 != hash.Bytes([]byte(changedConfig)) {
				t.Errorf("%s: err %v, aborts %d", c.name, err, ab.n)
			}
			continue
		}
		if !errors.Is(err, errAborted) || ab.category != "config_hash_changed" || !ab.o.configRejected || ab.o.unattended != c.unattended {
			t.Errorf("%s: err %v, abort %+v", c.name, err, ab)
		}
	}
}

// J1: a state that is gone or cannot be read is a lost state: with a
// console it asks, and accepted the references are recorded again and the
// report says the integrity was not checked; otherwise, red. Never fixed in
// silence.
func TestIntegrityStateUnreadable(t *testing.T) {
	cat, _ := catalog.Load("en")
	for _, c := range []struct {
		name                 string
		answer               string
		terminal, unattended bool
		accept               bool
	}{
		{"yes", "s\n", true, false, true},
		{"no", "n\n", true, false, false},
		{"no console", "", false, false, false},
		{"unattended", "", false, true, false},
	} {
		_, stPath, cfgPath, reports := integritySetup(t)
		if err := os.Remove(stPath); err != nil {
			t.Fatal(err)
		}
		fallback, err := state.DefaultState(version)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		var ab aborted
		r := newRun(t, cat, &fallback, stPath, cfgPath, reports, c.answer, c.terminal, c.unattended, newBinary, &ab, &out)
		r.stErr = errors.New("state: no such file")
		notChecked, err := r.check()
		if !strings.Contains(out.String(), cat.Text("console.state_lost", nil)) {
			t.Errorf("%s: no state_lost in %q", c.name, out.String())
		}
		if c.accept {
			saved := savedState(t, stPath)
			if !notChecked || err != nil || ab.n != 0 || saved.Integrity.VigilBinarySHA256 != newBinary || saved.Integrity.ConfigSHA256 != hash.Bytes([]byte(acceptedConfig)) {
				t.Errorf("%s: notChecked %v, err %v, aborts %d, saved %+v", c.name, notChecked, err, ab.n, saved.Integrity)
			}
			continue
		}
		if !errors.Is(err, errAborted) || ab.n != 1 || ab.category != "binary_hash_changed" || ab.o.unattended != c.unattended {
			t.Errorf("%s: err %v, abort %+v", c.name, err, ab)
		}
		if _, statErr := os.Stat(stPath); statErr == nil {
			t.Errorf("%s: a state was written", c.name)
		}
	}
}

// Nothing changed: no question, no abort, nothing written.
func TestIntegrityUnchanged(t *testing.T) {
	cat, _ := catalog.Load("en")
	st, stPath, cfgPath, reports := integritySetup(t)
	var out bytes.Buffer
	var ab aborted
	notChecked, err := newRun(t, cat, &st, stPath, cfgPath, reports, "", false, true, oldBinary, &ab, &out).check()
	if notChecked || err != nil || ab.n != 0 || out.Len() != 0 {
		t.Fatalf("notChecked %v, err %v, aborts %d, out %q", notChecked, err, ab.n, out.String())
	}
}
