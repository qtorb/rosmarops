package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/problems"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
)

func enCat(t *testing.T) catalog.Catalog {
	t.Helper()
	c, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestComposeSummary(t *testing.T) {
	cat := enCat(t)
	one := composeSummary(cat, []problems.Entry{{Code: problems.ConfigUnreadable, Step: problems.StepAgents, Agent: "cline", Day: "2026-11-09", Count: 1}})
	// CTO-32 N3: the summary names the agent by its product name, not its id.
	if !strings.Contains(one, "Cline") {
		t.Errorf("single config_unreadable summary should name the agent: %q", one)
	}
	// github-copilot shows as «GitHub Copilot»; the raw id never appears.
	cop := composeSummary(cat, []problems.Entry{{Code: problems.ConfigUnreadable, Step: problems.StepAgents, Agent: "github-copilot", Day: "2026-11-09", Count: 1}})
	if !strings.Contains(cop, "GitHub Copilot") || strings.Contains(cop, "github-copilot") {
		t.Errorf("config_unreadable should show the product name, not the id: %q", cop)
	}
	// The value that would be sent still carries the identifier, not the name.
	sent := previewJSON("en", "0.10.3", []problems.Entry{{Code: problems.ConfigUnreadable, Step: problems.StepAgents, Agent: "github-copilot", Day: "2026-11-09", Count: 1}})
	if !strings.Contains(sent, `"agent": "github-copilot"`) || strings.Contains(sent, "GitHub Copilot") {
		t.Errorf("the sent body must carry the id, not the display name: %s", sent)
	}
	many := composeSummary(cat, []problems.Entry{
		{Code: problems.SpaceRejected, Step: problems.StepSpaceSend, Day: "2026-11-09", Count: 1},
		{Code: problems.StepFailed, Step: "mcp", Day: "2026-11-09", Count: 1},
	})
	if !strings.Contains(many, "2") {
		t.Errorf("several summary should carry the count: %q", many)
	}
}

// TestOneAskPerSession is CTO-33 condition 2: with Window B shown at startup
// (the previous review did not finish) and answered «Not now», the after-report
// step must not ask again — problemUIHook is called exactly once this run.
func TestOneAskPerSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	stPath, err := state.StatePath()
	if err != nil {
		t.Fatal(err)
	}
	st, err := state.DefaultState("0.11.0")
	if err != nil {
		t.Fatal(err)
	}
	st.SendProblemsWithoutAsking = false // checkbox off: Window B and Window A both ask
	if err := os.MkdirAll(filepath.Dir(stPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := state.SaveState(stPath, st); err != nil {
		t.Fatal(err)
	}
	reports := filepath.Join(home, ".rosmarops", "reports")
	dataDir := report.DataDir(reports)
	seedNotebook(t, dataDir) // unsent problems exist, so a prompt would be shown

	calls := 0
	orig := problemUIHook
	t.Cleanup(func() { problemUIHook = orig })
	problemUIHook = func(problemAsk) (problemAnswer, bool) { calls++; return problemAnswer{send: false}, true } // «Not now»

	problemsAskedThisRun = false
	t.Cleanup(func() { problemsAskedThisRun = false })

	// Window B asks at startup, then marks the run asked (as cmd_run.go does).
	offerProblems(dataDir, "en", "0.11.0", true /*windowB*/, true, &st, stPath, time.Now())
	problemsAskedThisRun = true

	// After the report: must not ask again.
	offerProblemsAfterReport(reports, "en", true)
	if calls != 1 {
		t.Fatalf("Window B «Not now»: problemUIHook must be called once per run, got %d", calls)
	}

	// Guard proof: without the run flag, the after-report step does ask.
	problemsAskedThisRun = false
	offerProblemsAfterReport(reports, "en", true)
	if calls != 2 {
		t.Fatalf("with no prior ask this run, the after-report step should ask, got %d", calls)
	}
}

func stubProblemUI(t *testing.T, ans problemAnswer, asked bool) {
	t.Helper()
	orig := problemUIHook
	t.Cleanup(func() { problemUIHook = orig })
	problemUIHook = func(problemAsk) (problemAnswer, bool) { return ans, asked }
}

func TestOfferProblemsNobodyPresentLeaves(t *testing.T) {
	dir := t.TempDir()
	seedNotebook(t, dir)
	bodies := withLocalProblemsServer(t, 204)
	problemUIHook = func(problemAsk) (problemAnswer, bool) {
		t.Fatal("nobody present: must not ask")
		return problemAnswer{}, false
	}
	t.Cleanup(func() { problemUIHook = askProblem })

	st := &state.State{}
	offerProblems(dir, "en", "0.10.3", false, false /*present*/, st, "", time.Now())

	if len(*bodies) != 0 {
		t.Fatalf("nobody present: nothing should be sent, got %d", len(*bodies))
	}
	left, _ := problems.New(dir).Unsent(problems.MaxPerSend)
	if len(left) != 2 {
		t.Fatalf("problems should remain unsent, got %d", len(left))
	}
}

func TestOfferProblemsAsksAndSends(t *testing.T) {
	dir := t.TempDir()
	seedNotebook(t, dir)
	bodies := withLocalProblemsServer(t, 204)
	stubProblemUI(t, problemAnswer{send: true, comment: "x", email: "a@b.co"}, true)

	st := &state.State{}
	offerProblems(dir, "en", "0.10.3", false, true, st, "", time.Now())

	if len(*bodies) != 1 {
		t.Fatalf("send answer should send once, got %d", len(*bodies))
	}
	// Comment and email reached the body.
	body := string((*bodies)[0])
	if !strings.Contains(body, "a@b.co") || !strings.Contains(body, `"comment":"x"`) {
		t.Fatalf("comment/email should be in the body: %s", body)
	}
}

func TestOfferProblemsNotNowLeavesButRemembers(t *testing.T) {
	dir := t.TempDir()
	seedNotebook(t, dir)
	bodies := withLocalProblemsServer(t, 204)
	stubProblemUI(t, problemAnswer{send: false, remember: true}, true)

	stPath := t.TempDir() + "/state.json"
	st, _ := state.DefaultState("0.10.3")
	offerProblems(dir, "en", "0.10.3", false, true, &st, stPath, time.Now())

	if len(*bodies) != 0 {
		t.Fatalf("Not now: nothing sent, got %d", len(*bodies))
	}
	// The checkbox was saved for next time.
	if got, err := state.LoadState(stPath); err != nil || !got.SendProblemsWithoutAsking {
		t.Fatalf("remember should persist the checkbox: %v %+v", err, got)
	}
}
