package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSendFailedLogLine (BV-201 point 3): when a package cannot be built, a
// "failed" line is written to the sends log carrying the schema field path and
// no value, and it is not terminal, so a later good package of the same name is
// still sent.
func TestSendFailedLogLine(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, SendsLogName)
	pkg := "rosmarops-results-2026-11-08-0123abcd.locked.json"

	appendSendLog(logPath, time.Date(2026, 11, 8, 9, 0, 0, 0, time.UTC), pkg, 0, sendFailed,
		"findings.0.text.en.title,findings.0.text.es.title")

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	line := strings.TrimRight(string(raw), "\n")
	fields := strings.Fields(line)
	// <ts> <size> <host> <result> <filename> <detail>
	if len(fields) != 6 {
		t.Fatalf("want 6 fields, got %d: %q", len(fields), line)
	}
	if fields[3] != sendFailed {
		t.Errorf("result = %q, want %q", fields[3], sendFailed)
	}
	if fields[4] != pkg {
		t.Errorf("filename = %q, want %q", fields[4], pkg)
	}
	if fields[5] != "findings.0.text.en.title,findings.0.text.es.title" {
		t.Errorf("detail (field path) = %q", fields[5])
	}
	// No value ever: the detail is only dotted paths, never a message with spaces.
	if strings.Contains(fields[5], " ") {
		t.Errorf("detail carries a space (possible value leak): %q", fields[5])
	}

	// "failed" is not terminal: the package name is not marked sent/terminal, so
	// a later successful build of the same name is still attempted.
	if terminalResults[sendFailed] {
		t.Error("sendFailed must not be terminal")
	}
	if term := terminalFilenames(dir); term[pkg] {
		t.Errorf("a failed line must not mark %q terminal", pkg)
	}
}
