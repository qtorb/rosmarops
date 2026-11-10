package main

import (
	"fmt"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/state"
)

// newProblemsCmd is `rosmarops problems` (BV-210, CTO-32 C4): its one
// subcommand, `ask`, unticks «Send the next ones without asking» so the program
// asks again before sending any problem. It is how the person undoes the
// checkbox without editing state.json by hand.
func newProblemsCmd() *cli.Command {
	return &cli.Command{
		Name: "problems", Short: "cli.cmd.problems.short",
		Subs: []*cli.Command{
			{
				Name: "ask", Short: "cli.cmd.problems.ask.short", Args: cli.AnyArgs,
				Flags:  []cli.Flag{{Name: "lang", Default: systemLang(), Usage: "cli.flag.lang"}},
				Before: integrityCheck,
				Run:    runProblemsAsk,
			},
		},
	}
}

// runProblemsAsk sets send_problems_without_asking to false and confirms it
// (CTO-32 C4.3). A missing or unreadable state is not an error: there is then
// no checkbox to clear, and the message still tells the person what holds.
func runProblemsAsk(inv cli.Invocation) error {
	cat, err := catalog.Load(inv.Flags["lang"])
	if err != nil {
		return fmt.Errorf("load catalog: %w", err)
	}
	stPath, err := state.StatePath()
	if err != nil {
		return fmt.Errorf("resolve state path: %w", err)
	}
	if st, err := state.LoadState(stPath); err == nil {
		if st.SendProblemsWithoutAsking {
			st.SendProblemsWithoutAsking = false
			if err := state.SaveState(stPath, st); err != nil {
				return fmt.Errorf("save state: %w", err)
			}
		}
	}
	fmt.Println(cat.Text("problems.ask.done", nil))
	return nil
}
