//go:build !windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// askProblem is the macOS/Linux «send a problem?» question (BV-210 §2): only in
// an interactive terminal. It prints the title and text, lets the person see
// exactly what would be sent, then asks whether to send, with an optional
// comment and email and the «send the next ones without asking» checkbox. It
// returns the answer and whether the person was asked. The pure prompt wiring
// is small and lives here; the compose/preview logic is platform-agnostic
// (problemprompt.go), tested on every platform.
func askProblem(a problemAsk) (problemAnswer, bool) {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return problemAnswer{}, false
	}
	out := os.Stdout
	in := bufio.NewReader(os.Stdin)

	titleKey, textKey := "problems.a.title", "problems.a.text"
	if a.windowB {
		titleKey, textKey = "problems.b.title", "problems.b.text"
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, a.cat.Text(titleKey, nil))
	fmt.Fprintln(out, a.cat.Text(textKey, map[string]string{"summary": a.summary}))
	fmt.Fprintln(out)

	// «See what is sent»: show the exact JSON on request.
	fmt.Fprint(out, a.cat.Text("problems.see", nil)+" ["+yesNoHint(a.cat)+"] ")
	if yes(readLine(in)) {
		fmt.Fprintln(out, a.preview)
	}

	// Send or not.
	fmt.Fprint(out, a.cat.Text("problems.send", nil)+"? ["+yesNoHint(a.cat)+"] ")
	ans := problemAnswer{send: yes(readLine(in))}

	if ans.send {
		fmt.Fprintln(out, a.cat.Text("problems.comment.q", nil))
		ans.comment = strings.TrimSpace(readLine(in))
		fmt.Fprintln(out, a.cat.Text("problems.email.q", nil))
		ans.email = strings.TrimSpace(readLine(in))
	}

	fmt.Fprint(out, a.cat.Text("problems.remember", nil)+" ["+yesNoHint(a.cat)+"] ")
	ans.remember = yes(readLine(in))
	return ans, true
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

func readLine(r *bufio.Reader) string {
	s, _ := r.ReadString('\n')
	return strings.TrimRight(s, "\r\n")
}

// yes reports whether s is an affirmative answer in either language (y/yes/s/si).
func yes(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "y", "yes", "s", "si", "sí":
		return true
	}
	return false
}

func yesNoHint(cat catalog.Catalog) string {
	// A fixed y/N hint; the catalog words are for buttons, not the terminal hint.
	_ = cat
	return "y/N"
}
