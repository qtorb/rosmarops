package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/report"
)

// moveOldReports moves the reports from ~/.rosmarops/reports to ~/RosmarOps
// on Linux, once, before anything is written to the new default folder, and
// says so on out. Nothing moves when the person's configuration sets
// report_directory: the folder in use is then theirs. When both folders exist
// nothing moves and ~/RosmarOps is the one in use.
func moveOldReports(out io.Writer, cat catalog.Catalog) {
	moveOldReportsFor(runtime.GOOS, out, cat)
}

func moveOldReportsFor(goos string, out io.Writer, cat catalog.Catalog) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	if cfgPath, err := resolveCfgPath(); err == nil && configuredReportDir(cfgPath) != "" {
		return
	}
	moved, both, err := report.MoveOldReports(goos, home)
	switch {
	case err != nil:
		slog.Warn("reports: could not move the old reports folder", "err", err)
	case both:
		slog.Warn("reports: both ~/RosmarOps and ~/.rosmarops/reports exist; nothing moved")
	case moved:
		fmt.Fprintln(out, cat.Text("console.reports_moved", nil))
	}
}

// configuredReportDir is the report_directory of the configuration at path,
// read without validating the rest: "" when it is not set or the file cannot
// be read.
func configuredReportDir(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var c struct {
		ReportDirectory string `json:"report_directory"`
	}
	if json.Unmarshal(raw, &c) != nil {
		return ""
	}
	return c.ReportDirectory
}
