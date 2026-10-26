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
	"time"

	windowsadapter "github.com/qtorb/rosmarops/adapters/windows"
	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/finding"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/hash"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/notify"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan"
	"github.com/qtorb/rosmarops/internal/scan/avexclusions"
	"github.com/qtorb/rosmarops/internal/scan/credfiles"
	"github.com/qtorb/rosmarops/internal/scan/globals"
	"github.com/qtorb/rosmarops/internal/scan/mcp"
	"github.com/qtorb/rosmarops/internal/scan/repos"
	"github.com/qtorb/rosmarops/internal/scan/scheduledtasks"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
	"github.com/qtorb/rosmarops/internal/scan/shellprofile"
	"github.com/qtorb/rosmarops/internal/scan/vscode"
	"github.com/qtorb/rosmarops/internal/state"
)

// runLangs are the languages of --lang (decision 2026-10-23, idioma): the
// user chooses; the system's language is never detected.
var runLangs = map[string]bool{"en": true, "es": true}

func newRunCmd() *cli.Command {
	return &cli.Command{
		Name: "run", Short: "cli.cmd.run.short", Long: "cli.cmd.run.long", Args: cli.AnyArgs,
		Flags: []cli.Flag{
			{Name: "lang", Default: catalog.DefaultLang, Usage: "cli.flag.lang"},
			// A scheduled review (decision 2026-10-26, integridad y revisión
			// semanal, J3): left out of the help, which has no text for it.
			{Name: "unattended", Bool: true, Hidden: true},
		},
		Before: integrityCheck,
		Run:    runRun,
	}
}

func runRun(inv cli.Invocation) (err error) {
	start := time.Now()
	lang := inv.Flags["lang"]
	if !runLangs[lang] {
		return fmt.Errorf("lang_unsupported: %q (en, es)", lang)
	}
	// A scheduled review (J3): no window, never reads input, its log in
	// the reports directory.
	unattended := inv.Flags["unattended"] == "true"
	var runLog *deferredLog
	if unattended {
		freeConsole()
		runLog = &deferredLog{}
		slog.SetDefault(slog.New(slog.NewTextHandler(runLog, nil)))
		defer func() {
			if err != nil {
				slog.Error("scheduled run failed", "err", err)
				if dir, dirErr := report.DefaultReportDir(); dirErr == nil {
					_ = runLog.attach(dir) // a failure before the reports directory was known
				}
			}
		}()
	}
	// Invariant §6: every scanner and subprocess runs under the global cap.
	ctx, cancel := context.WithTimeout(context.Background(), scan.GlobalTimeout)
	defer cancel()

	// Load config: a scheduled review reads only ~/.rosmarops/config.json,
	// never one in its working directory (J3).
	resolve := config.ResolveConfigPath
	if unattended {
		resolve = config.HomeConfigPath
	}
	cfgPath, err := resolve()
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Load state (may not exist on first run after init but before baseline).
	stPath, err := state.StatePath()
	if err != nil {
		return fmt.Errorf("resolve state path: %w", err)
	}
	st, stErr := state.LoadState(stPath)
	if stErr != nil {
		slog.Warn("could not load state; treating as first run", "err", stErr)
		st, err = state.DefaultState(version)
		if err != nil {
			return fmt.Errorf("create fallback state: %w", err)
		}
	}

	// Determine report directory.
	reportDir := cfg.ReportDirectory
	if reportDir == "" {
		reportDir, err = report.DefaultReportDir()
		if err != nil {
			return fmt.Errorf("resolve report dir: %w", err)
		}
	}

	if unattended {
		if err := runLog.attach(reportDir); err != nil {
			return err
		}
	}

	cat, err := catalog.Load(lang)
	if err != nil {
		return fmt.Errorf("message catalog: %w", err)
	}
	// Integrity (J1–J3): the state, the binary and the configuration
	// against the references init recorded; a change is confirmed in the
	// console or the run stops with a red report. A scheduled review never
	// reads input.
	ask := asker{in: os.Stdin, out: os.Stdout, terminal: canAsk(os.Stdin, os.Stdout), cat: cat}
	if unattended {
		ask.in, ask.out, ask.terminal = strings.NewReader(""), io.Discard, false
	}
	integrityNotChecked, err := integrityRun{
		ask: ask, st: &st, stErr: stErr, stPath: stPath, cfgPath: cfgPath, reportDir: reportDir,
		unattended: unattended, lang: lang, selfHash: hash.Self,
	}.check()
	if err != nil {
		return err
	}
	progress := newProgress(os.Stdout, isTerminal(os.Stdout), cat)

	var (
		findings         []finding.Finding
		scanErrors       []finding.ScanError
		allowListedTotal int
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

	// Resolve home dir once — used by the MCP writable-path check.
	homeDir, homeDirErr := os.UserHomeDir()
	if homeDirErr != nil {
		slog.Warn("run: could not resolve home dir; mcp_server_user_writable_command check disabled", "err", homeDirErr)
	}

	plat := windowsadapter.New()

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
		mcpScan   platform.MCPScan
		mcpErr    error
		credFiles platform.CredentialFileScan
		built     *graph.Graph // the graph written this run, for the v2 report
		builtSHA  string
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
				st.Baselines.VSCodeExtensions = extResult.NewBaseline
				if extResult.FirstRun && extResult.Recorded > 0 {
					vscodeFirstRun = &report.VSCodeFirstRun{Recorded: extResult.Recorded, Recent: extResult.Recent}
				}
			}
		}},

		// --- MCP server scanner (M5) ---
		{Name: "mcp", Run: func(ctx context.Context) func() {
			scanned, listErr := plat.ListMCPServers(ctx, cfg.RepoRoots)
			// Only the servers a client loads (A4); no reference: new and
			// modified servers are the v2 memory's, or retired.
			mcpResult := mcp.ScanMCPServers(scanned.Servers, cfg.AllowList.MCPServers, homeDir)
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
			}
		}},

		// --- Credential files collector (decision 2026-10-24, D1, D2): .env
		// files of every repo and the user's credential files, for the graph.
		// No git, no subprocess; outside the per-repo budget. ---
		{Name: "credentials", Run: func(ctx context.Context) func() {
			scanned := credfiles.Collect(homeDir, cfg.RepoRoots, nil)
			// The envFile of Cursor's MCP servers (decision 2026-10-25,
			// Cursor, K10).
			var envFiles []string
			for _, srv := range mcpScan.Servers {
				if srv.EnvFile != "" {
					envFiles = append(envFiles, srv.EnvFile)
				}
			}
			scanned = credfiles.AddEnvFiles(scanned, envFiles)
			return func() { credFiles = scanned }
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
			instructionsErr = errors.Join(instructionsErr, cursorErr)
			if ctx.Err() != nil {
				settingsErr = errors.Join(settingsErr, fmt.Errorf("claude settings: %w", ctx.Err()))
			}
			listErr := errors.Join(mcpErr, settingsErr, instructionsErr)
			return func() {
				graphDone = true
				// The graph is built once the repositories are scanned: the
				// secrets committed in them are part of it (CTO-7 §2.2).
				buildGraph = func(repoGraph graph.Repos) {
					g, graphSHA, graphPath, graphErr := writeGraph(mcpScan, listErr, settingsScan, cursorScan, instructions, credFiles, vscodeExts, repoGraph, cfg.RepoRoots, homeDir, st, reportDir, start)
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

		// --- Repository scanner (M6) ---
		{Name: "repos", Run: func(ctx context.Context) func() {
			var repoFamilies [][]string
			scanCfg := repos.ScanConfig{
				Patterns:     repoPatterns,
				AllowHooks:   cfg.AllowList.GitHooks,
				AllowHistory: cfg.AllowList.SecretsInHistory,
				OnFamilies:   func(f [][]string) { repoFamilies = f },
				OnProgress:   progress.repos,
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
				repoFindings = scanned
				findings = append(findings, scanned...)
				scanErrors = append(scanErrors, repoErrs...)
				// Update repo baselines in state.
				st.Baselines.Repos = newRepoBaselines
			}
		}},

		// --- Shell profile scanner (M7) ---
		{Name: "shellprofile", Run: func(ctx context.Context) func() {
			currentProfiles, profErr := plat.ListShellProfiles(ctx)
			profResult := shellprofile.ScanShellProfiles(currentProfiles, st.Baselines.ShellProfileSHA256)
			return func() {
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
			tasksResult := scheduledtasks.ScanScheduledTasks(currentTasks, st.Baselines.ScheduledTasks, cfg.AllowList.ScheduledTasks, self)
			return func() {
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
				st.Baselines.ScheduledTasks = tasksResult.NewBaseline
			}
		}},

		// --- AV exclusions scanner (M7) ---
		{Name: "avexclusions", Run: func(ctx context.Context) func() {
			currentExcls, avErr := plat.ListDefenderExclusions(ctx)
			avResult := avexclusions.ScanAVExclusions(currentExcls, st.Baselines.AVExclusions, cfg.AllowList.AVExclusions)
			return func() {
				if avErr != nil {
					slog.Warn("avexclusions: could not list Defender exclusions", "err", avErr)
					scanErrors = append(scanErrors, finding.ScanError{
						Scope:   "avexclusions",
						Message: avErr.Error(),
					})
				}
				findings = append(findings, avResult.Findings...)
				allowListedTotal += avResult.AllowListedCount
				st.Baselines.AVExclusions = avResult.NewBaseline
			}
		}},

		// --- Global npm/pnpm packages + npmrc scanner (M7) ---
		{Name: "globals", Run: func(ctx context.Context) func() {
			globalsResult := globals.ScanGlobals(ctx, homeDir, st.Baselines.GlobalPackages, cfg.AllowList.GlobalPackages)
			return func() {
				findings = append(findings, globalsResult.Findings...)
				allowListedTotal += globalsResult.AllowListedCount
				st.Baselines.GlobalPackages = globalsResult.NewPackageBaseline
				if globalsResult.NewNpmrcSHA != "" {
					st.Baselines.NpmrcSHA256 = globalsResult.NewNpmrcSHA
				}
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
		return fmt.Errorf("knowledge base: %w", err)
	}
	base := bases[knowledge.ClaudeCodeProduct]
	// The score history (I9): the last entry is the comparison's base; an
	// invalid file is set aside, never deleted.
	history, historyReset, err := index.ReadHistory(reportDir)
	if err != nil {
		return fmt.Errorf("index history: %w", err)
	}
	if historyReset {
		slog.Warn("index history did not validate: set aside as .invalid, a new one starts", "dir", reportDir)
	}
	// The findings memory (memoria, M2): an invalid file is set aside, never
	// deleted.
	mem, memReset, err := memory.Read(reportDir)
	if err != nil {
		return fmt.Errorf("findings memory: %w", err)
	}
	if memReset {
		slog.Warn("findings memory did not validate: set aside as .invalid, a new one starts", "dir", reportDir)
	}
	if saltErr != nil {
		return fmt.Errorf("findings memory: %w", saltErr)
	}
	// A scheduled review's notification (J7): whether Windows will show it,
	// read before the report so that this report says so.
	notificationUnavailable := ""
	if unattended {
		if setting, err := notify.Run(ctx, ""); err != nil {
			slog.Warn("notify: setting not read", "err", err)
		} else if code, off := notify.Unavailable(setting); off {
			notificationUnavailable = code
		}
	}
	v2, ruled := report.BuildV2(report.V2Input{
		Graph: built, GraphSHA256: builtSHA, Base: base, Bases: bases, RepoRoots: cfg.RepoRoots,
		Families: families, V1: r, GeneratedAt: time.Now(),
		Previous: history.Last(), HistoryReset: historyReset, History: history.Entries,
		Salt: salt, Memory: mem, MemoryReset: memReset,
		Rotated: cfg.AllowList.RotatedCredentials, Fixtures: cfg.AllowList.TestFixtures, ConfigFile: absPath(cfgPath),
		VSCodeFirstRun: vscodeFirstRun, ClaudeSettings: builtSettings, Home: homeDir,
		IntegrityNotChecked: integrityNotChecked,
		ExePath:             exePath, Weekly: weekly,
	})
	launcherRules := make([]string, 0, len(ruled.Launchers))
	for _, l := range ruled.Launchers {
		launcherRules = append(launcherRules, l.Rule)
	}
	slog.Info("report: launcher rules counted as unrestricted shell", "count", len(launcherRules), "rules", launcherRules)
	slog.Info("report: v2", "status", v2.Status, "findings", len(v2.Findings), "today", v2.Today.FindingID, "action", v2.Today.ActionID)

	// Write report files.
	progress.line("console.progress.report", nil)
	mdPath, jsonPath, v2Path, err := report.WriteV2(r, v2, time.Local, reportDir)
	if err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	htmlPath, err := report.WriteHTML(v2, cat, homeDir, time.Local, reportDir, report.FilenameV2(r, v2))
	if err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	slog.Info("html report written", "html", htmlPath)
	aiPath, err := report.WriteAIExport(v2, cat, homeDir, reportDir, report.FilenameV2(r, v2))
	if err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	slog.Info("ai export written", "ai", aiPath)
	if err := index.WriteHistory(reportDir, history, index.EntryOf(v2.Index, time.Now().Format("2006-01-02"))); err != nil {
		return fmt.Errorf("index history: %w", err)
	}
	if err := memory.Write(reportDir, v2.NextMemory()); err != nil {
		return fmt.Errorf("findings memory: %w", err)
	}
	slog.Info("report written", "md", mdPath, "json", jsonPath, "v2", v2Path)

	// Update state with the run's status (the highest of v1 and v2).
	st.LastRun = r.GeneratedAt
	st.LastRunStatus = v2.Status
	if v2.Status == "green" {
		st.ConsecutiveGreen++
	} else {
		st.ConsecutiveGreen = 0
	}
	if err := state.SaveState(stPath, st); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	// Print summary.
	if unattended {
		// The notification, last (J7): agent, score and counts.
		if text, ok := report.NotificationText(v2, cat); ok && notificationUnavailable == "" {
			notifyText(text, filepath.Join(reportDir, report.HTMLFileName))
		}
		slog.Info("scheduled run finished", "status", v2.Status)
		return nil
	}
	fmt.Print("\n" + report.ConsoleSummary(v2, cat, mdPath))

	return nil
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
// scan, the agent instruction files and the credential files, infers Claude Code's reaches and
// writes_config_of edges (repoRoots and home anchor its permission rules),
// and writes it to dir. Credential fingerprints and host_id are
// keyed with the state's local salt. If the sources could not be listed, or
// the salt is unusable, there is nothing trustworthy to build, so no graph is
// written and any previous one is removed.
func writeGraph(
	scan platform.MCPScan,
	listErr error,
	settings platform.ClaudeSettingsScan,
	cursor platform.CursorScan,
	instructions platform.InstructionScan,
	credFiles platform.CredentialFileScan,
	exts []platform.VSCodeExtension,
	repoGraph graph.Repos,
	repoRoots []string,
	home string,
	st state.State,
	dir string,
	now time.Time,
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
	frag, stats, err := graph.FromScansWithRepos(scan, settings, cursor, credFiles, repoGraph, salt, now)
	if err != nil {
		return fail(err)
	}
	frag = graph.AddInstructions(frag, instructions, now)
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
