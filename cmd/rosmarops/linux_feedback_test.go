package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
)

// placeholder is a {name} left unfilled in a text shown to the person.
var placeholder = regexp.MustCompile(`\{[a-z_]+\}`)

// Every integrity stop takes its why and action from the catalog, in both
// languages: filled, and never sending the person to baseline.
func TestIntegrityAbortTexts(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		for _, k := range []string{abortBinary, abortConfig, abortState, abortUnverifiable} {
			for _, part := range []string{".why", ".action"} {
				got := cat.Text(k+part, map[string]string{"error": "hash failed"})
				if got == "" || got == k+part || strings.Contains(got, "["+k) {
					t.Errorf("%s %s%s: no text (%q)", lang, k, part, got)
				}
				if strings.Contains(strings.ToLower(got), "baseline") || placeholder.MatchString(got) {
					t.Errorf("%s %s%s: %q", lang, k, part, got)
				}
			}
		}
	}
}

// run's check stops with the catalog key of what changed.
func TestIntegrityAbortKeys(t *testing.T) {
	cat, _ := catalog.Load("en")
	st, stPath, cfgPath, reports := integritySetup(t)
	var out bytes.Buffer
	var ab aborted
	if _, err := newRun(t, cat, &st, stPath, cfgPath, reports, "", false, false, newBinary, &ab, &out).check(); !errors.Is(err, errAborted) || ab.text != abortBinary {
		t.Errorf("binary: err %v, text %q", err, ab.text)
	}
	st, stPath, cfgPath, reports = integritySetup(t)
	if err := os.WriteFile(cfgPath, []byte(`{"schema_version":1,"repo_roots":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	ab = aborted{}
	if _, err := newRun(t, cat, &st, stPath, cfgPath, reports, "", false, false, oldBinary, &ab, &out).check(); !errors.Is(err, errAborted) || ab.text != abortConfig {
		t.Errorf("config: err %v, text %q", err, ab.text)
	}
	st, stPath, cfgPath, reports = integritySetup(t)
	st.Integrity.VigilBinarySHA256 = zeroHash
	ab = aborted{}
	if _, err := newRun(t, cat, &st, stPath, cfgPath, reports, "", false, false, newBinary, &ab, &out).check(); !errors.Is(err, errAborted) || ab.text != abortState {
		t.Errorf("state: err %v, text %q", err, ab.text)
	}
	if strings.Contains(strings.ToLower(out.String()), "baseline") {
		t.Errorf("baseline in the output: %q", out.String())
	}
}

// Before starting from scratch, the line about a new identity, only when the
// computer was connected to a space or the state could not be read, and only
// when the question is asked.
func TestStateNewIdentityBeforeConfirm(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		line := cat.Text("console.state_new_identity", nil)
		confirm := cat.Text("console.state_confirm", nil)
		for _, c := range []struct {
			name           string
			space, readErr bool
			terminal       bool
			want           bool
		}{
			{"connected", true, false, true, true},
			{"not connected", false, false, true, false},
			{"state unreadable", false, true, true, true},
			{"connected, no console", true, false, false, false},
		} {
			st, stPath, cfgPath, reports := integritySetup(t)
			st.Integrity.VigilBinarySHA256 = zeroHash
			st.SpacePermission = c.space
			var out bytes.Buffer
			var ab aborted
			r := newRun(t, cat, &st, stPath, cfgPath, reports, "n\n", c.terminal, false, newBinary, &ab, &out)
			if c.readErr {
				r.stErr = errors.New("unreadable")
			}
			_, _ = r.check()
			got := out.String()
			if strings.Contains(got, line) != c.want {
				t.Errorf("%s %s: line shown %v, want %v in %q", lang, c.name, !c.want, c.want, got)
			}
			if c.want && strings.Index(got, line) > strings.Index(got, confirm) {
				t.Errorf("%s %s: the line comes after the question", lang, c.name)
			}
		}
	}
}

// moveOldReports moves the reports once and says so; with report_directory
// in the person's configuration, nothing moves.
func TestMoveOldReportsConsole(t *testing.T) {
	cat, _ := catalog.Load("es")
	for _, c := range []struct {
		name      string
		reportDir bool
		want      bool
	}{
		{"default folder", false, true},
		{"report_directory set", true, false},
	} {
		home := t.TempDir()
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		t.Chdir(t.TempDir())
		old := report.LegacyReportDir(home)
		if err := os.MkdirAll(filepath.Join(old, report.DataSubdir), 0o700); err != nil {
			t.Fatal(err)
		}
		cfg := `{"schema_version":1}`
		if c.reportDir {
			cfg = `{"schema_version":1,"report_directory":` + `"` + filepath.ToSlash(filepath.Join(home, "mine")) + `"}`
		}
		if err := os.WriteFile(filepath.Join(home, ".rosmarops", "config.json"), []byte(cfg), 0o600); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		moveOldReportsFor("linux", &out, cat)
		_, err := os.Stat(filepath.Join(home, report.LinuxReportDirName, report.DataSubdir))
		if moved := err == nil; moved != c.want {
			t.Errorf("%s: moved %v, want %v", c.name, moved, c.want)
		}
		if shown := strings.Contains(out.String(), cat.Text("console.reports_moved", nil)); shown != c.want {
			t.Errorf("%s: message shown %v: %q", c.name, shown, out.String())
		}
		out.Reset()
		moveOldReportsFor("linux", &out, cat)
		if out.Len() != 0 {
			t.Errorf("%s: said something the second time: %q", c.name, out.String())
		}
	}
}

// The snap hint only for a report inside a hidden folder.
func TestSnapHintOnlyInHiddenFolder(t *testing.T) {
	for path, want := range map[string]bool{
		"/home/u/.rosmarops/reports/report.html": true,
		"/home/u/RosmarOps/report.html":          false,
		"/home/u/.hidden/x/report.html":          true,
		"/home/u/RosmarOps/.report.html":         false, // the file itself does not count
	} {
		if got := inHiddenFolder(path); got != want {
			t.Errorf("inHiddenFolder(%q) = %v", path, got)
		}
	}
}

// After a review with a new finding, run prints the line that names
// rosmarops findings and never shows the findings UI.
func TestFindingsHintInsteadOfQuestion(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cfgPath, _, _ := bv184CmdSetup(t)
		prev := dismissUIHook
		dismissUIHook = func([]report.FindingCard, catalog.Catalog) (findingChoice, bool) {
			t.Error("the findings UI was shown")
			return findingChoice{}, false
		}
		var out bytes.Buffer
		findingsHintAfterReport(&out, lang, cfgPath)
		dismissUIHook = prev
		cat, _ := catalog.Load(lang)
		if got := out.String(); got != cat.Text("console.findings_hint", nil)+"\n" || placeholder.MatchString(got) {
			t.Errorf("%s: %q", lang, got)
		}
	}
	// A configuration outside ~/.rosmarops: nothing to point to.
	var out bytes.Buffer
	findingsHintAfterReport(&out, "en", filepath.Join(t.TempDir(), "rosmarops.config.json"))
	if out.Len() != 0 {
		t.Errorf("hint for a configuration outside: %q", out.String())
	}
}

// os_version is read from the system's own files, cleaned to what
// problems.v1 allows.
func TestOSVersionReaders(t *testing.T) {
	for in, want := range map[string]string{
		"NAME=\"Ubuntu\"\nID=ubuntu\nVERSION_ID=\"24.04\"\n": "ubuntu 24.04",
		"ID=fedora\nVERSION_ID=41\n":                         "fedora 41",
		"ID=arch\n":                                          "arch",
		"ID=\"weird$;id\"\nVERSION_ID='1.2'\n":               "weirdid 1.2",
		"":                                                   "",
	} {
		if got := osReleaseVersion(in); got != want {
			t.Errorf("osReleaseVersion(%q) = %q, want %q", in, got, want)
		}
	}
	plist := `<plist><dict><key>ProductName</key><string>macOS</string><key>ProductVersion</key><string>15.1</string></dict></plist>`
	if got := plistProductVersion(plist); got != "15.1" {
		t.Errorf("plist: %q", got)
	}
	if got := plistProductVersion("<plist/>"); got != "" {
		t.Errorf("empty plist: %q", got)
	}
	if got := cleanOSVersion(strings.Repeat("a", 60)); len(got) != osVersionMaxLen {
		t.Errorf("not cut to 40: %d", len(got))
	}
	if v := osVersion(); !regexp.MustCompile(`^[0-9A-Za-z._ -]{0,40}$`).MatchString(v) {
		t.Errorf("this system's os_version %q does not fit problems.v1", v)
	}
}

// The state's folder is created when the state is saved for the first time:
// on Linux nothing else creates ~/.rosmarops before a company first run.
func TestSaveStateCreatesItsFolder(t *testing.T) {
	st, err := state.DefaultState("0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), ".rosmarops", "state.json")
	if err := state.SaveState(p, st); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Dir(p))
	if err != nil || !info.IsDir() {
		t.Fatalf("folder: %v", err)
	}
	// Windows keeps no Unix mode: there the folder's access is its ACL.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		t.Errorf("folder mode %v, want 0700", info.Mode().Perm())
	}
}
