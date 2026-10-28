package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
)

// version is the release tag without the «v», set when building a release
// with -ldflags="-X main.version=<version>"; a development build says
// 0.1.0-dev.
var version = "0.1.0-dev"

// zeroHash is the placeholder written by DefaultState before rosmarops baseline runs.
// Integrity fields equal to zeroHash mean "baseline pending" — NOT a mismatch.
const zeroHash = "0000000000000000000000000000000000000000000000000000000000000000"

// Process exit codes. A plain error exits 1; an integrity abort, 2; a
// scheduled review or double click that could not finish, 3 (BV-93, B3) — a
// distinct code so a run that never produced a report is told apart from a
// clean red result and from an integrity abort.
const (
	exitIntegrity = 2
	exitRunFailed = 3
)

// The aborts, as package vars so tests can intercept them. In production both
// write their red report and call os.Exit, so they never return; the explicit
// returns after the call sites are reached only when a test replaces them.
var (
	abortRun       = abortRunFailed
	abortIntegrity = abortWithIntegrityFailure
)

func main() {
	if len(os.Args) == 1 {
		os.Exit(runNoArgs()) // a double click (U1); help, --help and -h still show the help
	}
	p := cli.Program{Name: "rosmarops", Version: version, Lang: systemLang(), Root: newRootCmd(), Stdout: os.Stdout, Stderr: os.Stderr}
	if err := p.Execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// newRootCmd is the command tree. The startup integrity check runs before
// every command except init and baseline: those two are the only ones
// allowed to run without a verified state.
func newRootCmd() *cli.Command {
	return &cli.Command{
		Name: "rosmarops",
		Subs: []*cli.Command{
			newInitCmd(),     // cmd_init.go   — skips integrity check
			newRunCmd(),      // cmd_run.go
			newBaselineCmd(), // cmd_baseline.go — skips integrity check
			newStatusCmd(),   // cmd_status.go
			newAllowCmd(),    // stub → M4
			newScheduleCmd(), // stub → M8
		},
	}
}

// integrityCheck is the Before of every command but init and baseline. It
// runs before the command's Run and validates that neither the binary nor the
// config has been modified since the last `rosmarops baseline`.
//
// Skip conditions (command does NOT check integrity):
//   - "init": state may not exist yet on first run.
//   - "baseline": this command IS the mechanism that sets the baseline.
//
// Baseline-pending condition (zeroHash in state):
//   - If either hash field is zeroHash, the baseline hasn't been set yet.
//     This is not a mismatch — it means `rosmarops baseline` hasn't run.
//     A slog.Warn is emitted but the command proceeds normally.
//
// Mismatch condition:
//   - Computed hash ≠ stored hash (and stored hash ≠ zeroHash) → abort.
//   - Writes a minimal RED report and exits with code 2.
func integrityCheck(path []string) error {
	// run checks its own integrity, with the person's answer when there is
	// a console (decision 2026-10-26, integridad y revisión semanal, J1–J3).
	if path[0] == "init" || path[0] == "baseline" || path[0] == "run" {
		return nil
	}

	stPath, err := state.StatePath()
	if err != nil {
		// Can't resolve state path: non-fatal for the integrity check.
		// The command itself will handle a missing state.
		slog.Warn("integrity check: could not resolve state path", "err", err)
		return nil
	}

	st, err := state.LoadState(stPath)
	if err != nil {
		// State unreadable or missing: skip integrity check.
		// This handles the case where the user hasn't run rosmarops init yet.
		slog.Warn("integrity check: state unavailable, skipping", "err", err)
		return nil
	}

	// Check binary hash.
	if st.Integrity.VigilBinarySHA256 == zeroHash || st.Integrity.VigilBinarySHA256 == "" {
		slog.Warn("integrity: binary baseline not set — run 'rosmarops baseline' to establish it")
	} else {
		selfHash, err := hash.Self()
		if err != nil {
			slog.Error("integrity: could not hash binary", "err", err)
			abortWithIntegrityFailure("binary_hash_changed",
				"Could not verify binary integrity: "+err.Error(),
				"Run 'rosmarops baseline' after verifying the binary is trustworthy.", abortOptions{lang: catalog.DefaultLang})
		}
		if selfHash != st.Integrity.VigilBinarySHA256 {
			slog.Error("integrity: binary hash mismatch",
				"stored", st.Integrity.VigilBinarySHA256[:16]+"…",
				"computed", selfHash[:16]+"…")
			abortWithIntegrityFailure("binary_hash_changed",
				"RosmarOps binary hash does not match the last baseline.",
				"If you updated RosmarOps intentionally, run 'rosmarops baseline'. "+
					"If not, do not run RosmarOps again — treat as potential compromise.", abortOptions{lang: catalog.DefaultLang})
		}
	}

	if st.Integrity.ConfigSHA256 == zeroHash || st.Integrity.ConfigSHA256 == "" {
		slog.Warn("integrity: config baseline not set — run 'rosmarops baseline' to establish it")
	} else {
		// Import config package only for path resolution, not for the full Load.
		// We hash the raw file bytes — schema validity is irrelevant here.
		//
		// Avoid importing internal/config to keep the startup path narrow.
		// Instead, use the same path-resolution logic inline.
		cfgPath, cfgErr := resolveCfgPath()
		if cfgErr != nil {
			slog.Warn("integrity: could not resolve config path", "err", cfgErr)
		} else {
			cfgHash, hashErr := hash.File(cfgPath)
			if hashErr != nil {
				slog.Warn("integrity: could not hash config", "path", cfgPath, "err", hashErr)
			} else if cfgHash != st.Integrity.ConfigSHA256 {
				slog.Error("integrity: config hash mismatch",
					"stored", st.Integrity.ConfigSHA256[:16]+"…",
					"computed", cfgHash[:16]+"…")
				abortWithIntegrityFailure("config_hash_changed",
					"Config file hash does not match the last baseline.",
					"If you edited rosmarops.config.json intentionally, run 'rosmarops baseline'. "+
						"If not, inspect the file for unexpected changes.", abortOptions{lang: catalog.DefaultLang, configRejected: true})
			}
		}
	}

	return nil
}

// abortOptions are what an integrity abort's report needs beyond the
// finding: the report's language, whether nobody confirmed a configuration
// change, whether the state was lost (J11: its own headline), and whether
// it is a scheduled review (decision 2026-10-26, integridad y revisión
// semanal, J2, J3; informe HTML v3 §17.6).
type abortOptions struct {
	lang           string
	configRejected bool
	stateLost      bool
	unattended     bool
	window         bool // the double-click window (U5): open the red report and wait for Enter
}

// abortWithIntegrityFailure writes a minimal RED report (Markdown, JSON and
// a red report.html), logs the finding, shows a scheduled review's
// notification or opens the report, and exits with code 2. This function never returns.
func abortWithIntegrityFailure(category, why, action string, o abortOptions) {
	f := finding.Finding{
		ID:              category + ":" + truncatedTimeID(),
		Severity:        finding.SeverityRed,
		Category:        category,
		Title:           why,
		WhyItMatters:    why,
		SuggestedAction: action,
		Confidence:      "high",
		FirstSeen:       time.Now().UTC().Format(time.RFC3339),
		IsNewSinceLast:  true,
	}

	if o.stateLost {
		// The Markdown is English (BV-45); the v1 category stays
		// binary_hash_changed, its list being closed.
		if en, err := catalog.Load(catalog.DefaultLang); err == nil {
			f.Title = en.Text("html.integrity.state_lost", nil)
		}
	}
	r := report.Build(version, []finding.Finding{f}, nil, 0, 0, 0, 0, nil)

	reportDir, err := report.DefaultReportDir()
	if err != nil {
		slog.Error("integrity abort: could not resolve report dir", "err", err)
	} else {
		mdPath, _, err := report.Write(r, reportDir)
		if err != nil {
			slog.Error("integrity abort: could not write report", "err", err)
		} else {
			fmt.Fprintln(os.Stderr, "INTEGRITY FAILURE — see report:", mdPath)
			cat, catErr := catalog.Load(o.lang)
			if catErr != nil {
				cat, _ = catalog.Load(catalog.DefaultLang)
			}
			// The red page in every language and in report.html (U7).
			base := strings.TrimSuffix(filepath.Base(mdPath), ".md")
			pages := map[string]string{}
			for _, l := range catalog.Langs() {
				if c, err := catalog.Load(l); err == nil {
					pages[l] = report.RenderAbortHTML(c, time.Now(), time.Local, base, o.configRejected, o.stateLost, o.unattended)
				}
			}
			htmlPath, err := report.WriteAbortHTML(reportDir, pages, cat.Lang())
			switch {
			case err != nil:
				slog.Error("integrity abort: could not write report.html", "err", err)
			case o.unattended:
				defaultNotifier.announce(context.Background(), cat.Text("notify.aborted", nil), htmlPath)
			case o.window:
				openReport(htmlPath)
			}
			if o.window {
				fmt.Fprintf(os.Stderr, "\n🔴 ABORT: %s\n%s\n", why, action)
				pressEnter(cat)
				os.Exit(exitIntegrity)
			}
		}
	}

	fmt.Fprintf(os.Stderr, "\n🔴 ABORT: %s\n%s\n", why, action)
	os.Exit(exitIntegrity)
}

// abortRunFailed is B3 (BV-93): a scheduled review and a double click never
// fail in silence. On any error from the run, it overwrites report.html in
// every language with a red "the review could not finish" page — there is no
// new report, so an earlier green one must not keep showing as today's — then
// shows the scheduled review's notification (unattended) or opens the page
// (window), and exits with the run_failed code. This function never returns.
func abortRunFailed(o abortOptions, cause error) {
	slog.Error("run failed", "err", cause)
	reportDir, err := report.DefaultReportDir()
	if err != nil {
		slog.Error("run_failed abort: could not resolve report dir", "err", err)
		fmt.Fprintln(os.Stderr, "error:", cause)
		os.Exit(exitRunFailed)
	}
	cat, catErr := catalog.Load(o.lang)
	if catErr != nil {
		cat, _ = catalog.Load(catalog.DefaultLang)
	}
	// The red page in every language and in report.html (U7).
	pages := map[string]string{}
	for _, l := range catalog.Langs() {
		if c, err := catalog.Load(l); err == nil {
			pages[l] = report.RenderRunFailedHTML(c, time.Now(), time.Local)
		}
	}
	htmlPath, err := report.WriteAbortHTML(reportDir, pages, cat.Lang())
	if err != nil {
		slog.Error("run_failed abort: could not write report.html", "err", err)
		fmt.Fprintln(os.Stderr, "error:", cause)
		os.Exit(exitRunFailed)
	}
	switch {
	case o.unattended:
		defaultNotifier.announce(context.Background(), cat.Text("notify.run_failed", nil), htmlPath)
	case o.window:
		openReport(htmlPath)
		fmt.Fprintln(os.Stdout, "error:", cause)
		fmt.Fprintln(os.Stdout, filepath.Join(reportDir, RunLogName))
		pressEnter(cat)
	}
	fmt.Fprintf(os.Stderr, "\n🔴 %s\n", cause)
	os.Exit(exitRunFailed)
}

// resolveCfgPath mirrors config.ResolveConfigPath without importing that package.
// Kept inline so the startup integrity check has minimal dependencies.
func resolveCfgPath() (string, error) {
	if _, err := os.Stat("rosmarops.config.json"); err == nil {
		return "rosmarops.config.json", nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, ".rosmarops", "config.json"), nil
}

// truncatedTimeID returns the last 16 hex chars of the current Unix nanosecond
// timestamp, used as the tail of a finding ID when no better evidence hash exists.
func truncatedTimeID() string {
	return fmt.Sprintf("%016x", time.Now().UnixNano())
}

// stub returns a Run function that prints to stderr and exits 1.
func stub(targetMilestone string) func(cli.Invocation) error {
	return func(inv cli.Invocation) error {
		slog.Error("not implemented yet", "target", targetMilestone, "command", "rosmarops "+strings.Join(inv.Path, " "))
		fmt.Fprintf(os.Stderr, "not implemented yet (target: %s)\n", targetMilestone)
		os.Exit(1)
		return nil // unreachable
	}
}

// --- rosmarops allow ----------------------------------------------------------

func newAllowCmd() *cli.Command {
	return &cli.Command{
		Name: "allow", Short: "cli.cmd.allow.short",
		Subs: []*cli.Command{
			{Name: "add", Usage: "<category> <id>", Short: "cli.cmd.allow.add.short", Args: 2, Before: integrityCheck, Run: stub("M4")},
			{Name: "remove", Usage: "<category> <id>", Short: "cli.cmd.allow.remove.short", Args: 2, Before: integrityCheck, Run: stub("M4")},
			{Name: "list", Short: "cli.cmd.allow.list.short", Args: cli.AnyArgs, Before: integrityCheck, Run: stub("M4")},
		},
	}
}

// --- rosmarops schedule -------------------------------------------------------

func newScheduleCmd() *cli.Command {
	return &cli.Command{
		Name: "schedule", Short: "cli.cmd.schedule.short", Args: cli.AnyArgs,
		Before: integrityCheck,
		Run:    stub("M8"),
	}
}
