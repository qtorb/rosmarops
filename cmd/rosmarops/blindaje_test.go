package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/firstrun"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
)

// emptyDetection is a first-run detection that finds nothing.
func emptyDetection() firstrun.Result { return firstrun.Result{} }

// writeIntegrityState writes a config and state under home so a double click
// skips first-use initialization. Their content is irrelevant to a test that
// errors before the integrity check runs.
func writeIntegrityState(t *testing.T, home string) {
	t.Helper()
	dir := filepath.Join(home, ".rosmarops")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteConfig(filepath.Join(dir, "config.json"), config.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	st, err := state.DefaultState(version)
	if err != nil {
		t.Fatal(err)
	}
	if err := state.SaveState(filepath.Join(dir, "state.json"), st); err != nil {
		t.Fatal(err)
	}
}

// BV-93, B3. A scheduled review and a double click never fail in silence, a
// double click with reports but no config/state is a lost state and not a
// first use, and integrity is verified before any config value is used.

// homeEnv points every home/config env var at a temp home and returns it.
func homeEnv(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, "home")
	for _, name := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME", "XDG_CACHE_HOME"} {
		dir := filepath.Join(root, strings.ToLower(name))
		if name == "HOME" || name == "USERPROFILE" {
			dir = home
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv(name, dir)
	}
	work := filepath.Join(root, "work")
	if err := os.MkdirAll(work, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)
	return home
}

// captureAbortRun replaces abortRun with a recorder for the test's duration.
func captureAbortRun(t *testing.T) *struct {
	called bool
	opts   abortOptions
	cause  error
} {
	t.Helper()
	rec := &struct {
		called bool
		opts   abortOptions
		cause  error
	}{}
	prev := abortRun
	t.Cleanup(func() { abortRun = prev })
	abortRun = func(o abortOptions, cause error) { rec.called, rec.opts, rec.cause = true, o, cause }
	return rec
}

// B3: a scheduled review whose run errors goes through the run_failed abort.
func TestRunFailed_UnattendedAborts(t *testing.T) {
	homeEnv(t)
	rec := captureAbortRun(t)
	// An unsupported language makes the run return before any scan.
	err := runRun(cli.Invocation{Path: []string{"run"}, Flags: map[string]string{"lang": "fr", "unattended": "true"}})
	if !rec.called {
		t.Fatal("abortRun was not called for a failed scheduled review")
	}
	if !rec.opts.unattended || rec.opts.window {
		t.Errorf("abort options = %+v, want unattended", rec.opts)
	}
	if rec.opts.lang != "fr" {
		t.Errorf("abort lang = %q, want fr", rec.opts.lang)
	}
	if err == nil || rec.cause == nil {
		t.Errorf("err = %v, cause = %v: want both set", err, rec.cause)
	}
}

// B3: a manual run that errors does NOT go through the abort — it returns the
// error to the caller, as before. Only a scheduled review or a double click
// must not fail in silence.
func TestRunFailed_ManualDoesNotAbort(t *testing.T) {
	homeEnv(t)
	rec := captureAbortRun(t)
	err := runRun(cli.Invocation{Path: []string{"run"}, Flags: map[string]string{"lang": "fr"}})
	if rec.called {
		t.Error("abortRun was called for a manual run")
	}
	if err == nil {
		t.Error("a manual run with a bad language returned no error")
	}
}

// B3: a double click whose run errors goes through the run_failed abort.
func TestRunFailed_WindowAborts(t *testing.T) {
	home := homeEnv(t)
	writeIntegrityState(t, home) // config + state present, so no first-use init
	rec := captureAbortRun(t)
	prevLang := systemLang
	t.Cleanup(func() { systemLang = prevLang })
	systemLang = func() string { return "fr" } // unsupported: the run errors

	if code := runNoArgs(); code != exitRunFailed {
		t.Errorf("exit = %d, want %d", code, exitRunFailed)
	}
	if !rec.called || !rec.opts.window || rec.opts.unattended {
		t.Errorf("abort options = %+v (called %v), want window", rec.opts, rec.called)
	}
}

// B3: a double click with reports already in the folder but neither config nor
// state is a lost state (J1), not a first use: it does not reinitialize in
// silence.
func TestDoubleClick_StateLostWhenReportsButNoConfigState(t *testing.T) {
	home := homeEnv(t)
	reports := filepath.Join(home, ".rosmarops", "reports")
	if err := os.MkdirAll(reports, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reports, report.HTMLFileName), []byte("<html>old</html>"), 0o600); err != nil {
		t.Fatal(err)
	}

	var called bool
	var opts abortOptions
	prev := abortIntegrity
	t.Cleanup(func() { abortIntegrity = prev })
	abortIntegrity = func(category, why, action string, o abortOptions) { called, opts = true, o }
	prevLang, prevDetect := systemLang, detectProjects
	t.Cleanup(func() { systemLang, detectProjects = prevLang, prevDetect })
	systemLang = func() string { return "en" }
	detectProjects = func() firstrun.Result { panic("detection must not run for a lost state") }

	code := runNoArgs()
	if code != exitIntegrity {
		t.Errorf("exit = %d, want %d", code, exitIntegrity)
	}
	if !called || !opts.stateLost || !opts.window {
		t.Errorf("abort options = %+v (called %v), want stateLost+window", opts, called)
	}
	// No silent reinitialization: the config was not created.
	if _, err := os.Stat(filepath.Join(home, ".rosmarops", "config.json")); err == nil {
		t.Error("config.json was created: the lost state was silently reinitialized")
	}
}

// B3: an empty reports folder with no config or state is a genuine first use —
// it initializes and does not abort as a lost state.
func TestDoubleClick_FirstUseInitializes(t *testing.T) {
	home := homeEnv(t)
	var aborted bool
	prev := abortIntegrity
	t.Cleanup(func() { abortIntegrity = prev })
	abortIntegrity = func(category, why, action string, o abortOptions) { aborted = true }
	prevLang, prevDetect, prevPick, prevOpen, prevDelay := systemLang, detectProjects, pickFolder, openReportFile, closeDelay
	t.Cleanup(func() {
		systemLang, detectProjects, pickFolder, openReportFile, closeDelay = prevLang, prevDetect, prevPick, prevOpen, prevDelay
	})
	systemLang = func() string { return "en" }
	detectProjects = emptyDetection
	pickFolder = func(string) (string, error) { return "", nil }
	openReportFile = func(string) error { return nil }
	closeDelay = 0

	if code := runNoArgs(); code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if aborted {
		t.Error("a genuine first use aborted as a lost state")
	}
	if _, err := os.Stat(filepath.Join(home, ".rosmarops", "config.json")); err != nil {
		t.Errorf("config.json not created on a first use: %v", err)
	}
}

// B3: integrity is checked before any config value is used. A configuration
// with a custom report directory still writes its scheduled-run log to the
// fixed default directory, never to the directory the (not-yet-verified)
// config names.
func TestIntegrityBeforeConfig_LogInDefaultDir(t *testing.T) {
	home := homeEnv(t)
	dir := filepath.Join(home, ".rosmarops")
	defaultReports := filepath.Join(dir, "reports")
	customReports := filepath.Join(t.TempDir(), "custom-reports")
	cfgPath, stPath := filepath.Join(dir, "config.json"), filepath.Join(dir, "state.json")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.ReportDirectory = customReports
	if err := config.WriteConfig(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(cfgPath)
	self, err := hash.Self()
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.DefaultState(version)
	if err != nil {
		t.Fatal(err)
	}
	st.Integrity.VigilBinarySHA256 = self
	st.Integrity.ConfigSHA256 = hash.Bytes(raw)
	if err := state.SaveState(stPath, st); err != nil {
		t.Fatal(err)
	}
	// The default directory must exist for the log; the accepted copy lives
	// there too while the config is still unverified.
	if err := os.MkdirAll(defaultReports, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeAcceptedCopy(defaultReports, raw); err != nil {
		t.Fatal(err)
	}

	if _, err := runWith("en", runMode{unattended: true}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(defaultReports, ScheduledLogName)); err != nil {
		t.Errorf("scheduled log not in the default report dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(customReports, ScheduledLogName)); err == nil {
		t.Error("scheduled log written to the config's report dir before integrity was verified")
	}
	// The report itself does go to the configured directory (used after the
	// integrity check passed).
	if _, err := os.Stat(filepath.Join(customReports, "report.v2.json")); err != nil {
		t.Errorf("report not written to the configured dir: %v", err)
	}
}
