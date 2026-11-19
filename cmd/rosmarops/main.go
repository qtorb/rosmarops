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
	"github.com/qtorb/rosmarops/internal/company"
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
			newInitCmd(),         // cmd_init.go   — skips integrity check
			newRunCmd(),          // cmd_run.go
			newBaselineCmd(),     // cmd_baseline.go — skips integrity check
			newStatusCmd(),       // cmd_status.go
			newFindingsCmd(),     // cmd_findings.go — «No me afecta» (BV-184)
			newProblemsCmd(),     // cmd_problems_cli.go — «problems ask» (BV-210, CTO-32 C4)
			newUsageCmd(),        // cmd_usage.go — usage statistics on/off
			newAllowCmd(),        // stub → M4
			newScheduleCmd(),     // stub → M8
			newKitCmd(),          // cmd_kit.go
			newConnectCmd(),      // cmd_space.go
			newDisconnectCmd(),   // cmd_space.go
			newSessionCheckCmd(), // cmd_agenthooks.go — BV-226, hidden
			newStatuslineCmd(),   // cmd_agenthooks.go — BV-226, hidden
		},
	}
}

// integrityCheck is the Before of every command but init, baseline and run
// (run checks its own integrity, with its flags). It checks the state, the
// binary and the configuration in use against the references in the state,
// with the same check as run: in a console the person is asked and, if they
// confirm, the new references are recorded and the command goes on; without
// a console nobody can confirm, so the command stops with a red report and
// exit code 2 (invariant 8). Before gets no flags, so its texts are in the
// system's language.
//
// A missing or unreadable state, or one with no reference recorded yet, is
// not checked here, as before: the command itself handles it.
func integrityCheck(path []string) error {
	if path[0] == "init" || path[0] == "baseline" || path[0] == "run" {
		return nil
	}
	stPath, err := state.StatePath()
	if err != nil {
		slog.Warn("integrity check: could not resolve state path", "err", err)
		return nil
	}
	st, err := state.LoadState(stPath)
	if err != nil {
		slog.Warn("integrity check: state unavailable, skipping", "err", err)
		return nil
	}
	if isZeroHash(st.Integrity.VigilBinarySHA256) || isZeroHash(st.Integrity.ConfigSHA256) {
		slog.Warn("integrity check: no reference recorded yet")
		return nil
	}
	cfgPath, err := resolveCfgPath()
	if err != nil {
		slog.Warn("integrity check: could not resolve config path", "err", err)
		return nil
	}
	defaultDir, err := report.DefaultReportDir()
	if err != nil {
		return fmt.Errorf("resolve report dir: %w", err)
	}
	lang := systemLang()
	cat, err := catalog.Load(lang)
	if err != nil {
		return fmt.Errorf("message catalog: %w", err)
	}
	moveOldReports(os.Stdout, cat)
	// The company configuration decides which file is the configuration in
	// use, as in run; this command never writes the company summary.
	home, _ := os.UserHomeDir()
	comp := &companyRun{loaded: company.LoadConfig(companyDir()), source: configSourceUser, binary: binaryUnchanged, noSummary: true}
	_, cfgPath, _ = resolveRunConfig(cfgPath, home, comp)
	_, err = integrityRun{
		ask: asker{in: os.Stdin, out: os.Stdout, terminal: runCanAsk(), cat: cat}, st: &st, stPath: stPath,
		cfgPath: cfgPath, reportDir: defaultDir, lang: lang, selfHash: hash.Self, abort: abortIntegrity,
		store: realStoreCheck, adminList: adminListed, company: comp,
	}.check()
	return err
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
	window         bool   // the double-click window (U5): open the red report and wait for Enter
	detail         string // the technical detail of integrity.abort.unverifiable ({error})
}

// abortWithIntegrityFailure writes a minimal RED report (Markdown, JSON and
// a red report.html), logs the finding, shows a scheduled review's
// notification or opens the report, and exits with code 2. text is the
// catalog key of the abort's why and action (abortBinary…): the Markdown
// stays in English, the console in the person's language. This function
// never returns.
func abortWithIntegrityFailure(category, text string, o abortOptions) {
	ui.failed() // BV-151: the window and taskbar show the run could not finish
	cat, catErr := catalog.Load(o.lang)
	if catErr != nil {
		cat, _ = catalog.Load(catalog.DefaultLang)
	}
	en, enErr := catalog.Load(catalog.DefaultLang)
	if enErr != nil {
		en = cat
	}
	p := map[string]string{"error": o.detail}
	why, action := cat.Text(text+".why", p), cat.Text(text+".action", p)
	f := finding.Finding{
		ID:              category + ":" + truncatedTimeID(),
		Severity:        finding.SeverityRed,
		Category:        category,
		Title:           en.Text(text+".why", p),
		WhyItMatters:    en.Text(text+".why", p),
		SuggestedAction: en.Text(text+".action", p),
		Confidence:      "high",
		FirstSeen:       time.Now().UTC().Format(time.RFC3339),
		IsNewSinceLast:  true,
	}

	if o.stateLost {
		// The Markdown is English (BV-45); the v1 category stays
		// binary_hash_changed, its list being closed.
		f.Title = en.Text("html.integrity.state_lost", nil)
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
			// The red page in every language and in report.html (U7).
			base := strings.TrimSuffix(filepath.Base(mdPath), ".md")
			pages := map[string]string{}
			for _, l := range catalog.Langs() {
				if c, err := catalog.Load(l); err == nil {
					pages[l] = report.RenderAbortHTML(c, time.Now(), time.Local, base, o.configRejected, o.stateLost, o.unattended)
				}
			}
			htmlPath, err := report.WriteAbortHTML(reportDir, pages, cat.Lang())
			writeAbortedStatusline(reportDir, time.Now()) // BV-226 §2
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
	ui.failed() // BV-151: the window and taskbar show the run could not finish
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
	writeAbortedStatusline(reportDir, time.Now()) // BV-226 §2
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
