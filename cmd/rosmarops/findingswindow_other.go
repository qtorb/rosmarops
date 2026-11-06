//go:build !windows

package main

import (
	"bufio"
	"fmt"
	"os"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/report"
)

// showDismiss is the macOS/Linux «No me afecta» list (BV-184 point 5): only in
// an interactive terminal. It prints the question and a numbered line per
// finding, reads the numbers whose state to flip, and returns the ids that end
// up dismissed and whether the person answered. Enter with nothing changes
// nothing (it is not saved). The pure list and toggle logic lives in
// findings.go (consoleFindingLine, applyConsoleToggles), tested on every
// platform.
func showDismiss(cards []report.FindingCard, cat catalog.Catalog) ([]string, bool) {
	if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
		return nil, false
	}
	out := os.Stdout
	fmt.Fprintln(out, cat.Text("findings.q", nil))
	fmt.Fprintln(out, cat.Text("findings.text", nil))
	fmt.Fprintln(out)
	for i, c := range cards {
		fmt.Fprintln(out, consoleFindingLine(i+1, c, cat))
	}
	fmt.Fprint(out, cat.Text("findings.console.prompt", nil))
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return applyConsoleToggles(cards, answer)
}
