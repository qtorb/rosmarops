package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/report"
)

// BV-183: the reports directory shows only the three HTML pages; everything
// else is in data/, dated reports of older versions in data/archive/, and old
// sealed packages are pruned.

func bv183Write(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestRunLeavesOnlyHTMLInRoot: after a review, the reports root holds only the
// three HTML pages and the data/ directory.
func TestRunLeavesOnlyHTMLInRoot(t *testing.T) {
	stPath, reportDir := bv176Setup(t)
	sends := 0
	bv176Stubs(t, stPath, 201, &sends)

	if code := runNoArgs(); code != 0 {
		t.Fatalf("exit %d", code)
	}
	entries, err := os.ReadDir(reportDir)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		got = append(got, e.Name())
	}
	sort.Strings(got)
	want := []string{"data", "report.en.html", "report.es.html", "report.html"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("reports root = %v, want %v", got, want)
	}
	// The data directory holds the run's files.
	for _, f := range []string{"report.md", "report.json", report.V2FileName, "graph.v2.json", "index-history.json", "findings-memory.json"} {
		if _, err := os.Stat(filepath.Join(reportDir, "data", f)); err != nil {
			t.Errorf("data/%s missing: %v", f, err)
		}
	}
}

// TestMigrateReportsLayout: a directory shaped like v0.7.8 is tidied into data/
// and data/archive/ without deleting anything.
func TestMigrateReportsLayout(t *testing.T) {
	root := t.TempDir()
	// The three fixed HTML pages stay in the root.
	fixedHTML := []string{"report.html", "report.en.html", "report.es.html"}
	for _, f := range fixedHTML {
		bv183Write(t, filepath.Join(root, f))
	}
	// Fixed-name data files and a package in the root.
	fixedData := []string{
		"graph.v2.json", "index-history.json", "findings-memory.json",
		acceptedCopyName, ScheduledLogName, RunLogName, SendsLogName, report.V2FileName,
	}
	pkg := "rosmarops-results-2026-11-01-hostabcd.locked.json"
	for _, f := range append(append([]string{}, fixedData...), pkg) {
		bv183Write(t, filepath.Join(root, f))
	}
	// Dated reports and dated HTML copies of older runs.
	dated := []string{
		"report-2026-10-25__RED__3R-1Y.md",
		"report-2026-10-25__RED__3R-1Y.json",
		"report-2026-10-25__RED__3R-1Y.ai.md",
		"report-2026-10-25__RED__3R-1Y.en.html",
		"report-2026-10-25__RED__3R-1Y.es.html",
	}
	for _, f := range dated {
		bv183Write(t, filepath.Join(root, f))
	}
	before := countFiles(t, root)

	migrateReportsLayout(root)

	// The root now holds only the three HTML pages and the data dir.
	entries, _ := os.ReadDir(root)
	var rootNames []string
	for _, e := range entries {
		rootNames = append(rootNames, e.Name())
	}
	sort.Strings(rootNames)
	want := []string{"data", "report.en.html", "report.es.html", "report.html"}
	if strings.Join(rootNames, ",") != strings.Join(want, ",") {
		t.Errorf("root after migration = %v, want %v", rootNames, want)
	}
	data := filepath.Join(root, report.DataSubdir)
	for _, f := range append(append([]string{}, fixedData...), pkg) {
		if _, err := os.Stat(filepath.Join(data, f)); err != nil {
			t.Errorf("data/%s not moved: %v", f, err)
		}
	}
	for _, f := range dated {
		if _, err := os.Stat(filepath.Join(data, "archive", f)); err != nil {
			t.Errorf("data/archive/%s not moved: %v", f, err)
		}
	}
	// Nothing was deleted: the same number of files, just relocated.
	if after := countFiles(t, root); after != before {
		t.Errorf("file count changed: %d -> %d (migration must not delete)", before, after)
	}

	// Idempotent: a second run (a fresh layout) moves nothing and does not fail.
	migrateReportsLayout(root)
	if after := countFiles(t, root); after != before {
		t.Errorf("second migration changed the file count: %d", after)
	}
}

// TestPruneResultsPackages: only packages whose name is more than 30 days old
// AND whose last send result is terminal are removed.
func TestPruneResultsPackages(t *testing.T) {
	data := t.TempDir()
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -40).Format("2006-01-02") // > 30 days
	recent := now.AddDate(0, 0, -10).Format("2006-01-02")
	oldTerminal := "rosmarops-results-" + old + "-hostaaaa.locked.json"
	oldPending := "rosmarops-results-" + old + "-hostbbbb.locked.json"
	recentTerminal := "rosmarops-results-" + recent + "-hostcccc.locked.json"
	for _, f := range []string{oldTerminal, oldPending, recentTerminal} {
		bv183Write(t, filepath.Join(data, f))
	}
	// oldTerminal and recentTerminal are terminal; oldPending is not.
	terminal := map[string]bool{oldTerminal: true, recentTerminal: true}

	pruneResultsPackages(data, now, terminal)

	if _, err := os.Stat(filepath.Join(data, oldTerminal)); !os.IsNotExist(err) {
		t.Error("old terminal package was not pruned")
	}
	for _, keep := range []string{oldPending, recentTerminal} {
		if _, err := os.Stat(filepath.Join(data, keep)); err != nil {
			t.Errorf("%s was pruned but should be kept: %v", keep, err)
		}
	}
}

func countFiles(t *testing.T, root string) int {
	t.Helper()
	n := 0
	err := filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}
