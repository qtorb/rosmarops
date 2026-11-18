// Command p2-template prints Claude Code's P2 template «Sandboxed development»
// (internal/profiles, CTO-58 §1.1) exactly as the program gives it: the user's
// settings block, or with -project the project's. A manual helper for the
// BV-253 §C.3 measurement (measure-plugin workflow), never part of the program.
//
//	go run ./tests/manual/p2-template > ~/.claude/settings.json
//	go run ./tests/manual/p2-template -project > .claude/settings.json
package main

import (
	"flag"
	"fmt"

	"github.com/qtorb/rosmarops/internal/profiles"
)

func main() {
	project := flag.Bool("project", false, "print the project's block")
	flag.Parse()
	if *project {
		fmt.Println(profiles.ProjectBlock(profiles.P2))
		return
	}
	fmt.Println(profiles.Block(profiles.P2))
}
