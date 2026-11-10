//go:build !windows

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// problemTerminalTimeout caps the terminal question (CTO-32 C3): if nobody
// answers within it, the prompt ends as «Not now», so a review is never blocked
// waiting for input. It matches the Windows window's 15-minute cap.
const problemTerminalTimeout = 15 * time.Minute

// askProblem is the macOS/Linux «send a problem?» question (BV-210 §2): only in
// an interactive terminal. It returns the answer and whether the person was
// asked.
func askProblem(a problemAsk) (problemAnswer, bool) {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return problemAnswer{}, false
	}
	return askProblemTerminal(a, os.Stdin, os.Stdout, problemTerminalTimeout)
}

// askProblemTerminal drives the question on the given reader and writer, ending
// as «Not now» if there is no answer within timeout (CTO-32 C3). The timeout and
// the streams are parameters so the cap is testable without a real terminal.
func askProblemTerminal(a problemAsk, in io.Reader, out io.Writer, timeout time.Duration) (problemAnswer, bool) {
	type result struct {
		ans   problemAnswer
		asked bool
	}
	done := make(chan result, 1)
	go func() {
		r := bufio.NewReader(in)

		titleKey, textKey := "problems.a.title", "problems.a.text"
		if a.windowB {
			titleKey, textKey = "problems.b.title", "problems.b.text"
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, a.cat.Text(titleKey, nil))
		fmt.Fprintln(out, a.cat.Text(textKey, map[string]string{"summary": a.summary}))
		fmt.Fprintln(out)

		// «See what is sent»: show the exact JSON on request.
		fmt.Fprint(out, a.cat.Text("problems.see", nil)+" ["+sendHint(a.cat)+"] ")
		if yes(a.cat, readLine(r)) {
			fmt.Fprintln(out, a.preview)
		}

		// Send or not (CTO-32 C4.4: «¿Enviar? [s/N]» / «Send? [y/N]»).
		fmt.Fprint(out, a.cat.Text("problems.terminal.send", nil)+" ")
		ans := problemAnswer{send: yes(a.cat, readLine(r))}

		if ans.send {
			fmt.Fprintln(out, a.cat.Text("problems.comment.q", nil))
			ans.comment = strings.TrimSpace(readLine(r))
			fmt.Fprintln(out, a.cat.Text("problems.email.q", nil))
			ans.email = strings.TrimSpace(readLine(r))
		}

		fmt.Fprint(out, a.cat.Text("problems.remember", nil)+" ["+sendHint(a.cat)+"] ")
		ans.remember = yes(a.cat, readLine(r))
		done <- result{ans, true}
	}()

	select {
	case r := <-done:
		return r.ans, r.asked
	case <-time.After(timeout):
		// No answer in time: «Not now» — asked, nothing sent, nothing remembered.
		return problemAnswer{}, true
	}
}

// showProblemOutcome prints the «Sent»/«couldn't be sent» line (§3).
func showProblemOutcome(cat catalog.Catalog, delivered bool, email string) {
	if !isTerminal(os.Stdout) {
		return
	}
	if !delivered {
		fmt.Fprintln(os.Stdout, cat.Text("problems.fail", nil))
		return
	}
	line := cat.Text("problems.sent", nil)
	if email != "" {
		line += " " + cat.Text("problems.sent.email", map[string]string{"email": email})
	}
	fmt.Fprintln(os.Stdout, line)
}

// showProblemAutoSent is the «we sent {n}; keep sending or ask again?» box
// (CTO-32 C4.1). It is a window only on Windows; on macOS/Linux there is no box
// and the report line tells the person to run «rosmarops problems ask», so this
// returns false (keep the checkbox). _ keeps the shared signature.
func showProblemAutoSent(_ catalog.Catalog, _ int) bool { return false }

func readLine(r *bufio.Reader) string {
	s, _ := r.ReadString('\n')
	return strings.TrimRight(s, "\r\n")
}

// yes reports whether s is an affirmative in the catalog's language: «y» for
// English, «s» for Spanish (CTO-32 C4.4). Any other answer is «Not now».
func yes(cat catalog.Catalog, s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "s", "sí", "si":
		return cat.Lang() == "es"
	case "y", "yes":
		return cat.Lang() != "es"
	}
	return false
}

// sendHint is the y/N hint in the catalog's language.
func sendHint(cat catalog.Catalog) string {
	if cat.Lang() == "es" {
		return "s/N"
	}
	return "y/N"
}
