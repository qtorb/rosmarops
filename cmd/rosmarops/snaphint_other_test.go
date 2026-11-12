//go:build !linux

package main

import (
	"bytes"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// BV-219 B4: on Windows and macOS the snap hint is never shown.
func TestReportSnapHintNeverOffLinux(t *testing.T) {
	if browserIsSnap() {
		t.Fatal("browserIsSnap is true off Linux")
	}
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	printReportOpened(&b, cat, `C:\Users\u\.rosmarops\reports\report.html`)
	if want := cat.Text("console.report_opened", nil) + "\n"; b.String() != want {
		t.Errorf("got %q, want %q", b.String(), want)
	}
}
