package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"runtime"
	"strconv"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/problems"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/spacenet"
	"github.com/qtorb/rosmarops/internal/state"
)

// problemAsk is everything the «send a problem?» prompt needs to draw. All text
// is resolved from the catalog in the caller's language; the window and the
// terminal only render strings.
type problemAsk struct {
	cat     catalog.Catalog
	windowB bool   // B (startup, last review unfinished) vs A (end of a review)
	summary string // the composed {summary} line
	preview string // the exact JSON that would be sent («See what is sent»)
}

// problemAnswer is what the person chose.
type problemAnswer struct {
	send     bool
	comment  string
	email    string
	remember bool // tick «Send the next ones without asking»
}

// problemUIHook is the seam for the prompt: production is askProblem (a window
// on Windows, a terminal question on macOS/Linux); tests replace it. The bool
// is whether the person was actually asked (false when there is no interactive
// surface). TestMain neutralizes it so the headless test suite never launches a
// real window (as it does for the connect window).
var problemUIHook = askProblem

// showProblemOutcomeFn is the seam for the «Sent»/«couldn't be sent» message
// shown after a send (a message box on Windows). TestMain neutralizes it too,
// so a test that answers «Send» does not pop a real window.
var showProblemOutcomeFn = showProblemOutcome

// problemUnmarkHook is the seam for the «we sent {n}, keep sending or ask me
// again?» box shown after an automatic send with someone present (CTO-32 C4.1,
// a window on Windows; macOS/Linux has no box and returns false). It returns
// true when the person chose «Ask me before sending». TestMain neutralizes it.
var problemUnmarkHook = showProblemAutoSent

// problemsAutoSentThisRun is how many problems the checkbox sent this run, set
// by runWith before the report is written and read by the after-report step so
// the untick box can show the count. Reset at the start of every run.
var problemsAutoSentThisRun int

// offerProblems asks whether to send the unsent problems and acts on the answer
// (BV-210 §2, Window A/B or the terminal question). It is ask-only: the
// automatic send when the checkbox is on happens in runWith, not here, so the
// one «Send» consent lives in a single place (CTO-32 C1/C4). The caller only
// reaches here with someone present and the checkbox off. On «Send» it sends
// under ConsentFromSendButton and shows the outcome; «remember» ticks the
// checkbox. It never fails the run. windowB selects the «last check didn't
// finish» text.
func offerProblems(dataDir, lang, version string, windowB, present bool, st *state.State, stPath string, now time.Time) {
	if !present {
		return
	}
	nb := problems.New(dataDir)
	unsent, err := nb.Unsent(problems.MaxPerSend)
	if err != nil || len(unsent) == 0 {
		return
	}
	cat, err := catalog.Load(lang)
	if err != nil {
		slog.Warn("problems prompt: catalog", "err", err)
		return
	}

	ask := problemAsk{
		cat:     cat,
		windowB: windowB,
		summary: composeSummary(cat, unsent),
		preview: previewJSON(lang, version, unsent),
	}
	ans, asked := problemUIHook(ask)
	if !asked {
		return
	}

	// Remember the checkbox regardless of this time's choice: it governs the
	// next reviews.
	if ans.remember && st != nil {
		st.SendProblemsWithoutAsking = true
		if err := state.SaveState(stPath, *st); err != nil {
			slog.Warn("problems prompt: save checkbox", "err", err)
		}
	}
	if !ans.send {
		return
	}

	var comment, email *string
	if ans.comment != "" {
		comment = &ans.comment
	}
	if ans.email != "" {
		email = &ans.email
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	// CTO-32 C1: the «Send» button is the one place that calls
	// ConsentFromSendButton (a trust test enforces it).
	res := submitProblems(ctx, spacenet.ConsentFromSendButton(), dataDir, lang, version, comment, email, now)
	cancel()

	// Tell the person the outcome (§3: «Sent. Thank you…» / «It couldn't be
	// sent…»), a message box on Windows and a console line elsewhere.
	showProblemOutcomeFn(cat, res.Delivered, ans.email)
}

// offerProblemsAfterReport is the end-of-review problems step, run by the caller
// after the report has opened (CTO-32 C3), so an unanswered window never delays
// the report. present is whether someone is at the machine. With the checkbox
// off it asks (Window A); with the checkbox on it offers to untick it after the
// automatic send (CTO-32 C4.1). It never fails the run.
func offerProblemsAfterReport(reportDir, lang string, present bool) {
	if !present {
		return
	}
	dataDir := report.DataDir(reportDir)
	stPath, err := state.StatePath()
	if err != nil {
		return
	}
	st, err := state.LoadState(stPath)
	if err != nil {
		return
	}
	if st.SendProblemsWithoutAsking {
		if problemsAutoSentThisRun <= 0 {
			return
		}
		cat, err := catalog.Load(lang)
		if err != nil {
			return
		}
		if problemUnmarkHook(cat, problemsAutoSentThisRun) {
			st.SendProblemsWithoutAsking = false
			if err := state.SaveState(stPath, st); err != nil {
				slog.Warn("problems prompt: untick checkbox", "err", err)
			}
		}
		return
	}
	offerProblems(dataDir, lang, version, false, true, &st, stPath, time.Now())
}

// presentForProblems reports whether someone is at the machine to be asked: an
// interactive run or the double-click window, never a scheduled review.
func presentForProblems(m runMode, unattended bool) bool {
	return !unattended && (m.window || runCanAsk())
}

// composeSummary builds the {summary} line (§3/§10): the per-code summary for a
// single problem, or «{n} problems, including {first}» for several.
func composeSummary(cat catalog.Catalog, unsent []problems.Entry) string {
	if len(unsent) == 0 {
		return ""
	}
	first := summaryOf(cat, unsent[0])
	if len(unsent) == 1 {
		return first
	}
	return cat.Text("problems.summary.several", map[string]string{
		"n":     strconv.Itoa(len(unsent)),
		"first": first,
	})
}

// summaryOf is the one-problem summary; config_unreadable carries the agent.
func summaryOf(cat catalog.Catalog, e problems.Entry) string {
	p := map[string]string{}
	if e.Agent != "" {
		p["agent"] = e.Agent
	}
	return cat.Text("problems.summary."+string(e.Code), p)
}

// previewJSON is the exact body that would be sent, pretty-printed for «See what
// is sent». It carries no comment or email (those are added only if the person
// writes them) and nothing that identifies the machine.
func previewJSON(lang, version string, unsent []problems.Entry) string {
	sys := problems.System{Lang: lang, Version: version, OS: runtime.GOOS, OSVersion: osVersionFn(), Arch: runtime.GOARCH}
	b, err := json.MarshalIndent(problems.BuildReport(sys, unsent, nil, nil), "", "  ")
	if err != nil {
		return ""
	}
	return string(b)
}
