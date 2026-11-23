package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qtorb/rosmarops/internal/report"
)

// seedEarlierReview leaves the report of an earlier review in the reports
// folder, so the next review is not the installation's first (BV-286): the
// first review shows neither the connect offer nor the «No me afecta» window.
func seedEarlierReview(t *testing.T) {
	t.Helper()
	dir, err := report.DefaultReportDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, report.HTMLFileName), []byte("<html></html>\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
