//go:build windows

package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/report"
)

func synthCards(n int) []report.FindingCard {
	cards := make([]report.FindingCard, n)
	for i := range cards {
		cards[i] = report.FindingCard{
			ID:          "finding-" + strings.Repeat("x", 8) + "-" + strconv.Itoa(i),
			Severity:    "yellow",
			Title:       "A synthetic finding number " + strconv.Itoa(i),
			WhatItMeans: "What it means, repeated text to make the card big " + strings.Repeat("y", 40),
			WhenApplies: "When it applies, more text " + strings.Repeat("z", 30),
			New:         i%2 == 0,
			Agent:       "claude-code",
			Group:       report.CardGroupRest,
		}
	}
	return cards
}

// TestBV211bCommandLineFixedAndContentComplete is BV-211b §1/§3: with 1 or 500
// synthetic findings the dismiss window's command line is the same size and
// well under 2048 characters (the script and the data are no longer on it), and
// the data the script receives on standard input decodes byte for byte to the
// full config.
func TestBV211bCommandLineFixedAndContentComplete(t *testing.T) {
	cat := enCat(t)

	cfg1 := dismissWindowConfigFrom(cat, synthCards(1))
	cfg500 := dismissWindowConfigFrom(cat, synthCards(500))

	// The command line does not depend on the data: the script goes to a file
	// whose path is a fixed shape, so a 1-finding and a 500-finding run build
	// the identical, tiny command line.
	args := windowArgs(`C:\Users\x\.rosmarops\reports\data\rosmarops-window-123456.ps1`)
	cmdline := "powershell.exe " + strings.Join(args, " ")
	if len(cmdline) >= 2048 {
		t.Fatalf("command line should be well under 2048, got %d: %q", len(cmdline), cmdline)
	}
	for _, bad := range []string{"finding-", "synthetic finding"} {
		if strings.Contains(cmdline, bad) {
			t.Fatalf("the command line must not carry finding data: %q", cmdline)
		}
	}

	// The data the window gets on stdin is the complete config, byte for byte.
	in500 := dismissWindowInput(cfg500)
	raw, err := base64.StdEncoding.DecodeString(in500)
	if err != nil {
		t.Fatalf("stdin payload is not valid base64: %v", err)
	}
	want, err := json.Marshal(cfg500)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(want) {
		t.Fatal("the stdin payload does not decode to the full config byte for byte")
	}
	// The payload grows with the data (it is on stdin, not the command line).
	if dismissWindowInput(cfg1) == in500 {
		t.Fatal("expected the 500-finding payload to differ from the 1-finding one")
	}
}

// TestBV211bScriptFileInDataAndRemoved is BV-211b §1/§3: the window script is
// written to a .ps1 inside <reports>/data and removed when the window closes.
func TestBV211bScriptFileInDataAndRemoved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	path, cleanup, ok := windowScriptFile(dismissWindowScript)
	if !ok {
		t.Fatal("windowScriptFile should succeed with a writable home")
	}
	dir, err := report.DefaultReportDir()
	if err != nil {
		t.Fatal(err)
	}
	wantDir := report.DataDir(dir)
	if filepath.Dir(path) != wantDir {
		t.Fatalf("script should be in <reports>/data (%s), got %s", wantDir, path)
	}
	if !strings.HasSuffix(path, ".ps1") {
		t.Fatalf("script file should be a .ps1: %s", path)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("script file should exist: %v", err)
	}
	if string(got) != dismissWindowScript {
		t.Fatal("script file content should be the full window script")
	}
	cleanup()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("cleanup should remove the script file; stat err = %v", err)
	}
}
