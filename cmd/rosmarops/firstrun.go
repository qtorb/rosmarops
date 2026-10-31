package main

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/firstrun"
	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/kitfind"
	"github.com/qtorb/rosmarops/internal/notify"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
)

// A double click on rosmarops.exe, no arguments (decision 2026-10-27,
// primer uso con un solo .exe, U1–U5): on first use, an init with the
// projects found (or chosen in the folder picker); then a run; then the
// report opens. The window shows the progress; the log goes to the reports
// directory.

// The double click's seams; tests replace them.
var (
	detectProjects = func() firstrun.Result { return firstrun.Detect(firstrun.EnvFromOS()) }
	pickedProjects = func(dir string) firstrun.Result { return firstrun.Picked(firstrun.EnvFromOS(), dir) }
	pickFolder     = firstrun.Pick
	openReportFile = notify.OpenReport
	closeDelay     = 3 * time.Second // the window closes by itself after a run that opened the report
	// BV-131 seams, replaced in tests. Off Windows they are no-ops
	// (ErrNotApplicable / a window that never opens), so the double click
	// behaves exactly as before.
	downloadsDir     = kitfind.DownloadsDir
	scanDownloads    = kitfind.Scan
	askKitPhrase     = firstrun.AskKitPhrase
	revealInExplorer = notify.RevealInExplorer
	kitStorePath     = kit.StorePath
)

// runNoArgs is the double click; it returns the exit code.
func runNoArgs() int {
	lang := systemLang()
	cat, err := catalog.Load(lang)
	if err != nil {
		cat, _ = catalog.Load(catalog.DefaultLang)
	}
	log := &deferredLog{name: RunLogName}
	defer log.Close()
	slog.SetDefault(slog.New(slog.NewTextHandler(log, nil)))
	fail := func(err error) int {
		slog.Error("run failed", "err", err)
		dir, dirErr := report.DefaultReportDir()
		if dirErr == nil {
			_ = log.attach(dir) // a failure before the reports directory was known
		}
		fmt.Fprintln(os.Stdout, "error:", err)
		if dirErr == nil {
			fmt.Fprintln(os.Stdout, filepath.Join(dir, RunLogName))
		}
		pressEnter(cat)
		return 1
	}

	// U1: first use when neither the configuration nor the state exists.
	// One of the two alone is not a first use: J1 applies, as in run.
	cfgPath, err := config.HomeConfigPath()
	if err != nil {
		return fail(fmt.Errorf("resolve config path: %w", err))
	}
	stPath, err := state.StatePath()
	if err != nil {
		return fail(fmt.Errorf("resolve state path: %w", err))
	}
	if !fileExists(cfgPath) && !fileExists(stPath) {
		// B3 (BV-93): reports already in the folder mean this is not a first
		// use — the configuration and the state were lost or removed. Never
		// reinitialize in silence: treat it as J1, state lost, with the red
		// page (it never returns). Only a truly empty reports folder is a
		// first use.
		if reportDir, dirErr := report.DefaultReportDir(); dirErr == nil && priorReportsExist(reportDir) {
			slog.Warn("double click: reports present but no config or state — treating as state lost (J1)")
			abortIntegrity("binary_hash_changed",
				"RosmarOps lost its integrity reference: the state was lost or reset.",
				"If you reset or reinstalled RosmarOps yourself, run it by hand and confirm. If not, inspect ~/.rosmarops.",
				abortOptions{lang: lang, stateLost: true, window: true})
			return exitIntegrity
		}
		if _, _, err := initialize(cfgPath, firstRunRepos(cat)); err != nil {
			return fail(err)
		}
	}

	// BV-131 (AP-20 / CTO-12): passwordless onboarding. If this computer has no
	// kit yet, look in Downloads for a kit file and offer the phrase window; a
	// correct phrase imports the kit so this very run writes the closed package.
	// Windows only; elsewhere the seams no-op and nothing changes.
	maybeImportKitFromDownloads(cat, lang)

	fmt.Println(cat.Text("console.checking", nil))
	var pkgPath string
	htmlPath, err := runWith(lang, runMode{window: true, log: log, onPackage: func(p string) { pkgPath = p }})
	if err != nil {
		// B3 (BV-93): a double click never fails in silence — the red
		// "could not finish" page, opened. The run's log was already flushed
		// by runWith's failure path. Never returns.
		abortRun(abortOptions{lang: lang, window: true}, err)
		return exitRunFailed
	}
	// BV-131 (CTO-12 H4): if this run wrote the closed package, open Explorer
	// with that file selected, so the person can drag it into their space.
	// RevealInExplorer declines a UNC or quote-bearing path itself (H4); off
	// Windows it is ErrNotApplicable.
	revealPackage(pkgPath)

	if openReport(htmlPath) {
		fmt.Println(cat.Text("console.report_opened", nil))
		if isTerminal(os.Stdout) {
			time.Sleep(closeDelay)
		}
		return 0
	}
	fmt.Println(cat.Text("console.report_not_opened", map[string]string{"path": absPath(htmlPath)}))
	pressEnter(cat)
	return 0
}

// maybeImportKitFromDownloads is BV-131's Downloads onboarding (AP-20, CTO-12
// H2/H3). It runs only on the double click and only when this computer has no
// kit yet. It never writes to Downloads and never deletes anything; the only
// write is the kit key the invariant already allows, and only after a correct
// phrase. «Not now», a closed window, five wrong phrases, Constrained Language
// Mode or any PowerShell failure all leave the run without a kit (fail closed),
// and the next double click asks again. Only the file name is logged.
func maybeImportKitFromDownloads(cat catalog.Catalog, lang string) {
	store, err := kitStorePath()
	if err != nil {
		slog.Warn("kit onboarding skipped: store path", "err", err)
		return
	}
	if fileExists(store) {
		return // this computer already has a key
	}
	dir, err := downloadsDir()
	switch {
	case errors.Is(err, platform.ErrNotApplicable):
		return // not Windows
	case err != nil:
		slog.Warn("kit onboarding skipped: downloads folder", "err", err)
		return
	}
	found := scanDownloads(dir)
	if found == nil {
		return // no kit file in Downloads
	}
	slog.Info("kit onboarding: key file found in Downloads", "file", filepath.Base(found.Path))

	cfg := firstrun.KitConfig{
		Title:  cat.Text("firstrun.kit_title", nil),
		Found:  cat.Text("firstrun.kit_found", nil),
		Ask:    cat.Text("firstrun.kit_ask", nil),
		Use:    cat.Text("firstrun.kit_use", nil),
		NotNow: cat.Text("firstrun.kit_not_now", nil),
		Wrong:  cat.Text("firstrun.kit_wrong", nil),
	}
	if found.ShowDate {
		// The report's date format, language-independent (report.go).
		cfg.Date = cat.Text("firstrun.kit_date", map[string]string{"date": found.ModTime.Local().Format("2006-01-02")})
	}

	if askKitPhrase(cfg, func(phrase string) bool {
		return importKitWithPhrase(store, found.Path, phrase)
	}) {
		slog.Info("kit onboarding: key imported from Downloads")
	} else {
		slog.Info("kit onboarding: no key imported; the review goes on without one")
	}
}

// importKitWithPhrase opens the kit file at path with phrase and, if it opens,
// keeps its key on this computer and aligns the state's local salt (as
// `rosmarops kit import` does). It returns whether the phrase opened the key.
// Any read, parse or save error is a closed door: false, so the window shows
// «wrong phrase» rather than leaking a different failure.
func importKitWithPhrase(store, path, phrase string) bool {
	f, err := readKitFile(path)
	if err != nil {
		slog.Warn("kit onboarding: kit file could not be read", "file", filepath.Base(path), "err", err)
		return false
	}
	sec, err := kit.Open(f, phrase)
	if err != nil {
		return false // wrong phrase, or any other reason the key did not open
	}
	stored, err := kit.FromFile(f, sec)
	if err != nil {
		slog.Warn("kit onboarding: kit could not be stored", "err", err)
		return false
	}
	if err := kit.Save(store, stored); err != nil {
		slog.Warn("kit onboarding: kit key could not be saved", "err", err)
		return false
	}
	if stPath, err := state.StatePath(); err == nil {
		if st, err := state.LoadState(stPath); err == nil {
			if salt := hex.EncodeToString(sec.Salt); st.LocalSalt != salt {
				st.LocalSalt = salt
				if err := state.SaveState(stPath, st); err != nil {
					slog.Warn("kit onboarding: could not align local salt", "err", err)
				}
			}
		}
	}
	return true
}

// revealPackage opens Explorer with the written package selected (BV-131 H4).
// Nothing happens without a package, off Windows, or for a path Explorer must
// not be pointed at (UNC or quote-bearing); none of that fails the run.
func revealPackage(pkgPath string) {
	if pkgPath == "" {
		return
	}
	switch err := revealInExplorer(absPath(pkgPath)); {
	case err == nil:
		slog.Info("package shown in Explorer", "path", filepath.Base(pkgPath))
	case errors.Is(err, platform.ErrNotApplicable):
		slog.Info("package not shown in Explorer", "err", err)
	default:
		slog.Warn("package not shown in Explorer", "err", err)
	}
}

// firstRunRepos is U3, and U4 when U3 finds nothing: the repositories of
// the first configuration. Every decision goes to the log.
func firstRunRepos(cat catalog.Catalog) []string {
	fmt.Println(cat.Text("console.first_run.detecting", nil))
	res := detectProjects()
	logDecisions(res)
	if len(res.Repos) > 0 {
		fmt.Println(cat.Text("console.first_run.found", map[string]string{"count": strconv.Itoa(len(res.Repos))}))
		return res.Repos
	}
	fmt.Println(cat.Text("console.first_run.none", nil))
	dir, err := pickFolder(cat.Text("console.first_run.picker_title", nil))
	switch {
	case errors.Is(err, platform.ErrNotApplicable):
		slog.Info("first run: folder picker not opened; treated as cancelled", "err", err)
	case err != nil:
		slog.Warn("first run: folder picker not opened; treated as cancelled", "err", err)
	}
	if dir == "" {
		slog.Info("first run: no folder chosen; checking only the agents")
		fmt.Println(cat.Text("console.first_run.agents_only", nil))
		return []string{}
	}
	// U11: the chosen folder's repositories through U3's filters and limit.
	slog.Info("first run: folder chosen", "path", dir)
	picked := pickedProjects(dir)
	logDecisions(picked)
	fmt.Println(cat.Text("console.first_run.found", map[string]string{"count": strconv.Itoa(len(picked.Repos))}))
	if picked.Repos == nil {
		return []string{}
	}
	return picked.Repos
}

// logDecisions writes each first-run decision, and the repositories left
// out by the limit, to the log.
func logDecisions(res firstrun.Result) {
	for _, d := range res.Decisions {
		slog.Info("first run: candidate", "source", d.Source, "rule", d.Rule, "path", d.Path)
	}
	if res.Over > 0 {
		slog.Warn("first run: repositories over the limit left out", "count", res.Over, "limit", firstrun.MaxRepos)
	}
}

// openReport opens the report in the default browser (U5, as J9's plan
// B); false when it could not.
func openReport(path string) bool {
	switch err := openReportFile(absPath(path)); {
	case errors.Is(err, platform.ErrNotApplicable):
		slog.Info("report not opened", "err", err)
		return false
	case err != nil:
		slog.Warn("report not opened", "err", err)
		return false
	}
	slog.Info("report opened", "path", path)
	return true
}

// priorReportsExist reports whether the reports directory already holds a
// report from an earlier run: report.html or any dated report-*.md. Used to
// tell a true first use (an empty folder) from a lost state (BV-93, B3).
func priorReportsExist(dir string) bool {
	if fileExists(filepath.Join(dir, report.HTMLFileName)) {
		return true
	}
	matches, _ := filepath.Glob(filepath.Join(dir, "report-*.md"))
	return len(matches) > 0
}

// pressEnter asks to press Enter and waits for it on a console (U5).
func pressEnter(cat catalog.Catalog) {
	fmt.Println(cat.Text("console.press_enter", nil))
	if isTerminal(os.Stdin) {
		_, _ = bufio.NewReader(os.Stdin).ReadString('\n')
	}
}
