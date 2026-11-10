package main

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/problems"
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
	if !strings.Contains(one, "cline") {
		t.Errorf("single config_unreadable summary should name the agent: %q", one)
	}
	many := composeSummary(cat, []problems.Entry{
		{Code: problems.SpaceRejected, Step: problems.StepSpaceSend, Day: "2026-11-09", Count: 1},
		{Code: problems.StepFailed, Step: "mcp", Day: "2026-11-09", Count: 1},
	})
	if !strings.Contains(many, "2") {
		t.Errorf("several summary should carry the count: %q", many)
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
