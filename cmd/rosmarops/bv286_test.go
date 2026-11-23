package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/firstrun"
	"github.com/qtorb/rosmarops/internal/problems"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/spacenet"
	"github.com/qtorb/rosmarops/internal/state"
)

// bv286Home is a home for a double click of a published version, with
// someone at a console: the windows' seams record, in order, what each
// review showed (E usage statistics, C connect, F «No me afecta», P the
// problem list). usage is the state's answer. withProblem leaves one unsent
// problem. It returns the order slice and the state path.
func bv286Home(t *testing.T, usage *state.Usage, withProblem bool, window usageWindowAnswer) (*[]string, string) {
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
	t.Chdir(root)
	rosmaDir := filepath.Join(home, ".rosmarops")
	if err := os.MkdirAll(rosmaDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteConfig(filepath.Join(rosmaDir, "config.json"), config.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	st, err := state.DefaultState("0.20.0")
	if err != nil {
		t.Fatal(err)
	}
	st.Usage = usage
	stPath := filepath.Join(rosmaDir, "state.json")
	if err := state.SaveState(stPath, st); err != nil {
		t.Fatal(err)
	}
	if withProblem {
		dir, err := report.DefaultReportDir()
		if err != nil {
			t.Fatal(err)
		}
		recordProblems(report.DataDir(dir), problems.Entry{Code: problems.StepFailed, Step: problems.StepReview, Day: "2026-11-23"})
	}

	prev := struct {
		lang    func() string
		detect  func() firstrun.Result
		pick    func(string) (string, error)
		open    func(string) error
		delay   time.Duration
		send    func(context.Context, spacenet.UsageConsent, []byte) (int, error)
		canAsk  func() bool
		in      io.Reader
		version string
		connect func(spacePaths, catalog.Catalog) connectResult
		dismiss func([]report.FindingCard, catalog.Catalog) (findingChoice, bool)
		problem func(problemAsk) (problemAnswer, bool)
		usage   func(catalog.Catalog) (usageWindowAnswer, func())
	}{systemLang, detectProjects, pickFolder, openReportFile, closeDelay, sendUsageFn, runCanAsk, runInput, version, connectOfferHook, dismissUIHook, problemUIHook, usageWindowHook}
	t.Cleanup(func() {
		systemLang, detectProjects, pickFolder, openReportFile, closeDelay = prev.lang, prev.detect, prev.pick, prev.open, prev.delay
		sendUsageFn, runCanAsk, runInput, version = prev.send, prev.canAsk, prev.in, prev.version
		connectOfferHook, dismissUIHook, problemUIHook, usageWindowHook = prev.connect, prev.dismiss, prev.problem, prev.usage
	})
	systemLang = func() string { return "en" }
	detectProjects = func() firstrun.Result { return firstrun.Result{} }
	pickFolder = func(string) (string, error) { return "", nil }
	openReportFile = func(string) error { return nil }
	closeDelay = 0
	version = "0.20.0"
	runCanAsk = func() bool { return true }
	runInput = strings.NewReader("")
	sendUsageFn = func(context.Context, spacenet.UsageConsent, []byte) (int, error) {
		t.Error("a test sent usage statistics")
		return 204, nil
	}
	order := []string{}
	connectOfferHook = func(spacePaths, catalog.Catalog) connectResult {
		order = append(order, "C")
		return connectDeclined
	}
	dismissUIHook = func([]report.FindingCard, catalog.Catalog) (findingChoice, bool) {
		order = append(order, "F")
		return findingChoice{}, false
	}
	problemUIHook = func(problemAsk) (problemAnswer, bool) {
		order = append(order, "P")
		return problemAnswer{}, true
	}
	usageWindowHook = func(catalog.Catalog) (usageWindowAnswer, func()) {
		order = append(order, "E")
		return window, nil
	}
	return &order, stPath
}

func joinOrder(o []string) string { return strings.Join(o, ",") }

// §2.1: the installation's first review shows the problem list, if something
// failed, and then only the usage statistics window: no connect offer and no
// «No me afecta» window.
func TestBV286FirstReviewOnlyUsageAfterProblems(t *testing.T) {
	order, _ := bv286Home(t, nil, true, usageWindowLater)
	if code := runNoArgs(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := joinOrder(*order); got != "P,E" {
		t.Errorf("first review: %s, want P,E", got)
	}
	// Without a problem, the usage window is the only one.
	order, _ = bv286Home(t, nil, false, usageWindowLater)
	if code := runNoArgs(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := joinOrder(*order); got != "E" {
		t.Errorf("first review without a problem: %s, want E", got)
	}
}

// §2.2: in the following reviews, while there is no answer, the usage window
// comes first, then what the double click showed before.
func TestBV286NextReviewUsageFirst(t *testing.T) {
	order, _ := bv286Home(t, nil, true, usageWindowLater)
	seedEarlierReview(t)
	if code := runNoArgs(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	got := joinOrder(*order)
	if got != "E,C,P" && got != "E,C,F,P" {
		t.Errorf("next review: %s, want E,C,(F,)P", got)
	}
}

// §6.6 (advisor): a first review with usage statistics already answered
// shows only the problem list, if there is one.
func TestBV286FirstReviewAlreadyAnswered(t *testing.T) {
	answered := &state.Usage{Enabled: false, DecidedAt: "2026-11-23T10:00:00Z", TextVersion: usageTextVersion, InstallID: "3b2c1d4e-5f60-4a7b-8c9d-0e1f2a3b4c5d"}
	order, _ := bv286Home(t, answered, true, usageWindowLater)
	if code := runNoArgs(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := joinOrder(*order); got != "P" {
		t.Errorf("first review, already answered: %s, want P", got)
	}
}

// §2.3: with an answer to the current text, the window does not come back.
func TestBV286AnsweredNoWindow(t *testing.T) {
	answered := &state.Usage{Enabled: false, DecidedAt: "2026-11-23T10:00:00Z", TextVersion: usageTextVersion, InstallID: "3b2c1d4e-5f60-4a7b-8c9d-0e1f2a3b4c5d"}
	order, _ := bv286Home(t, answered, false, usageWindowLater)
	seedEarlierReview(t)
	if code := runNoArgs(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := joinOrder(*order); strings.Contains(got, "E") {
		t.Errorf("answered: %s, want no usage window", got)
	}
}

// §6.7: TextVersion is 2, so an answer to the earlier text (1) is asked again,
// once, with the window.
func TestBV286OldAnswerAskedAgain(t *testing.T) {
	if usageTextVersion != 2 {
		t.Fatalf("usage.TextVersion = %d, want 2 (BV-286, owner's decision)", usageTextVersion)
	}
	old := &state.Usage{Enabled: true, DecidedAt: "2026-11-18T10:00:00Z", TextVersion: 1, InstallID: "3b2c1d4e-5f60-4a7b-8c9d-0e1f2a3b4c5d"}
	order, _ := bv286Home(t, old, false, usageWindowLater)
	seedEarlierReview(t)
	if code := runNoArgs(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if got := *order; len(got) == 0 || got[0] != "E" {
		t.Errorf("an answer to text 1: %v, want the usage window first", got)
	}
}

// `rosmarops run` from a terminal: the first review asks after the problem
// list; the following ones ask first. run never shows C or F.
func TestBV286RunOrder(t *testing.T) {
	for _, tc := range []struct {
		name    string
		earlier bool
		want    string
	}{{"first", false, "P,E"}, {"next", true, "E,P"}} {
		t.Run(tc.name, func(t *testing.T) {
			order, _ := bv286Home(t, nil, true, usageWindowLater)
			if tc.earlier {
				seedEarlierReview(t)
			}
			if err := runRun(cli.Invocation{Path: []string{"run"}, Flags: map[string]string{"lang": "en"}}); err != nil {
				t.Fatal(err)
			}
			if got := joinOrder(*order); got != tc.want {
				t.Errorf("run, %s review: %s, want %s", tc.name, got, tc.want)
			}
		})
	}
}

// The four answers of the window (§2): «Yes, help» turns them on and sends
// this review's summary, then the window is let close (finish runs after the
// send); «No, thanks» turns them off and sends nothing; the X, Esc or no
// answer (later) record nothing and send nothing; no window falls back to the
// console question.
func TestBV286WindowAnswers(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ans      usageWindowAnswer
		console  string
		wantOn   *bool
		wantSent int
	}{
		{"yes", usageWindowYes, "", ptrBool(true), 1},
		{"no", usageWindowNo, "", ptrBool(false), 0},
		{"later", usageWindowLater, "y\n", nil, 0},
		{"no window, console yes", usageWindowNotShown, "y\n", ptrBool(true), 1},
		{"no window, console Enter", usageWindowNotShown, "\n", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stPath, sends := usageTestHome(t, nil, tc.console)
			prevHook := usageWindowHook
			t.Cleanup(func() { usageWindowHook = prevHook })
			finished, sentBeforeFinish := false, -1
			usageWindowHook = func(catalog.Catalog) (usageWindowAnswer, func()) {
				return tc.ans, func() { finished, sentBeforeFinish = true, *sends }
			}
			afterReportUsage(t.TempDir(), "en", true)
			u := loadUsage(t, stPath)
			switch {
			case tc.wantOn == nil && u != nil:
				t.Errorf("recorded %+v, want nothing", u)
			case tc.wantOn != nil && (u == nil || u.Enabled != *tc.wantOn):
				t.Errorf("recorded %+v, want enabled=%v", u, *tc.wantOn)
			}
			if *sends != tc.wantSent {
				t.Errorf("%d sends, want %d", *sends, tc.wantSent)
			}
			if !finished {
				t.Error("the window was not let close")
			}
			if tc.ans == usageWindowYes && sentBeforeFinish != 1 {
				t.Errorf("the summary was sent after the window closed (sent %d before)", sentBeforeFinish)
			}
		})
	}
}

func ptrBool(b bool) *bool { return &b }

// Never (§2): no window, and no attempt to open one, without a console, in a
// scheduled review (the caller passes canAskNow false) or in a build that is
// not a published version.
func TestBV286NeverOpensWindow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		version string
		canAsk  bool
	}{{"no console or scheduled", "0.20.0", false}, {"dev build", "0.1.0-dev", true}, {"journeys build", "0.20.0-journeys.1", true}} {
		t.Run(tc.name, func(t *testing.T) {
			stPath, sends := usageTestHome(t, nil, "y\n")
			version = tc.version
			prevHook := usageWindowHook
			t.Cleanup(func() { usageWindowHook = prevHook })
			opened := 0
			usageWindowHook = func(catalog.Catalog) (usageWindowAnswer, func()) { opened++; return usageWindowYes, nil }
			afterReportUsage(t.TempDir(), "en", tc.canAsk)
			if opened != 0 || *sends != 0 || loadUsage(t, stPath) != nil {
				t.Errorf("window opened %d times, %d sends", opened, *sends)
			}
		})
	}
}

// The console asks with the window's texts, in the brief's order: title, why,
// body, never, check, off, and then the question.
func TestBV286ConsoleTextOrder(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		askUsage(strings.NewReader("\n"), &out, cat)
		s, at := out.String(), -1
		for _, k := range []string{"usage.ask.title", "usage.ask.why", "usage.ask.body", "usage.ask.never", "usage.ask.check", "usage.ask.off", "usage.ask.prompt"} {
			i := strings.Index(s, cat.Text(k, nil))
			if i <= at {
				t.Errorf("%s: %s out of order in %q", lang, k, s)
			}
			at = i
		}
	}
}

// The window's protocol: SHOWN, then yes, no or later. A stream that ends
// before SHOWN is no window (the console asks); one that ends after it with no
// answer is later; nothing counts before SHOWN.
func TestBV286ReadUsageWindow(t *testing.T) {
	for in, want := range map[string]usageWindowAnswer{
		"SHOWN\nyes\n":                   usageWindowYes,
		"SHOWN\r\nno\r\n":                usageWindowNo,
		"SHOWN\nCLIENT=480,300\nlater\n": usageWindowLater,
		"SHOWN\n":                        usageWindowLater,
		"":                               usageWindowNotShown,
		"yes\n":                          usageWindowNotShown,
	} {
		if got := <-readUsageWindow(strings.NewReader(in)); got != want {
			t.Errorf("%q: %v, want %v", in, got, want)
		}
	}
}

// Nobody answers within the limit: the window is ended and it counts as the X.
func TestBV286AwaitUsageWindowTimeout(t *testing.T) {
	done := make(chan usageWindowAnswer, 1)
	fire := make(chan time.Time, 1)
	killed := false
	var gotLimit time.Duration
	after := func(d time.Duration) <-chan time.Time { gotLimit = d; fire <- time.Time{}; return fire }
	kill := func() { killed = true; done <- usageWindowLater }
	if got := awaitUsageWindow(done, usageAnswerLimit, after, kill); got != usageWindowLater || !killed {
		t.Errorf("timeout: %v killed %v", got, killed)
	}
	if gotLimit != 10*time.Minute {
		t.Errorf("answer limit %v, want 10 minutes", gotLimit)
	}
	if usageThanksLimit <= 60*time.Second {
		t.Errorf("thanks limit %v does not leave the 60-second thank-you time to close", usageThanksLimit)
	}
}

// With the usage window's self-test hook set, its SHOWN line is logged with
// the window's name (the journeys check it) and the answer still counts;
// without the hook nothing is logged.
func TestBV286UsageSelfTestShownLogged(t *testing.T) {
	log := captureLog(t)
	t.Setenv(usageSelfTestEnv, "1")
	if got := <-readUsageWindow(strings.NewReader("SHOWN\nCLIENT=480,300\nlater\n")); got != usageWindowLater {
		t.Fatalf("answer %v, want later", got)
	}
	if !strings.Contains(log.String(), "window self-test: shown") || !strings.Contains(log.String(), "window=usage") {
		t.Fatalf("log has no self-test line for the usage window:\n%s", log)
	}
	log.Reset()
	t.Setenv(usageSelfTestEnv, "")
	<-readUsageWindow(strings.NewReader("SHOWN\nlater\n"))
	if strings.Contains(log.String(), "self-test") {
		t.Fatalf("without the hook nothing is logged, got:\n%s", log)
	}
}

// The window script, on every OS: no default or cancel button, the keys
// previewed (Esc closes, Enter and the space bar do nothing until Tab), no
// button with the focus when it opens, the 60-second thank-you, and its three
// answers. The real window is checked on Windows (TestUsageWindowSelfTest).
func TestBV286UsageWindowScript(t *testing.T) {
	for _, want := range []string{
		"$form.AcceptButton = $null", "$form.CancelButton = $null", "$form.KeyPreview = $true",
		"$form.ActiveControl = $null", "$thanksTimer.Interval = 60000",
		"WriteLine('yes')", "WriteLine('no')", "WriteLine('later')", "WriteLine('SHOWN')",
		"[System.Windows.Forms.Keys]::Escape", "(Px 480)", "(Px 424)", "(Px 28)", "'#2346B8'",
	} {
		if !strings.Contains(usageWindowScript, want) {
			t.Errorf("usageWindowScript lacks %q", want)
		}
	}
	for _, bad := range []string{"AcceptButton = $yes", "CancelButton = $no"} {
		if strings.Contains(usageWindowScript, bad) {
			t.Errorf("usageWindowScript has %q", bad)
		}
	}
}

// session-check and statusline (the Claude Code plugin) never reach the
// usage question or its window: their source calls neither.
func TestBV286AgentCommandsNeverAsk(t *testing.T) {
	src, err := os.ReadFile("cmd_agenthooks.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"afterReportUsage", "usageWindowHook", "askUsageWindow", "askUsage("} {
		if strings.Contains(string(src), name) {
			t.Errorf("cmd_agenthooks.go calls %s", name)
		}
	}
}
