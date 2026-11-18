// Command statusline-seed leaves a last-review line in a reports data folder
// exactly as `rosmarops run` does (agenthooks.WriteStatusline, English and
// Spanish), checked an hour ago. A manual helper for the measure-plugin
// workflow (BV-254): session-check then has a latest result to pass to Claude
// Code. Never part of the program.
//
//	go run ./tests/manual/statusline-seed -data ~/.rosmarops/reports/data
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/qtorb/rosmarops/internal/agenthooks"
	"github.com/qtorb/rosmarops/internal/catalog"
)

func main() {
	data := flag.String("data", "", "reports data folder")
	flag.Parse()
	lines := map[string]string{}
	for _, l := range catalog.Langs() {
		c, err := catalog.Load(l)
		if err != nil {
			fmt.Fprintln(os.Stderr, "statusline-seed:", err)
			os.Exit(1)
		}
		lines[l] = agenthooks.StatuslineText(c, 1, 2, 64)
	}
	if err := agenthooks.WriteStatusline(*data, time.Now().Add(-time.Hour), lines); err != nil {
		fmt.Fprintln(os.Stderr, "statusline-seed:", err)
		os.Exit(1)
	}
}
