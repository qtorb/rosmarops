package main

import (
	"fmt"
	"os"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/state"
)

// newFindingsCmd is `rosmarops findings` (BV-184): it opens the «No me afecta»
// window (Windows) or the console list (macOS/Linux) with the last review's
// findings, at any time, so the person can dismiss findings or recover ones
// they dismissed before — without waiting for a new finding.
func newFindingsCmd() *cli.Command {
	return &cli.Command{
		Name: "findings", Short: "cli.cmd.findings.short", Long: "cli.cmd.findings.long", Args: cli.AnyArgs,
		Flags: []cli.Flag{
			{Name: "lang", Default: systemLang(), Usage: "cli.flag.lang"},
		},
		Before: integrityCheck,
		Run:    runFindings,
	}
}

func runFindings(inv cli.Invocation) error {
	cfgPath, err := config.ResolveConfigPath()
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	// CTO-49 v2: a configuration outside ~/.rosmarops (a ./rosmarops.config.json
	// in the working directory) is never changed: say so and write nothing.
	if outside, _ := dismissConfigOutside(cfgPath); outside {
		cat, err := catalog.Load(inv.Flags["lang"])
		if err != nil {
			cat, _ = catalog.Load(catalog.DefaultLang)
		}
		fmt.Fprintln(os.Stdout, cat.Text("findings.config_outside", map[string]string{"path": cfgPath}))
		return nil
	}
	stPath, err := state.StatePath()
	if err != nil {
		return fmt.Errorf("resolve state path: %w", err)
	}
	// `rosmarops findings` always shows (no «only when new» gate).
	offerDismissAfterReport(inv.Flags["lang"], cfgPath, stPath, false, time.Now())
	return nil
}
