// Command p2-template prints Claude Code's P2 template «Sandboxed development»
// (internal/profiles, CTO-58 §1.1, BV-254) exactly as the program gives it:
// the user's settings block, its only text. A manual helper for the
// measure-plugin workflow, never part of the program.
//
//	go run ./tests/manual/p2-template > ~/.claude/settings.json
package main

import (
	"fmt"

	"github.com/qtorb/rosmarops/internal/profiles"
)

func main() {
	fmt.Println(profiles.Block(profiles.P2))
}
