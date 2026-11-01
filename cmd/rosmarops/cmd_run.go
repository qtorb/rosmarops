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
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan"
	"github.com/qtorb/rosmarops/internal/scan/avexclusions"
	"github.com/qtorb/rosmarops/internal/scan/configkeys"
	"github.com/qtorb/rosmarops/internal/scan/credfiles"
	"github.com/qtorb/rosmarops/internal/scan/globals"
	"github.com/qtorb/rosmarops/internal/scan/mcp"
	"github.com/qtorb/rosmarops/internal/scan/pyenv"
	"github.com/qtorb/rosmarops/internal/scan/repos"
	"github.com/qtorb/rosmarops/internal/scan/scheduledtasks"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
	"github.com/qtorb/rosmarops/internal/scan/shellprofile"
	"github.com/qtorb/rosmarops/internal/scan/vscode"
	"github.com/qtorb/rosmarops/internal/state"
)

// runLangs are the languages of --lang. Without it, the Windows display
// language when it has a translation, else English (decision 2026-10-27,
// primer uso con un solo .exe, U7).
var runLangs = map[string]bool{"en": true, "es": true}

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
	newScanContext           = func() (context.Context, context.CancelFunc) {
		return context.WithTimeout(context.Background(), scan.GlobalTimeout)
	}
)

func runRun(inv cli.Invocation) error {
	unattended := inv.Flags["unattended"] == "true"
	_, err := runWith(inv.Flags["lang"], runMode{unattended: unattended})
	if err != nil && unattended {
		// B3 (BV-93): a scheduled review never fails in silence — a red page,
		// a notification and the run_failed exit code. Never returns.
		abortRun(abortOptions{lang: inv.Flags["lang"], unattended: true}, err)
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

	if runLog != nil {
		if err := runLog.attach(defaultDir); err != nil {
			return "", err
		}
	}

	cat, err := catalog.Load(lang)
	if err != nil {
		return "", fmt.Errorf("message catalog: %w", err)
	}
	// Integrity (J1–J3): the state, the binary and the configuration
	// against the references init recorded; a change is confirmed in the
	// console or the run stops with a red report. A scheduled review never
	// reads input.
	ask := asker{in: runInput, out: os.Stdout, terminal: runCanAsk(), cat: cat}
	if unattended {
		ask.in, ask.out, ask.terminal = strings.NewReader(""), io.Discard, false
	}
	integrityNotChecked, err := integrityRun{
		ask: ask, st: &st, stErr: stErr, stPath: stPath, cfgPath: cfgPath, reportDir: defaultDir,
		unattended: unattended, window: m.window, lang: lang, selfHash: hash.Self,
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
	// Invariant §6: every scanner and subprocess runs under the global cap,
	// which counts from after the integrity questions: the time a person
	// takes to answer is not the scan's (J13).
	ctx, cancel := newScanContext()
	defer cancel()
	progress := newProgress(os.Stdout, isTerminal(os.Stdout), cat)

	var (
		findings         []finding.Finding
		scanErrors       []finding.ScanError
		allowListedTotal int
		// allowHits are the warnings the configuration hid, for the
		// report (decision 2026-10-27, blindaje, B9).
		allowHits []config.AllowHit
	)

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
	langs := make([]string, 0, len(runLangs))
	for l := range runLangs {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	self := scheduledtasks.Self{Exe: exePath, Domain: os.Getenv("USERDOMAIN"), User: os.Getenv("USERNAME"), Langs: langs}
	weekly := scheduledtasks.Weekly{State: scheduledtasks.WeeklyNone}
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
		mcpScan    platform.MCPScan
		mcpErr     error
		hermesScan platform.HermesScan // BV-111: config.yaml drives Hermes's caps
		credFiles  platform.CredentialFileScan
		built      *graph.Graph // the graph written this run, for the v2 report
		builtSHA   string
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
			scanned, listErr := plat.ListMCPServers(ctx, cfg.RepoRoots)
			// Hermes Agent's config.yaml (N2a, BV-97): its MCP servers and
			// its sources join every agent's; a config.yaml not read is a
			// failed source (B6).
			hermes, hermesErr := plat.ListHermes(ctx)
			scanned.Sources = append(scanned.Sources, hermes.Sources...)
			scanned.Servers = append(scanned.Servers, hermes.Servers...)
			hermesScan = hermes // BV-111: kept for the graph step (approvals.mode, terminal.backend)
			listErr = errors.Join(listErr, hermesErr)
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
			scanned := credfiles.CollectOn(agentdirs.Current(), homeDir, cfg.RepoRoots, nil)
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
			settingsScan, settingsErr := plat.ListClaudeSettings(ctx, cfg.RepoRoots)
			instructions, instructionsErr := plat.ListAgentInstructions(ctx, cfg.RepoRoots)
			cursorScan, cursorErr := plat.ListCursor(ctx, cfg.RepoRoots)
			skillScan, skillsErr := plat.ListSkills(ctx, cfg.RepoRoots)
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
					pyDirs, pyErr := plat.PythonSitePackages(cfg.RepoRoots)
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
					pkgObs := packageObservations(pyResult, globalObs)
					g, graphSHA, graphPath, graphErr := writeGraph(mcpScan, listErr, settingsScan, cursorScan, hermesScan, instructions, skillScan, credFiles, vscodeExts, repoGraph, cfg.RepoRoots, homeDir, st, reportDir, start, intelMatcher, pkgObs)
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
				OnProgress: progress.repos,
			}
			if saltErr == nil {
				// Committed secrets become salted references in the scan;
				// their values are discarded at once (CTO-7 §2.2).
				scanCfg.Identify = graph.Identifier(salt)
			}
			scanned, newRepoBaselines, repoErrs := repos.ScanAllRepos(
				ctx, cfg.RepoRoots, st.Baselines.Repos, scanCfg,
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
	if timeoutErr := scan.Run(ctx, steps); timeoutErr != nil {
		scanErrors = append(scanErrors, *timeoutErr)
	}
	if buildGraph != nil {
		buildGraph(graph.Repos{Findings: repoFindings, Families: familyRoots(families)})
	}
	// A graph step that never returned leaves no graph of this run: remove a
	// previous one, as when the graph cannot be built.
	if !graphDone {
		if rmErr := graph.RemoveStale(reportDir); rmErr != nil {
			scanErrors = append(scanErrors, finding.ScanError{Scope: "graph", Message: rmErr.Error()})
		}
	}

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
	history, historyReset, err := index.ReadHistory(reportDir)
	if err != nil {
		return "", fmt.Errorf("index history: %w", err)
	}
	if historyReset {
		slog.Warn("index history did not validate: set aside as .invalid, a new one starts", "dir", reportDir)
	}
	// The findings memory (memoria, M2): an invalid file is set aside, never
	// deleted.
	mem, memReset, err := memory.Read(reportDir)
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
		Graph: built, GraphSHA256: builtSHA, Base: base, Bases: bases, RepoRoots: cfg.RepoRoots,
		Families: families, V1: r, GeneratedAt: time.Now(),
		Previous: history.Last(), HistoryReset: historyReset, History: history.Entries,
		Salt: salt, Memory: mem, MemoryReset: memReset, PriorRuns: st.HasRun() || priorReportsExist(reportDir),
		AllowHits: allowHits,
		Rotated:   cfg.AllowList.RotatedCredentials, Fixtures: cfg.AllowList.TestFixtures, ConfigFile: absPath(cfgPath),
		VSCodeFirstRun: vscodeFirstRun, ClaudeSettings: builtSettings, Home: homeDir, Hermes: hermesScan,
		IntegrityNotChecked: integrityNotChecked,
		ExePath:             exePath, Weekly: weekly, NotificationUnavailable: nt.code, NotChecked: notChecked,
		IntelVersion: intelVersion(intelMatcher), IntelPublishedAt: intelPublishedAt(intelMatcher),
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
	if kitErr != nil {
		slog.Warn("results package skipped: kit could not be read", "err", kitErr)
	}
	var currentPkg string
	if stored != nil && built != nil {
		pkgPath, err := report.WriteResultsPackage(v2, built, homeDir, version,
			stored.Secret.Key, stored.KitID, kit.KeyID(stored.Secret.Key), pkgNow, reportDir)
		if err != nil {
			return "", fmt.Errorf("results package: %w", err)
		}
		if pkgPath != "" {
			currentPkg = pkgPath
			slog.Info("results package written", "path", pkgPath)
		}
	}

	// Send to the connected space (point 6): pending first, then this run's
	// package. Never fails the run — offline leaves it for next time, a 401
	// forgets the space. Then set the «Your space» section (point 7).
	if storePath, perr := kit.StorePath(); perr == nil {
		sp := spacePaths{store: storePath, state: stPath, reports: reportDir}
		sendCtx, cancelSend := context.WithTimeout(context.Background(), 2*time.Minute)
		spaceSend(sendCtx, sp, currentPkg, pkgNow)
		cancelSend()
		// «Connected» is the permission AND a credential (CTO-15 PR2); a 401
		// during the send forgets both, and then the section is not shown.
		if isSpaceConnected(sp) {
			v2.SpaceConnected = true
			v2.SpaceSentDate = lastSentDate(reportDir, time.Local)
		}
	} else {
		slog.Warn("space send skipped: kit path", "err", perr)
	}

	// Write report files.
	progress.line("console.progress.report", nil)
	mdPath, jsonPath, v2Path, err := report.WriteV2(r, v2, time.Local, reportDir)
	if err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}
	htmlPath, err = report.WriteHTMLPages(v2, lang, homeDir, time.Local, reportDir, report.FilenameV2(r, v2))
	if err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}
	slog.Info("html report written", "html", htmlPath)
	aiPath, err := report.WriteAIExport(v2, cat, homeDir, reportDir, report.FilenameV2(r, v2))
	if err != nil {
		return "", fmt.Errorf("write report: %w", err)
	}
	slog.Info("ai export written", "ai", aiPath)
	if err := index.WriteHistory(reportDir, history, index.EntryOf(v2.Index, time.Now().Format("2006-01-02"))); err != nil {
		return "", fmt.Errorf("index history: %w", err)
	}
	if err := memory.Write(reportDir, v2.NextMemory()); err != nil {
		return "", fmt.Errorf("findings memory: %w", err)
	}
	slog.Info("report written", "md", mdPath, "json", jsonPath, "v2", v2Path)

	// Re-read the space permission from disk before saving the run's own
	// fields (CTO-15 PR2): spaceSend's 401 wipe, or a reverted connect, may
	// have changed it while this run held an older copy. runWith owns the
	// status and the baselines; the permission belongs to the space paths, so
	// take its latest value — a 401 during the run must end "not connected".
	if cur, curErr := state.LoadState(stPath); curErr == nil {
		st.SpacePermission = cur.SpacePermission
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

	// Print summary.
	if unattended {
		// The notice, last (J7, J9): agent, score and counts, or the report
		// opened.
		text, ok := report.NotificationText(v2, cat)
		defaultNotifier.finish(ctx, nt, text, ok, absPath(filepath.Join(reportDir, report.HTMLFileName)))
		slog.Info("scheduled run finished", "status", v2.Status)
		return htmlPath, nil
	}
	fmt.Print("\n" + report.ConsoleSummary(v2, cat, mdPath))

	return htmlPath, nil
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
	frag, stats, err := graph.FromScansWithHermes(scan, settings, cursor, credFiles, repoGraph, hermes, salt, now)
	if err != nil {
		return fail(err)
	}
	frag = graph.AddInstructions(frag, instructions, now)
	frag = graph.AddSkills(frag, skillScan, now)
	frag = graph.AddExtensions(frag, exts, filepath.Join(home, ".vscode", "extensions"), now)
	if len(stats.Unmodeled) > 0 || stats.Malformed > 0 {
		slog.Info("graph: permission rules not modeled", "by_tool", stats.Unmodeled, "malformed", stats.Malformed)
	}
	g := graph.Build(frag, graph.Provenance{
		EngineVersion:    version,
		RulesVersion:     graph.RulesVersion,
		KnowledgeVersion: graph.KnowledgeVersion,
		HostID:           graph.HostID(salt),
	}, now)
	// reaches and writes_config_of are inferred on the built graph.
	g, err = graph.Infer(g, repoRoots, home)
	if err != nil {
		return fail(err)
	}
	// Threat-intel cross (BV-100, F0a): mark matching mcp_server nodes and
	// append one file resource node per matching package read at rest, before
	// the graph is serialized so the match is reflected in the written file and
	// its hash. A nil matcher (invalid snapshot) makes this a no-op.
	graph.CrossIntel(&g, scan.Servers, pkgObs, matcher, now)
	// MCP catalog cross (BV-117/BV-121): mark mcp_server nodes of known servers
	// that act outside the machine. In-memory only (ExternalAction), so it does
	// not change the written file; the rules layer reads it.
	catalog, err := knowledge.LoadMCPCatalog()
	if err != nil {
		return nil, "", "", fmt.Errorf("mcp catalog: %w", err)
	}
	graph.CrossMCPCatalog(&g, scan.Servers, catalog, now)
	path, err := graph.WriteFile(dir, g)
	if err != nil {
		return nil, "", "", err
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
