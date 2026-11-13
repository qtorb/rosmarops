package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/qtorb/rosmarops/internal/agentdirs"
	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/intel"
	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/pipeline"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/problems"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan"
	"github.com/qtorb/rosmarops/internal/scan/avexclusions"
	"github.com/qtorb/rosmarops/internal/scan/configkeys"
	"github.com/qtorb/rosmarops/internal/scan/credfiles"
	"github.com/qtorb/rosmarops/internal/scan/globals"
	"github.com/qtorb/rosmarops/internal/scan/mcp"
	n8nscan "github.com/qtorb/rosmarops/internal/scan/n8n"
	"github.com/qtorb/rosmarops/internal/scan/pyenv"
	"github.com/qtorb/rosmarops/internal/scan/repos"
	"github.com/qtorb/rosmarops/internal/scan/scheduledtasks"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
	"github.com/qtorb/rosmarops/internal/scan/shellprofile"
	"github.com/qtorb/rosmarops/internal/scan/vscode"
	"github.com/qtorb/rosmarops/internal/spacenet"
	"github.com/qtorb/rosmarops/internal/state"
)

// runLangs are the languages of --lang. Without it, the Windows display
// language when it has a translation, else English (decision 2026-10-27,
// primer uso con un solo .exe, U7).
var runLangs = map[string]bool{"en": true, "es": true}

// writeResultsPkg builds and seals this run's results package. A package-level
// var so a test can make it fail and check the review still finishes (BV-152).
var writeResultsPkg = report.WriteResultsPackage

func newRunCmd() *cli.Command {
	return &cli.Command{
		Name: "run", Short: "cli.cmd.run.short", Long: "cli.cmd.run.long", Args: cli.AnyArgs,
		Flags: []cli.Flag{
			{Name: "lang", Default: systemLang(), Usage: "cli.flag.lang"},
			// A scheduled review (decision 2026-10-26, integridad y revisión
			// semanal, J3): left out of the help, which has no text for it.
			{Name: "unattended", Bool: true, Hidden: true},
		},
		Before: integrityCheck,
		Run:    runRun,
	}
}

// runInput, runCanAsk and newScanContext are the console's input, whether
// it can ask, and the scan's context under the global cap; tests replace
// them.
var (
	runInput       io.Reader = os.Stdin
	runCanAsk                = func() bool { return canAsk(os.Stdin, os.Stdout) }
	newScanContext           = func(searchStart time.Time, projects int) (context.Context, context.CancelFunc) {
		return context.WithDeadline(context.Background(), scan.DeadlineFor(searchStart, projects))
	}
)

// installRunLog routes slog to a fresh run log (RunLogName) and returns it, so
// a manual «run» keeps its log in the reports directory and out of the console
// (BV-211 §1). The log buffers until runWith attaches it to the reports dir.
func installRunLog() *deferredLog {
	log := &deferredLog{name: RunLogName}
	slog.SetDefault(slog.New(slog.NewTextHandler(log, nil)))
	return log
}

func runRun(inv cli.Invocation) error {
	unattended := inv.Flags["unattended"] == "true"
	m := runMode{unattended: unattended}
	// BV-211 §1: a manual «run» with a console sends its slog to the run log in
	// the reports directory (RunLogName), the same file as a double click, never
	// to stdout or stderr — so the console shows only the catalog's progress
	// lines, never «INFO …» mixed in. A scheduled review (unattended) keeps its
	// own log (ScheduledLogName), set inside runWith.
	if !unattended {
		log := installRunLog()
		defer log.Close()
		m.log = log
	}
	htmlPath, err := runWith(inv.Flags["lang"], m)
	if err != nil && unattended {
		// B3 (BV-93): a scheduled review never fails in silence — a red page,
		// a notification and the run_failed exit code. Never returns.
		abortRun(abortOptions{lang: inv.Flags["lang"], unattended: true}, err)
	}
	// BV-184 point 3: from an interactive terminal (never a scheduled review),
	// offer to dismiss findings after the review when at least one is new.
	if err == nil && !unattended && runCanAsk() {
		if cfgPath, e := config.ResolveConfigPath(); e == nil {
			if stPath, e := state.StatePath(); e == nil {
				offerDismissAfterReport(inv.Flags["lang"], cfgPath, stPath, true, time.Now())
			}
		}
		// BV-210 §2 A / CTO-32 C3: the problems step after the report is written.
		offerProblemsAfterReport(filepath.Dir(absPath(htmlPath)), inv.Flags["lang"], true)
	}
	return err
}

// runMode is how a run was started: by hand (zero value), as a scheduled
// review (unattended, J3), or by a double click (window, decision
// 2026-10-27, primer uso con un solo .exe, U1–U5), whose log the caller
// already sends to log.
type runMode struct {
	unattended bool
	window     bool
	log        *deferredLog
}

// runWith is a run; it returns report.html's path.
func runWith(lang string, m runMode) (htmlPath string, err error) {
	start := time.Now()
	if !runLangs[lang] {
		return "", fmt.Errorf("lang_unsupported: %q (en, es)", lang)
	}
	unattended := m.unattended
	runLog := m.log
	// A scheduled review (J3): no window, never reads input, its log in
	// the reports directory.
	if unattended {
		freeConsole()
		runLog = &deferredLog{name: ScheduledLogName}
		defer runLog.Close() // after the deferred attach below
		slog.SetDefault(slog.New(slog.NewTextHandler(runLog, nil)))
	}
	if runLog != nil {
		defer func() {
			if err != nil {
				slog.Error("run failed", "err", err)
				if dir, dirErr := report.DefaultReportDir(); dirErr == nil {
					_ = runLog.attach(dir) // a failure before the reports directory was known
				}
			}
		}()
	}
	// Load config: a scheduled review and a double click read only
	// ~/.rosmarops/config.json, never one in the working directory (J3, U2).
	resolve := config.ResolveConfigPath
	if unattended || m.window {
		resolve = config.HomeConfigPath
	}
	cfgPath, err := resolve()
	if err != nil {
		return "", fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return "", fmt.Errorf("load config: %w", err)
	}

	// Load state (may not exist on first run after init but before baseline).
	stPath, err := state.StatePath()
	if err != nil {
		return "", fmt.Errorf("resolve state path: %w", err)
	}
	st, stErr := state.LoadState(stPath)
	if stErr != nil {
		slog.Warn("could not load state; treating as first run", "err", stErr)
		st, err = state.DefaultState(version)
		if err != nil {
			return "", fmt.Errorf("create fallback state: %w", err)
		}
	}
	// The engine version the loaded state was written with, read before the
	// integrity check bumps it: a different version means the binary changed,
	// so the scheduled-task hash (BV-93, B2) may now cover new fields and must
	// not flag every task. The scheduled-task scan treats this run as fresh.
	freshTaskHash := st.EngineVersion != version

	// B3 (BV-93): integrity is verified before any configuration value is
	// used, the report (log) directory included — a tampered config must not
	// redirect a single write before it is caught. Until the check passes,
	// only the fixed default report directory is written to (the abort report,
	// the accepted copy and the log); the configured report directory is used
	// only afterwards.
	defaultDir, err := report.DefaultReportDir()
	if err != nil {
		return "", fmt.Errorf("resolve report dir: %w", err)
	}
	// BV-183: tidy the default reports directory into data/ before anything is
	// read from or written to it (the log below, and the accepted-config copy
	// the integrity check reads). The default directory is fixed, so this runs
	// before the integrity check that still gates the configured directory.
	// Idempotent and best effort.
	migrateReportsLayout(defaultDir)

	if runLog != nil {
		if err := runLog.attach(defaultDir); err != nil {
			return "", err
		}
	}

	cat, err := catalog.Load(lang)
	if err != nil {
		return "", fmt.Errorf("message catalog: %w", err)
	}

	// Console branding (BV-151): on a manual run in a console, name the window,
	// drive the taskbar button and print the brand header — the first thing the
	// run prints, before the integrity questions and console.checking. A
	// scheduled review (freeConsole above) and redirected output get none.
	active := !unattended && isTerminal(os.Stdout)
	vtOK := false
	if active {
		vtOK = enableVTProcessing()
	}
	taskbarOn := active && vtOK && os.Getenv("WT_SESSION") != ""
	ui = newConsoleUI(cat, active, taskbarOn, os.Stdout)
	if active {
		writeBrandHeader(os.Stdout, version, vtOK)
	}
	ui.setTitle("console.title.running")

	// Integrity (J1–J3): the state, the binary and the configuration
	// against the references init recorded; a change is confirmed in the
	// console or the run stops with a red report. A scheduled review never
	// reads input.
	ask := asker{in: runInput, out: os.Stdout, terminal: runCanAsk(), cat: cat}
	if unattended {
		ask.in, ask.out, ask.terminal = strings.NewReader(""), io.Discard, false
	}
	// BV-222: a Microsoft Store update of RosmarOps itself is accepted
	// without asking (invariant 8); storeUpd records it for the report.
	var storeUpd storeUpdate
	integrityNotChecked, err := integrityRun{
		ask: ask, st: &st, stErr: stErr, stPath: stPath, cfgPath: cfgPath, reportDir: defaultDir,
		unattended: unattended, window: m.window, lang: lang, selfHash: hash.Self,
		store: realStoreCheck, accepted: &storeUpd,
	}.check()
	if err != nil {
		return "", err
	}

	// The configuration is verified now: its report directory can be used for
	// the report files that follow.
	reportDir := cfg.ReportDirectory
	if reportDir == "" {
		reportDir = defaultDir
	}
	// BV-183: now that the configuration is verified, tidy the configured
	// reports directory too (a no-op when it is the default, already migrated
	// above). Everything but the three HTML pages lives in dataDir.
	if reportDir != defaultDir {
		migrateReportsLayout(reportDir)
	}
	dataDir := report.DataDir(reportDir)

	// BV-210: mark this review in progress so an abrupt end (killed, hung,
	// crashed) is seen as run_unfinished next time, and learn whether the
	// previous review did not finish. The notebook and the mark live in the
	// reports data directory (an already-allowed path, invariant 2); nothing
	// here leaves the machine. EndReview clears the mark on any return — only a
	// true crash, where defers do not run, leaves it behind.
	notebook := problems.New(dataDir)
	problemsAutoSentThisRun = 0
	problemsAskedThisRun = false
	// BV-210 test aid: ROSMAROPS_DEMO_PROBLEM lets Albert see the windows
	// without touching his real config — it only writes to the problem notebook
	// and run mark (allowed paths), never to any agent or RosmarOps config.
	// "b" leaves a fake in-progress mark so this run detects run_unfinished and
	// shows Window B at startup; "a" seeds one unsent problem so Window A shows
	// at the end of this review.
	seedDemoProblemB(dataDir, start)
	prevUnfinishedDay, _ := notebook.StartReview(start)
	defer func() { _ = notebook.EndReview() }()
	seedDemoProblemA(dataDir, start)

	// BV-210 §2 B: the previous review did not finish. Record run_unfinished now
	// (so Window B and the report see it) and, when someone is present and the
	// checkbox is off, ask before starting the new review. Nobody present (a
	// scheduled review): never ask here; the checkbox auto-send below sends it.
	if prevUnfinishedDay != "" {
		recordProblems(dataDir, problems.Entry{Code: problems.RunUnfinished, Step: problems.StepReview, Day: prevUnfinishedDay})
		// §2 B: with someone present and the checkbox off, ask before the new
		// review. With the checkbox on, the pre-report send below sends it.
		if presentForProblems(m, unattended) && !st.SendProblemsWithoutAsking {
			// offerProblems sets problemsAskedThisRun only when it really asks
			// (CTO-33, one question per session). BV-211d: it is no longer set
			// unconditionally here — a Window B that could not be shown must not
			// suppress Window A for a problem recorded later this run.
			offerProblems(dataDir, lang, version, true, true, &st, stPath, time.Now())
		}
	}

	// The connect offer moved to the end of the double click (BV-158 part 4,
	// decision A): it is shown after the report opens, in runNoArgs, not here
	// before the run. A fresh connection now sends the next run's results, as
	// the report's «Your space» section says; this run seals its package and,
	// if already connected, sends it in the send path below.
	//
	// Invariant §6 (BV-155): the global limit is 15 s per project to check, at
	// least 300 and at most 1200 s, computed once now that the projects are
	// known and before they are checked. The time already spent (agents,
	// discovery) counts within the same limit: every scanner runs under this
	// one context, which counts from after the integrity questions — the time a
	// person takes to answer is not the scan's (J13).
	//
	// CTO-38: the projects are found by the project search, in every review
	// and with no cache (internal/scan/projects); repo_roots is only its
	// input. The global limit counts from the start of the search (G2), so the
	// search's own time (at most projects.MaxTime) is within it.
	search := searchProjects(cfg, st)
	prevRepos := previousRepos(st) // before the repo scan replaces the baselines
	repoList := search.Repos()
	nProjects := len(repoList)
	limit := scan.GlobalTimeoutFor(nProjects)
	limitMinutes := scan.LimitMinutes(limit)
	// The console lines, once the number of projects is known (BV-155 point 2,
	// CTO-38 §4.1); never on a scheduled review (no console).
	if !unattended {
		printProjects(os.Stdout, cat, search, consoleWidth())
		fmt.Fprintln(os.Stdout, checkingLine(cat, nProjects, limitMinutes))
	}
	slog.Info("project search", "status", search.Status, "found", search.Found, "checked", nProjects,
		"dirs_read", search.DirsRead, "duration", search.Duration)
	ctx, cancel := newScanContext(search.Started, nProjects)
	defer cancel()
	progress := newProgress(os.Stdout, isTerminal(os.Stdout), cat)
	// BV-211 §3: refresh the repositories line once a second so its elapsed time
	// ticks and a slow repository is explained, even while the count holds.
	progress.start()
	defer progress.stop()

	var (
		findings         []finding.Finding
		scanErrors       []finding.ScanError
		allowListedTotal int
		// allowHits are the warnings the configuration hid, for the
		// report (decision 2026-10-27, blindaje, B9).
		allowHits []config.AllowHit
	)
	// CTO-38 §2.2: a search that reached its limit is a yellow finding, so the
	// review is never green (invariant 4).
	if !search.Complete() {
		findings = append(findings, projectSearchFinding(cat, search))
	}

	// The fallback state carries a fresh local salt, so this run's credential
	// fingerprints and host_id cannot be compared with earlier runs.
	if stErr != nil {
		scanErrors = append(scanErrors, finding.ScanError{
			Scope:   "state",
			Message: "state unavailable: fresh local salt; fingerprints and host_id not comparable with previous runs",
		})
	}

	// This RosmarOps, for its own weekly review (decision 2026-10-26,
	// integridad y revisión semanal, J4–J6).
	exePath, exeErr := os.Executable()
	if exeErr != nil {
		slog.Warn("run: could not resolve the executable's path", "err", exeErr)
	}
	// BV-209: from a Microsoft Store install the real exe is under a versioned
	// WindowsApps folder that changes on every update; the weekly-review command
	// the report gives and the task RosmarOps looks for must name the stable
	// execution alias instead. No-op for the standalone .exe and on macOS/Linux.
	// The integrity check still hashes the real running binary (hash.Self).
	exePath = storeAliasExe(exePath, os.Getenv("LOCALAPPDATA"))
	langs := make([]string, 0, len(runLangs))
	for l := range runLangs {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	self := scheduledtasks.Self{Exe: exePath, Domain: os.Getenv("USERDOMAIN"), User: os.Getenv("USERNAME"), Langs: langs}
	weekly := scheduledtasks.Weekly{State: scheduledtasks.WeeklyNone}
	weeklyNoSystemd := false
	// The checks that do not apply on this system yet (ErrNotApplicable),
	// by report.NotCheckedCodes' code: the macOS report lists them
	// (decision 2026-10-29, macOS vista previa, point 3).
	var notChecked []string
	if !displayLanguageChecked {
		notChecked = append(notChecked, "display_language")
	}

	// Resolve home dir once — used by the MCP writable-path check.
	homeDir, homeDirErr := os.UserHomeDir()
	if homeDirErr != nil {
		slog.Warn("run: could not resolve home dir; mcp_server_user_writable_command check disabled", "err", homeDirErr)
	}

	plat := newPlatform()

	// Threat-data snapshot (BV-100, F0a): validated against intel.v1 at load.
	// An invalid snapshot is fail-secure — the cross is disabled and the run is
	// marked in error, so it can never report green with broken threat data.
	var intelMatcher *intel.Matcher
	if snap, intelErr := intel.Load(); intelErr != nil {
		slog.Warn("intel: embedded threat data did not validate; cross disabled", "err", intelErr)
		scanErrors = append(scanErrors, finding.ScanError{Scope: "intel", Message: intelErr.Error()})
	} else {
		intelMatcher = intel.NewMatcher(snap)
	}

	repoPatterns := secrets.BuiltinPatterns()
	for _, spec := range cfg.ExtraSecretPatterns {
		p, compileErr := compileExtraPattern(spec)
		if compileErr != nil {
			slog.Warn("run: invalid extra_secret_patterns entry; skipping",
				"name", spec.Name, "err", compileErr)
			continue
		}
		repoPatterns = append(repoPatterns, p)
	}

	// Each step scans under ctx and returns a function that records its
	// results; scan.Run calls it on this goroutine (see scan.Step).
	var (
		mcpScan         platform.MCPScan
		mcpErr          error
		hermesScan      platform.HermesScan      // BV-111: config.yaml drives Hermes's caps
		codexScan       platform.CodexScan       // BV-140: TOML layers drive Codex's caps
		grokScan        platform.GrokScan        // BV-140: TOML layers drive Grok's caps
		copilotScan     platform.CopilotScan     // BV-140: VS Code settings drive Copilot's caps
		antigravityScan platform.AntigravityScan // BV-140: antigravity-cli settings drive its caps
		geminiScan      platform.GeminiScan      // BV-193: gemini-cli settings drive its caps
		clineScan       platform.ClineScan       // BV-193: cline globalState drives its caps
		credFiles       platform.CredentialFileScan
		built           *graph.Graph // the graph written this run, for the v2 report
		builtSHA        string
		// builtSettings is the Claude Code settings scan the graph was built
		// from, for the model provider row (T12).
		builtSettings *platform.ClaudeSettingsScan
		families      [][]string
		// buildGraph is set by the graph step once the agents' sources are
		// listed.
		buildGraph   func(graph.Repos)
		repoFindings []finding.Finding
		// vscodeExts are the extensions listed, for the graph's extension
		// agents (A3); vscodeFirstRun is set on a first run (A5).
		vscodeExts     []platform.VSCodeExtension
		vscodeFirstRun *report.VSCodeFirstRun
		// globalObs are the global npm/pnpm packages read this run, for the
		// threat-intel cross (BV-100).
		globalObs []globals.ObservedGlobal
	)
	salt, saltErr := st.Salt()
	graphDone := false
	// How many projects the global limit left unreviewed (BV-155): -1 until the
	// repos step runs and reports it; the limit-reached note reads it, falling
	// back to every project when the repos step never ran.
	reposNotReviewed := -1
	// BV-186: tracked files not opened because they are images, documents or
	// other binaries; set by the repos step, shown in the report's coverage.
	binarySkipped := 0
	steps := []scan.Step{
		// --- VS Code extensions scanner (M4). The reference is updated on
		// every run, as the other collectors' (decision 2026-10-26, agentes
		// y MCP nuevos, A5); the agent extensions go to the graph (A3). ---
		{Name: "vscode", Run: func(ctx context.Context) func() {
			currentExts, extErr := plat.ListVSCodeExtensions(ctx)
			extResult := vscode.ScanExtensions(currentExts, st.Baselines.VSCodeExtensions, cfg.AllowList.Extensions, start)
			return func() {
				if extErr != nil {
					slog.Warn("vscode: could not list extensions", "err", extErr)
					scanErrors = append(scanErrors, finding.ScanError{
						Scope:   "vscode",
						Message: extErr.Error(),
					})
					return
				}
				vscodeExts = currentExts
				findings = append(findings, extResult.Findings...)
				allowListedTotal += extResult.AllowListedCount
				allowHits = append(allowHits, extResult.Hits...)
				st.Baselines.VSCodeExtensions = extResult.NewBaseline
				if extResult.FirstRun && extResult.Recorded > 0 {
					vscodeFirstRun = &report.VSCodeFirstRun{Recorded: extResult.Recorded, Recent: extResult.Recent}
				}
			}
		}},

		// --- MCP server scanner (M5) ---
		{Name: "mcp", Run: func(ctx context.Context) func() {
			scanned, listErr := plat.ListMCPServers(ctx, repoList)
			// Hermes Agent's config.yaml (N2a, BV-97): its MCP servers and
			// its sources join every agent's; a config.yaml not read is a
			// failed source (B6).
			hermes, hermesErr := plat.ListHermes(ctx)
			scanned.Sources = append(scanned.Sources, hermes.Sources...)
			scanned.Servers = append(scanned.Servers, hermes.Servers...)
			hermesScan = hermes // BV-111: kept for the graph step (approvals.mode, terminal.backend)
			// OpenAI Codex's config layers (BV-140, CTO-16): its in-force MCP
			// servers join every agent's, so the shared pipeline builds their
			// nodes, loads edges and credentials; the scan is kept for the graph
			// step (sandbox and approval capabilities, the overview's assumptions).
			codex, codexErr := plat.ListCodex(ctx, repoList)
			scanned.Servers = append(scanned.Servers, graph.CodexMCPServers(codex)...)
			codexScan = codex
			// xAI Grok Build (BV-140, CTO-16 Grok): its in-force MCP servers join
			// every agent's; the scan is kept for the graph step.
			grok, grokErr := plat.ListGrok(ctx, repoList)
			scanned.Servers = append(scanned.Servers, graph.GrokMCPServers(grok)...)
			grokScan = grok
			// GitHub Copilot in VS Code (BV-140, CTO-16 Copilot): its MCP servers
			// join every agent's; the scan is kept for the graph step.
			copilot, copilotErr := plat.ListCopilot(ctx, repoList)
			scanned.Servers = append(scanned.Servers, graph.CopilotMCPServers(copilot)...)
			copilotScan = copilot
			// Google Antigravity CLI (BV-140, decision 2): its settings drive its
			// caps; its MCP servers already joined above as antigravity-cli.
			antigravity, antigravityErr := plat.ListAntigravity(ctx, repoList)
			antigravityScan = antigravity
			// Google Gemini CLI (BV-193, CTO-22 Gemini): its in-force MCP servers
			// join every agent's; the scan is kept for the graph step.
			gemini, geminiErr := plat.ListGemini(ctx, repoList)
			scanned.Servers = append(scanned.Servers, graph.GeminiMCPServers(gemini)...)
			geminiScan = gemini
			// Cline VS Code extension (BV-193, CTO-22 Cline): its globalState
			// drives its caps; its MCP servers already joined above as client cline.
			cline, clineErr := plat.ListCline(ctx, repoList)
			clineScan = cline
			listErr = errors.Join(listErr, hermesErr, codexErr, grokErr, copilotErr, antigravityErr, geminiErr, clineErr)
			// Only the servers a client loads (A4); no reference: new and
			// modified servers are the v2 memory's, or retired.
			mcpResult := mcp.ScanMCPServers(scanned.Servers, cfg.AllowList.MCPServers, homeDir,
				mcp.Binding{Salt: salt, Baseline: st.Baselines.MCPServers, Now: time.Now()})
			return func() {
				mcpScan, mcpErr = scanned, listErr
				if mcpErr != nil {
					slog.Warn("mcp: could not list MCP servers", "err", mcpErr)
					scanErrors = append(scanErrors, finding.ScanError{
						Scope:   "mcp",
						Message: mcpErr.Error(),
					})
				}
				findings = append(findings, mcpResult.Findings...)
				allowListedTotal += mcpResult.AllowListedCount
				allowHits = append(allowHits, mcpResult.Hits...)
				// B9: the destination each allow-listed server had when
				// its entry was accepted. Not on a failed listing, which
				// would forget them.
				if listErr == nil {
					st.Baselines.MCPServers = mcpResult.NewBaseline
				}
			}
		}},

		// --- Credential files collector (decision 2026-10-24, D1, D2): .env
		// files of every repo and the user's credential files, for the graph.
		// No git, no subprocess; outside the per-repo budget. ---
		{Name: "credentials", Run: func(ctx context.Context) func() {
			scanned := credfiles.CollectOn(agentdirs.Current(), homeDir, repoList, nil)
			// The envFile of Cursor's MCP servers (decision 2026-10-25,
			// Cursor, K10).
			var envFiles []string
			for _, srv := range mcpScan.Servers {
				if srv.EnvFile != "" {
					envFiles = append(envFiles, srv.EnvFile)
				}
			}
			scanned = credfiles.AddEnvFiles(scanned, envFiles)
			return func() {
				credFiles = scanned
				// B5 for the key files read with exact keys (BV-96).
				findings = append(findings, configkeys.FromCredentialFiles(scanned)...)
			}
		}},

		// --- v2 graph: MCP agents and loads edges, Claude Code permissions,
		// written next to the v1 report ---
		// A graph that cannot be built or fails validation is not written (and
		// any previous one is removed); the run continues and ends at least
		// yellow.
		{Name: "graph", Run: func(ctx context.Context) func() {
			settingsScan, settingsErr := plat.ListClaudeSettings(ctx, repoList)
			instructions, instructionsErr := plat.ListAgentInstructions(ctx, repoList)
			cursorScan, cursorErr := plat.ListCursor(ctx, repoList)
			skillScan, skillsErr := plat.ListSkills(ctx, repoList)
			instructionsErr = errors.Join(instructionsErr, cursorErr, skillsErr)
			if ctx.Err() != nil {
				settingsErr = errors.Join(settingsErr, fmt.Errorf("claude settings: %w", ctx.Err()))
			}
			listErr := errors.Join(mcpErr, settingsErr, instructionsErr)
			return func() {
				graphDone = true
				// B5: a key the agent reads differently from RosmarOps.
				findings = append(findings, configkeys.Findings(mcpScan, settingsScan, cursorScan)...)
				// The graph is built once the repositories are scanned: the
				// secrets committed in them are part of it (CTO-7 §2.2).
				buildGraph = func(repoGraph graph.Repos) {
					// Python packages installed at rest (BV-100, F0a): a directed,
					// one-level listing of the known site-packages folders; no
					// recursion, so the forced exclusions are untouched. A folder
					// that exists but cannot be read is coverage not read (yellow).
					// On a system whose layout does not apply (macOS, M1b) the
					// resolver returns ErrNotApplicable: the Python cross does not
					// run, and it is only logged — not a failure and not coverage.
					// The MCP and npm-global crosses do not depend on it.
					var pyResult pyenv.Result
					pyDirs, pyErr := plat.PythonSitePackages(repoList)
					if errors.Is(pyErr, platform.ErrNotApplicable) {
						slog.Info("pyenv", "err", pyErr)
						notChecked = append(notChecked, "python_cross")
					} else if pyErr != nil {
						slog.Warn("pyenv: could not resolve site-packages", "err", pyErr)
						scanErrors = append(scanErrors, finding.ScanError{Scope: "pyenv", Message: pyErr.Error()})
					} else {
						pyResult = pyenv.Scan(pyDirs)
						for _, fl := range pyResult.Failed {
							scanErrors = append(scanErrors, finding.ScanError{
								Scope:   "pyenv",
								Message: fmt.Sprintf("%s: %s", fl.Path, fl.Reason),
							})
						}
					}
					// n8n user folder (BV-175): read at rest, no network, no
					// writes. Its community node packages are crossed against the
					// threat data like any global npm package; the credential key
					// beside the database gives the n8n_key_with_data finding.
					n8nFolder := agentdirs.N8NDir(homeDir, nil)
					n8nResult, n8nErrs := n8nscan.Scan(n8nFolder)
					for _, e := range n8nErrs {
						scanErrors = append(scanErrors, finding.ScanError{Scope: "n8n", Message: e.Error()})
					}
					pkgObs := packageObservations(pyResult, globalObs)
					for _, p := range n8nResult.Packages {
						pkgObs = append(pkgObs, graph.PackageObservation{
							Ecosystem: intel.EcosystemNPM, Name: p.Name, Version: p.Version, Path: p.Path,
						})
					}
					g, graphSHA, graphPath, graphErr := writeGraph(mcpScan, listErr, settingsScan, cursorScan, hermesScan, codexScan, grokScan, copilotScan, antigravityScan, geminiScan, clineScan, instructions, skillScan, credFiles, vscodeExts, repoGraph, repoList, homeDir, st, report.DataDir(reportDir), start, intelMatcher, pkgObs, n8nResult.Folder, n8nResult.KeyAndData())
					if graphErr == nil {
						built, builtSHA, builtSettings = g, graphSHA, &settingsScan
					}
					if graphErr != nil {
						slog.Warn("graph: v2 graph not written", "err", graphErr)
						scanErrors = append(scanErrors, finding.ScanError{
							Scope:   "graph",
							Message: graphErr.Error(),
						})
					} else {
						slog.Info("graph written", "path", graphPath)
					}
				}
			}
		}},

		// --- Shell profile scanner (M7) ---
		{Name: "shellprofile", Run: func(ctx context.Context) func() {
			currentProfiles, profErr := plat.ListShellProfiles(ctx)
			profResult := shellprofile.ScanShellProfiles(currentProfiles, st.Baselines.ShellProfileSHA256)
			return func() {
				if errors.Is(profErr, platform.ErrNotApplicable) {
					slog.Info("shellprofile", "err", profErr)
					notChecked = append(notChecked, "shellprofile")
					return
				}
				if profErr != nil {
					slog.Warn("shellprofile: could not list profiles", "err", profErr)
					scanErrors = append(scanErrors, finding.ScanError{
						Scope:   "shellprofile",
						Message: profErr.Error(),
					})
				}
				findings = append(findings, profResult.Findings...)
				st.Baselines.ShellProfileSHA256 = profResult.NewSHA
			}
		}},

		// --- Scheduled tasks scanner (M7) ---
		{Name: "scheduledtasks", Run: func(ctx context.Context) func() {
			currentTasks, tasksErr := plat.ListScheduledTasks(ctx)
			tasksResult := scheduledtasks.ScanScheduledTasks(currentTasks, st.Baselines.ScheduledTasks, cfg.AllowList.ScheduledTasks, self, freshTaskHash)
			return func() {
				if errors.Is(tasksErr, platform.ErrNotApplicable) {
					slog.Info("scheduledtasks", "err", tasksErr)
					// BV-165: on Linux the weekly review is a systemd user timer,
					// read directly (not Windows Task Scheduler), so weekly IS
					// checked here. Elsewhere (macOS) there is no weekly review yet.
					if runtimeGOOS == "linux" {
						weekly, weeklyNoSystemd = linuxWeekly(exePath)
						return
					}
					// No task list: no weekly review either (point 4).
					notChecked = append(notChecked, "scheduledtasks", "weekly")
					return
				}
				weekly = scheduledtasks.WeeklyOf(currentTasks, self)
				if tasksErr != nil {
					slog.Warn("scheduledtasks: could not list tasks", "err", tasksErr)
					scanErrors = append(scanErrors, finding.ScanError{
						Scope:   "scheduledtasks",
						Message: tasksErr.Error(),
					})
				}
				findings = append(findings, tasksResult.Findings...)
				allowListedTotal += tasksResult.AllowListedCount
				allowHits = append(allowHits, tasksResult.Hits...)
				st.Baselines.ScheduledTasks = tasksResult.NewBaseline
			}
		}},

		// --- AV exclusions scanner (M7) ---
		{Name: "avexclusions", Run: func(ctx context.Context) func() {
			currentExcls, avErr := plat.ListDefenderExclusions(ctx)
			avResult := avexclusions.ScanAVExclusions(currentExcls, st.Baselines.AVExclusions, cfg.AllowList.AVExclusions)
			return func() {
				if errors.Is(avErr, platform.ErrNotApplicable) {
					slog.Info("avexclusions", "err", avErr)
					notChecked = append(notChecked, "avexclusions")
					return
				}
				if avErr != nil {
					slog.Warn("avexclusions: could not list Defender exclusions", "err", avErr)
					scanErrors = append(scanErrors, finding.ScanError{
						Scope:   "avexclusions",
						Message: avErr.Error(),
					})
				}
				findings = append(findings, avResult.Findings...)
				allowListedTotal += avResult.AllowListedCount
				allowHits = append(allowHits, avResult.Hits...)
				st.Baselines.AVExclusions = avResult.NewBaseline
			}
		}},

		// --- Global npm/pnpm packages + npmrc scanner (M7) ---
		{Name: "globals", Run: func(ctx context.Context) func() {
			globalsResult := globals.ScanGlobals(ctx, homeDir, st.Baselines.GlobalPackages, cfg.AllowList.GlobalPackages)
			return func() {
				findings = append(findings, globalsResult.Findings...)
				allowListedTotal += globalsResult.AllowListedCount
				allowHits = append(allowHits, globalsResult.Hits...)
				globalObs = globalsResult.Observed
				st.Baselines.GlobalPackages = globalsResult.NewPackageBaseline
				if globalsResult.NewNpmrcSHA != "" {
					st.Baselines.NpmrcSHA256 = globalsResult.NewNpmrcSHA
				}
			}
		}},

		// --- Repository scanner (M6) ---
		// Runs LAST (BV-101, decision 4): the fast, always-applicable steps above
		// (shellprofile, scheduledtasks, avexclusions, globals) finish first, so a
		// repository scan that spends the global budget never keeps them from
		// running. The per-repo (60 s) and global (300 s) limits are unchanged.
		// The git-history scan stays per clone: LECTURA-103 did NOT confirm a
		// per-family scan without losing findings (clones can diverge from the
		// family's root commit, so each HEAD must still be enumerated, and the
		// expensive commit-diff matching is already de-duplicated per run by the
		// SHA-keyed cache), so it is not changed here.
		{Name: "repos", Run: func(ctx context.Context) func() {
			var repoFamilies [][]string
			// The repository scan's allow-list hits come from its
			// goroutines (B9): gathered here, handed over in the record.
			var (
				hitsMu   sync.Mutex
				repoHits []config.AllowHit
			)
			scanCfg := repos.ScanConfig{
				Patterns:     repoPatterns,
				AllowHooks:   cfg.AllowList.GitHooks,
				AllowHistory: cfg.AllowList.SecretsInHistory,
				OnFamilies:   func(f [][]string) { repoFamilies = f },
				OnAllowHit: func(h config.AllowHit) {
					hitsMu.Lock()
					defer hitsMu.Unlock()
					repoHits = append(repoHits, h)
				},
				OnProgress:      progress.repos,
				OnCoverage:      func(notReviewed int) { reposNotReviewed = notReviewed },
				OnBinarySkipped: func(n int) { binarySkipped = n },
			}
			if saltErr == nil {
				// Committed secrets become salted references in the scan;
				// their values are discarded at once (CTO-7 §2.2).
				scanCfg.Identify = graph.Identifier(salt)
			}
			scanned, newRepoBaselines, repoErrs := repos.ScanAllRepos(
				ctx, repoList, st.Baselines.Repos, scanCfg,
			)
			return func() {
				families = repoFamilies
				hitsMu.Lock()
				allowHits = append(allowHits, repoHits...)
				hitsMu.Unlock()
				repoFindings = scanned
				findings = append(findings, scanned...)
				scanErrors = append(scanErrors, repoErrs...)
				// Update repo baselines in state.
				st.Baselines.Repos = newRepoBaselines
			}
		}},
	}
	progress.line("console.progress.agents", nil)
	// Run surfaces both a global-timeout error and any step that panicked
	// (BV-202 S1); all become visible errors[] so the run is fail-secure.
	scanErrors = append(scanErrors, scan.Run(ctx, steps)...)
	if buildGraph != nil {
		// The graph build runs outside scan.Run, so a panic here would otherwise
		// bring the program down with no report (BV-202 C1, CTO-28). Contain it
		// with the same recover: the graph stays unbuilt (E10), surfaced as a
		// visible scope-"graph" error, and graphDone stays false so any stale
		// graph is removed below.
		if se := scan.RecoverStep("graph", func() {
			buildGraph(graph.Repos{Findings: repoFindings, Families: familyRoots(families)})
		}); se != nil {
			scanErrors = append(scanErrors, *se)
		}
	}
	// A graph step that never returned leaves no graph of this run: remove a
	// previous one, as when the graph cannot be built.
	if !graphDone {
		if rmErr := graph.RemoveStale(dataDir); rmErr != nil {
			scanErrors = append(scanErrors, finding.ScanError{Scope: "graph", Message: rmErr.Error()})
		}
	}

	// The global time limit (BV-155): whether it expired this run, and how many
	// projects it left unreviewed (every project when the repos step never ran).
	limitReached := false
	for _, se := range scanErrors {
		if se.Scope == scan.GlobalTimeoutScope {
			limitReached = true
			break
		}
	}
	projectsNotReviewed := 0
	if limitReached {
		if reposNotReviewed >= 0 {
			projectsNotReviewed = reposNotReviewed
		} else {
			projectsNotReviewed = nProjects
		}
	}

	// BV-210: write this review's problems to the local notebook. Nothing
	// leaves the machine here; space_rejected and output_invalid are recorded in
	// the send path below. The agent for a config_unreadable is taken only from
	// the graph's agent product ids — never from a path or parser detail.
	recordProblems(dataDir, problemsFromRun(today(start), scanErrors, repoFindings, built)...)

	// Build report.
	elapsed := time.Since(start).Seconds()
	r := report.Build(version, findings, scanErrors, 0, 0, st.ConsecutiveGreen, allowListedTotal, nil)
	r.Header.ScanDurationSeconds = elapsed

	// The v2 report: the rules layer over this run's graph, with v1 grouped
	// in its legacy section. The run's status is the highest of both.
	bases, err := knowledge.All()
	if err != nil {
		return "", fmt.Errorf("knowledge base: %w", err)
	}
	base := bases[knowledge.ClaudeCodeProduct]
	// The score history (I9): the last entry is the comparison's base; an
	// invalid file is set aside, never deleted.
	history, historyReset, err := index.ReadHistory(dataDir)
	if err != nil {
		return "", fmt.Errorf("index history: %w", err)
	}
	if historyReset {
		slog.Warn("index history did not validate: set aside as .invalid, a new one starts", "dir", reportDir)
	}
	// The findings memory (memoria, M2): an invalid file is set aside, never
	// deleted.
	mem, memReset, err := memory.Read(dataDir)
	if err != nil {
		return "", fmt.Errorf("findings memory: %w", err)
	}
	if memReset {
		slog.Warn("findings memory did not validate: set aside as .invalid, a new one starts", "dir", reportDir)
	}
	if saltErr != nil {
		return "", fmt.Errorf("findings memory: %w", saltErr)
	}
	// A scheduled review's notification (J7): whether Windows will show it,
	// read before the report so that this report says so.
	var nt notice
	if unattended {
		nt = defaultNotifier.probe(ctx)
	}
	v2, ruled := report.BuildV2(report.V2Input{
		Graph: built, GraphSHA256: builtSHA, Base: base, Bases: bases, RepoRoots: repoList, Projects: projectsCoverage(search, prevRepos, homeDir),
		Families: families, V1: r, GeneratedAt: time.Now(),
		Previous: history.Last(), HistoryReset: historyReset, History: history.Entries,
		Salt: salt, Memory: mem, MemoryReset: memReset, PriorRuns: st.HasRun() || priorReportsExist(reportDir),
		AllowHits: allowHits,
		Rotated:   cfg.AllowList.RotatedCredentials, Fixtures: cfg.AllowList.TestFixtures, Dismissed: cfg.AllowList.Dismissed, ConfigFile: absPath(cfgPath),
		VSCodeFirstRun: vscodeFirstRun, ClaudeSettings: builtSettings, Home: homeDir, Hermes: hermesScan, Codex: codexScan, Grok: grokScan, Cline: clineScan,
		IntegrityNotChecked: integrityNotChecked,
		StoreUpdateOld:      storeUpd.Old, StoreUpdateNew: storeUpd.New,
		ExePath: exePath, Weekly: weekly, WeeklyNoSystemd: weeklyNoSystemd, NotificationUnavailable: nt.code, NotChecked: notChecked,
		IntelVersion: intelVersion(intelMatcher), IntelPublishedAt: intelPublishedAt(intelMatcher),
		LimitMinutes: limitMinutes, LimitReached: limitReached, ProjectsNotReviewed: projectsNotReviewed,
		BinarySkipped: binarySkipped,
	})
	launcherRules := make([]string, 0, len(ruled.Launchers))
	for _, l := range ruled.Launchers {
		launcherRules = append(launcherRules, l.Rule)
	}
	slog.Info("report: launcher rules counted as unrestricted shell", "count", len(launcherRules), "rules", launcherRules)
	slog.Info("report: v2", "status", v2.Status, "findings", len(v2.Findings), "today", v2.Today.FindingID, "action", v2.Today.ActionID)

	// Closed package for the connected space (BV-119/BV-137 points 6–7):
	// sealed only when a kit is present and a graph was built, then sent if
	// this computer is connected. Both happen before the report is written, so
	// the «Your space» section reflects this run (sent today, or pending).
	pkgNow := time.Now()
	stored, kitErr := loadKitIfPresent()
	// A failure to build, seal or read the kit for this run's package never
	// stops the review (BV-152 point 1): the local review is done and its report
	// is always written. pkgFailed only changes what the «Your space» section
	// says — «this check couldn't be sent» instead of «pending».
	pkgFailed := false
	if kitErr != nil {
		slog.Warn("results package skipped: kit could not be read", "err", kitErr)
		pkgFailed = true
	}
	var currentPkg string
	if stored != nil && built != nil {
		pkgPath, err := writeResultsPkg(v2, built, homeDir, version,
			stored.Secret.Key, stored.KitID, kit.KeyID(stored.Secret.Key), pkgNow, dataDir)
		switch {
		case err != nil:
			// Building or sealing the package failed (for example the space view
			// does not fit its schema). The error carries only field paths, no
			// values (tested in TestResultsPackageErrorHasNoValues), so logging
			// it is safe.
			slog.Warn("results package skipped", "err", err)
			pkgFailed = true
			// BV-201 point 3: never in silence. Record a "failed" line in the
			// sends log with the schema field path(s) only — never the value —
			// so a package that could not be built leaves a trace next to the
			// sends, not just a console line that is lost.
			if built != nil {
				fields := strings.Join(report.SchemaErrorFields(err), ",")
				appendSendLog(filepath.Join(dataDir, SendsLogName), pkgNow,
					report.ResultsFileName(pkgNow, built.Provenance.HostID), 0, sendFailed, fields)
			}
		case pkgPath != "":
			currentPkg = pkgPath
			slog.Info("results package written", "path", pkgPath)
		}
	}

	// Send to the connected space (point 6): pending first, then this run's
	// package. Never fails the run — offline leaves it for next time, a 401
	// forgets the space. Then set the «Your space» section (point 7).
	if storePath, perr := kit.StorePath(); perr == nil {
		sp := spacePaths{store: storePath, state: stPath, reports: dataDir}
		sendCtx, cancelSend := context.WithTimeout(context.Background(), 2*time.Minute)
		spaceSend(sendCtx, sp, currentPkg, pkgNow)
		cancelSend()
		// «Connected» is the permission AND a credential (CTO-15 PR2); a 401 or
		// 409 during the send forgets both, so the section then shows as not
		// connected. BV-172: the rejection of this run's package is shown either
		// way — a 409 key_mismatch forgets the space but the report must still
		// say the check could not be sent (report.space_rejected) and offer to
		// connect again. The send log carries «rejected» even after the forget.
		v2.SpaceConnected = isSpaceConnected(sp)
		// This run's package: sent (with this send's date), refused, or still
		// pending (CTO-15 PR5).
		v2.SpaceSentDate, v2.SpaceRejected = currentPackageStatus(dataDir, currentPkg, time.Local)
		// If this run had no package to send because it could not be built or
		// sealed, say so (BV-152 point 1): «rejected», not «pending».
		if pkgFailed {
			v2.SpaceSentDate, v2.SpaceRejected = "", true
		}
	} else {
		slog.Warn("space send skipped: kit path", "err", perr)
	}

	// BV-210 §2: before the report is written, if the «send the next ones
	// without asking» checkbox is on, send the unsent problems now (so the
	// report's line is right). Then set the two report lines: how many problems
	// were sent this run (checkbox) and how many remain unsent. The interactive
	// Window A, if any, comes after the report opens.
	if st.SendProblemsWithoutAsking {
		pctx, pcancel := context.WithTimeout(context.Background(), 2*time.Minute)
		// CTO-32 C1: the consent is read from the state inside spacenet.
		if consent, cerr := spacenet.ConsentFromState(stPath); cerr == nil {
			if res := submitProblems(pctx, consent, dataDir, lang, version, nil, nil, time.Now()); res.Delivered {
				v2.ProblemsSentThisRun = res.Sent
				problemsAutoSentThisRun = res.Sent
			}
		}
		pcancel()
	}
	if left, lerr := problems.New(dataDir).Unsent(problems.MaxPerSend); lerr == nil {
		v2.ProblemsUnsent = len(left)
	}
	// BV-211d: when someone is present, the checkbox is off and this run has not
	// already asked (Window B), the after-report step will show Window A for the
	// unsent problems. In that case the report must not print the «we'll ask you
	// next time» note — the program is about to ask now. The note stays only when
	// the program truly cannot ask: no one present, or the ask was already made
	// and declined this run.
	v2.ProblemsWillAsk = presentForProblems(m, unattended) && !st.SendProblemsWithoutAsking && !problemsAskedThisRun

	// Write report files.
	progress.line("console.progress.report", nil)
	mdPath, jsonPath, v2Path, err := report.WriteV2(r, v2, time.Local, dataDir)
	if err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}
	htmlPath, err = report.WriteHTMLPages(v2, lang, homeDir, time.Local, reportDir)
	if err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}
	slog.Info("html report written", "html", htmlPath)
	aiPath, err := report.WriteAIExport(v2, cat, homeDir, dataDir)
	if err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}
	slog.Info("ai export written", "ai", aiPath)
	if err := index.WriteHistory(dataDir, history, index.EntryOf(v2.Index, time.Now().Format("2006-01-02"))); err != nil {
		return "", fmt.Errorf("index history: %w", err)
	}
	if err := memory.Write(dataDir, v2.NextMemory()); err != nil {
		return "", fmt.Errorf("findings memory: %w", err)
	}
	slog.Info("report written", "md", mdPath, "json", jsonPath, "v2", v2Path)

	// BV-183: prune old sealed packages — those whose name is more than 30 days
	// old and whose last send result is terminal. Nothing else is removed.
	pruneResultsPackages(dataDir, time.Now(), terminalFilenames(dataDir))

	// Re-read the space permission from disk before saving the run's own
	// fields (CTO-15 PR2): spaceSend's 401 wipe, or a reverted connect, may
	// have changed it while this run held an older copy. runWith owns the
	// status and the baselines; the permission belongs to the space paths, so
	// take its latest value — a 401 during the run must end "not connected".
	if cur, curErr := state.LoadState(stPath); curErr == nil {
		st.SpacePermission = cur.SpacePermission
		// BV-172: the connect-offer decline is also a space-paths field; a 409 or
		// 401 during the send clears it (clearConnectOfferDecline) so the offer
		// shows this run. Take the latest from disk so this save does not restore
		// an old decline and suppress the offer.
		st.ConnectOfferDeclinedAt = cur.ConnectOfferDeclinedAt
	}

	// Update state with the run's status (the highest of v1 and v2).
	st.LastRun = r.GeneratedAt
	st.LastRunStatus = v2.Status
	if v2.Status == "green" {
		st.ConsecutiveGreen++
	} else {
		st.ConsecutiveGreen = 0
	}
	if err := state.SaveState(stPath, st); err != nil {
		return "", fmt.Errorf("save state: %w", err)
	}

	// BV-151: the report is written — the window says so and the taskbar clears,
	// just before the summary (and, on a double click, report_opened).
	ui.done()

	// BV-210 §2 A: the end-of-review problems step (Window A, or the untick box
	// after an automatic send) runs in the caller, AFTER the report has opened
	// (CTO-32 C3), so an unanswered window never delays the report.

	// Print summary.
	if unattended {
		// The notice, last (J7, J9): agent, score and counts, or the report
		// opened.
		if !scheduledNotice(ctx, defaultNotifier, nt, v2, cat, absPath(filepath.Join(reportDir, report.HTMLFileName))) {
			// BV-210: neither the notice nor the report reached the person.
			recordProblems(dataDir, problems.Entry{Code: problems.ReportNotOpened, Step: problems.StepNotify, Day: today(start)})
		}
		slog.Info("scheduled run finished", "status", v2.Status)
		return htmlPath, nil
	}
	// BV-183: point the person at the visual report (report.html), not the
	// Markdown that now lives in data/.
	fmt.Print("\n" + report.ConsoleSummary(v2, cat, htmlPath))

	return htmlPath, nil
}

// checkingLine is the console's «checking this computer» line (BV-155),
// chosen by the number of projects: none (agents only), one, or several, with
// the time limit in minutes.
func checkingLine(cat catalog.Catalog, projects, minutes int) string {
	switch {
	case projects == 0:
		return cat.Text("console.checking.agents_only", nil)
	case projects == 1:
		return cat.Text("console.checking.one", map[string]string{"minutes": strconv.Itoa(minutes)})
	default:
		return cat.Text("console.checking.other", map[string]string{"projects": strconv.Itoa(projects), "minutes": strconv.Itoa(minutes)})
	}
}

// compileExtraPattern compiles a single user-defined secret pattern from config.
func compileExtraPattern(spec config.SecretPattern) (secrets.Pattern, error) {
	re, err := regexp.Compile(spec.Regex)
	if err != nil {
		return secrets.Pattern{}, fmt.Errorf("compile %q: %w", spec.Name, err)
	}
	return secrets.NewPattern(spec.Name, spec.Name, re), nil
}

// writeGraph builds the v2 graph from the MCP scan, the Claude Code settings
// scan, the agent instruction files, the skills and the credential files,
// infers Claude Code's reaches and writes_config_of edges (repoRoots and
// home anchor its permission rules), and writes it to dir. Credential
// fingerprints and host_id are keyed with the state's local salt. If the sources could not be listed, or
// the salt is unusable, there is nothing trustworthy to build, so no graph is
// written and any previous one is removed.
func writeGraph(
	scan platform.MCPScan,
	listErr error,
	settings platform.ClaudeSettingsScan,
	cursor platform.CursorScan,
	hermes platform.HermesScan,
	codex platform.CodexScan,
	grok platform.GrokScan,
	copilot platform.CopilotScan,
	antigravity platform.AntigravityScan,
	gemini platform.GeminiScan,
	cline platform.ClineScan,
	instructions platform.InstructionScan,
	skillScan platform.SkillScan,
	credFiles platform.CredentialFileScan,
	exts []platform.VSCodeExtension,
	repoGraph graph.Repos,
	repoRoots []string,
	home string,
	st state.State,
	dir string,
	now time.Time,
	matcher *intel.Matcher,
	pkgObs []graph.PackageObservation,
	n8nFolder string,
	n8nKeyData bool,
) (*graph.Graph, string, string, error) {
	fail := func(err error) (*graph.Graph, string, string, error) {
		if rmErr := graph.RemoveStale(dir); rmErr != nil {
			err = errors.Join(err, rmErr)
		}
		return nil, "", "", err
	}
	if listErr != nil {
		return fail(fmt.Errorf("agent config sources could not be listed: %w", listErr))
	}
	salt, err := st.Salt()
	if err != nil {
		return fail(err)
	}
	// The whole graph assembly lives in internal/pipeline (E3, CTO-24): the one
	// function the real run, the full-machine test and the factory-note test
	// all call, so no test builds the graph on a path of its own.
	res, err := pipeline.Assemble(pipeline.Input{
		MCP:          scan,
		Settings:     settings,
		Cursor:       cursor,
		Hermes:       hermes,
		Codex:        codex,
		Grok:         grok,
		Copilot:      copilot,
		Antigravity:  antigravity,
		Gemini:       gemini,
		Cline:        cline,
		Instructions: instructions,
		Skills:       skillScan,
		CredFiles:    credFiles,
		Extensions:   exts,
		Repos:        repoGraph,
		Provenance: graph.Provenance{
			EngineVersion:    version,
			RulesVersion:     graph.RulesVersion,
			KnowledgeVersion: graph.KnowledgeVersion,
			HostID:           graph.HostID(salt),
		},
		Salt:       salt,
		RepoRoots:  repoRoots,
		Home:       home,
		Now:        now,
		Matcher:    matcher,
		PkgObs:     pkgObs,
		N8NFolder:  n8nFolder,
		N8NKeyData: n8nKeyData,
	})
	if err != nil {
		return fail(err)
	}
	if len(res.Stats.Unmodeled) > 0 || res.Stats.Malformed > 0 {
		slog.Info("graph: permission rules not modeled", "by_tool", res.Stats.Unmodeled, "malformed", res.Stats.Malformed)
	}
	g := res.Graph
	path, err := graph.WriteFile(dir, g)
	if err != nil {
		return fail(err)
	}
	// The v2 report records the hash of the file it evaluated.
	sha, err := hash.File(path)
	if err != nil {
		return fail(fmt.Errorf("hash graph: %w", err))
	}
	return &g, sha, path, nil
}

// intelVersion and intelPublishedAt are the snapshot stamp for the report, or
// empty when the snapshot did not load (the stamp is then omitted).
func intelVersion(m *intel.Matcher) string {
	if m == nil {
		return ""
	}
	return m.Version()
}

func intelPublishedAt(m *intel.Matcher) string {
	if m == nil {
		return ""
	}
	return m.PublishedAt()
}

// packageObservations turns the Python and npm packages read at rest into the
// coordinates the threat-intel cross compares (BV-100, F0a). Python packages
// are PyPI; npm and pnpm global packages are npm. Names are left as read; the
// matcher normalizes them per ecosystem.
func packageObservations(py pyenv.Result, globalObs []globals.ObservedGlobal) []graph.PackageObservation {
	obs := make([]graph.PackageObservation, 0, len(py.Packages)+len(globalObs))
	for _, p := range py.Packages {
		obs = append(obs, graph.PackageObservation{
			Ecosystem: intel.EcosystemPyPI,
			Name:      p.Name,
			Version:   p.Version,
			Path:      p.Path,
		})
	}
	for _, g := range globalObs {
		obs = append(obs, graph.PackageObservation{
			Ecosystem: intel.EcosystemNPM,
			Name:      g.Name,
			Version:   g.Version,
			Path:      g.Path,
		})
	}
	return obs
}

// familyRoots maps every repository root to the root of its family's first
// repository.
func familyRoots(families [][]string) map[string]string {
	out := map[string]string{}
	for _, fam := range families {
		for _, root := range fam {
			out[root] = fam[0]
		}
	}
	return out
}

// absPath is p made absolute when it can be (the configuration file the
// rotate_credential action edits).
func absPath(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// loadKitIfPresent reads the kit key of this computer (BV-119): (nil, nil)
// when there is no kit yet, the stored kit when there is one, an error when
// the kit file exists but cannot be read. The run writes the closed package
// only when a kit is present.
func loadKitIfPresent() (*kit.Stored, error) {
	path, err := kit.StorePath()
	if err != nil {
		return nil, err
	}
	return loadKitFrom(path)
}

// loadKitFrom reads the kit at an explicit path; a missing file is (nil, nil).
func loadKitFrom(path string) (*kit.Stored, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	stored, err := kit.Load(path)
	if err != nil {
		return nil, err
	}
	return &stored, nil
}
