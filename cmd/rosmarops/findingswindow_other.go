//go:build !windows

package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/report"
)

// showDismiss is the macOS/Linux «No me afecta» list (BV-184 point 5): only in
// an interactive terminal. It prints the question (W1, W2) and a numbered line per
// finding, reads the numbers to change («3» dismissed or pending, «3a» accepted
// or pending; BV-236 §5; «3c» a key saved in git changed or pending, BV-314), and returns both lists and whether the person answered. Enter with nothing changes
// nothing (it is not saved). The pure list and toggle logic lives in
// findings.go (consoleFindingLine, applyConsoleToggles), tested on every
// platform.
func showDismiss(cards []report.FindingCard, cat catalog.Catalog) (findingChoice, bool) {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return findingChoice{}, false
	}
	out := os.Stdout
	fmt.Fprintln(out, cat.Text("findings.q.accept", nil))    // W1
	fmt.Fprintln(out, cat.Text("findings.text.accept", nil)) // W2
	fmt.Fprintln(out)
	for i, c := range cards {
		fmt.Fprintln(out, consoleFindingLine(i+1, c, cat))
	}
	fmt.Fprint(out, cat.Text("findings.console.prompt.accept", nil)) // K1
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	choice, saved, na := applyConsoleToggles(cards, answer)
	for _, n := range na {
		fmt.Fprintln(out, cat.Text("findings.console.rotated.na", map[string]string{"n": strconv.Itoa(n)})) // BV-314
	}
	return choice, saved
}
