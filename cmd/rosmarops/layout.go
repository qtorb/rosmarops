package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/report"
)

// BV-183: the reports directory shows only the three HTML pages; everything
// else lives in data/ (fixed names, overwritten) with the dated reports of
// older versions in data/archive/. This file holds the one-time migration from
// the pre-BV-183 layout and the prune of old sealed packages.

// fixedDataFiles are the fixed-name files that belong in data/. On the first
// run with this version they are moved there from the reports root.
var fixedDataFiles = []string{
	"graph.v2.json",
	"index-history.json",
	"findings-memory.json",
	acceptedCopyName,  // config.accepted.json
	ScheduledLogName,  // last-scheduled-run-log.txt
	RunLogName,        // last-run-log.txt
	SendsLogName,      // rosmarops-sends.log
	report.V2FileName, // report.v2.json
}

// migrateReportsLayout moves pre-BV-183 files out of the reports root: the
// fixed-name files to data/, the dated reports (report-<date>__… .md/.json/
// .ai.md) and the dated HTML copies to data/archive/, and the sealed packages
// to data/. It never deletes and never overwrites an existing destination.
// Idempotent (a second run finds nothing to move) and best effort: a failed
// rename is logged without any value and the run continues. The three fixed
// HTML pages (report.html, report.en.html, report.es.html) start with
// "report." (not "report-") and stay in the root.
func migrateReportsLayout(reportsDir string) {
	entries, err := os.ReadDir(reportsDir)
	if err != nil {
		return // nothing there yet, or unreadable: a fresh run writes into data/
	}
	data := report.DataDir(reportsDir)
	archive := filepath.Join(data, "archive")
	fixed := make(map[string]bool, len(fixedDataFiles))
	for _, n := range fixedDataFiles {
		fixed[n] = true
	}
	move := func(name, destDir string) {
		dst := filepath.Join(destDir, name)
		if _, err := os.Stat(dst); err == nil {
			return // never overwrite in the migration
		}
		if err := os.MkdirAll(destDir, 0o700); err != nil {
			slog.Warn("reports layout: create data dir", "err", err)
			return
		}
		if err := os.Rename(filepath.Join(reportsDir, name), dst); err != nil {
			slog.Warn("reports layout: move file", "err", err)
		}
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		switch {
		case fixed[name]:
			move(name, data)
		case strings.HasPrefix(name, "rosmarops-results-") && strings.HasSuffix(name, ".locked.json"):
			move(name, data)
		case strings.HasPrefix(name, "report-"):
			// Dated report-<date>__… .md/.json/.ai.md and dated HTML copies.
			move(name, archive)
		}
	}
}

// pruneResultsPackages removes sealed packages in dataDir whose name is more
// than 30 days old and whose last send result is terminal (in terminal).
// Nothing else is removed. Best effort: a failure is logged without values.
func pruneResultsPackages(dataDir string, now time.Time, terminal map[string]bool) {
	matches, _ := filepath.Glob(filepath.Join(dataDir, "rosmarops-results-*.locked.json"))
	cutoff := now.AddDate(0, 0, -30)
	for _, m := range matches {
		base := filepath.Base(m)
		if !terminal[base] {
			continue
		}
		d, ok := packageDate(base)
		if !ok || !d.Before(cutoff) {
			continue
		}
		if err := os.Remove(m); err != nil {
			slog.Warn("reports layout: prune package", "err", err)
		}
	}
}

// packageDate reads the date in rosmarops-results-<YYYY-MM-DD>-<host>.locked.json.
func packageDate(base string) (time.Time, bool) {
	rest := strings.TrimPrefix(base, "rosmarops-results-")
	if len(rest) < len("2006-01-02") {
		return time.Time{}, false
	}
	d, err := time.Parse("2006-01-02", rest[:len("2006-01-02")])
	if err != nil {
		return time.Time{}, false
	}
	return d, true
}
