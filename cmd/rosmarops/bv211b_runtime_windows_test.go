//go:build windows

package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/winpath"
)

// BV-211b / CTO-34 §4: a runtime proof, on the verify-windows job (Windows
// PowerShell 5.1) and with no graphical interface, that windowBootstrap really
// reads a script from standard input up to the sentinel, runs it, and leaves
// the window's own data on the same standard input for the script to read.
//
// It launches powershell.exe with exactly windowCmdArgs() — the same fixed,
// small command line every window uses — and feeds the script through
// writeWindowScript, so it exercises the production launcher end to end. No
// Add-Type, no WinForms: safe to run on a headless runner, and its output is
// pasted in the PR.

// runBootstrap launches the real bootstrap, sends `script` (followed by the
// sentinel) and then the `data` lines on the same standard input, and returns
// the child's standard output and any run error.
func runBootstrap(t *testing.T, script string, data ...string) (string, error) {
	t.Helper()
	ps := winpath.PowerShellExe()
	if ps == "" {
		ps = "powershell"
	}
	cmd := exec.Command(ps, windowCmdArgs()...)
	cmd.Env = winpath.ChildEnv(os.Environ())
	var in strings.Builder
	if err := writeWindowScript(&in, script); err != nil {
		t.Fatalf("writeWindowScript: %v", err)
	}
	for _, d := range data {
		in.WriteString(d)
		in.WriteString("\n")
	}
	cmd.Stdin = strings.NewReader(in.String())
	out, err := cmd.Output()
	return string(out), err
}

// splitLines returns the non-empty CRLF/LF lines of s.
func splitLines(s string) []string {
	var lines []string
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimRight(l, "\r")
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// TestBV211bBootstrapReturnsWholeDataLine is §4 first case: a short script that
// writes a mark, reads the next standard-input line and returns it. The data
// line (the stand-in for a window's base64 config) must come back whole and
// nothing else must follow it.
func TestBV211bBootstrapReturnsWholeDataLine(t *testing.T) {
	const mark = "BV211B-MARK"
	// A representative config line: long, base64-shaped, one line, no spaces.
	dataLine := strings.Repeat("QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVowMTIzNDU2Nzg5", 120)
	script := "[Console]::Out.WriteLine('" + mark + "')\n" +
		"$d = [Console]::In.ReadLine()\n" +
		"[Console]::Out.WriteLine($d)"
	out, err := runBootstrap(t, script, dataLine)
	if err != nil {
		t.Fatalf("bootstrap failed: %v (output %q)", err, out)
	}
	lines := splitLines(out)
	if len(lines) != 2 {
		t.Fatalf("stdout should be exactly the mark and the data line, got %d lines: %q", len(lines), lines)
	}
	if lines[0] != mark {
		t.Fatalf("first line = %q, want the mark %q (the script did not run before reading)", lines[0], mark)
	}
	if lines[1] != dataLine {
		t.Fatalf("the data line did not come back whole: got %d chars, want %d", len(lines[1]), len(dataLine))
	}
	t.Logf("§4 first case OK: mark then a %d-char data line returned whole, nothing else", len(dataLine))
}

// TestBV211bBootstrapStartsHugeScript is §4 second case: a script of more than
// 40,000 characters (padded with ASCII comments) must start — proof that the
// size that broke the command line (CreateProcess «The filename or extension is
// too long») is no obstacle over standard input.
func TestBV211bBootstrapStartsHugeScript(t *testing.T) {
	const mark = "BV211B-BIGMARK"
	var b strings.Builder
	// Each comment line is 80 ASCII chars; none is the sentinel.
	for b.Len() < 40001 {
		b.WriteString("# ")
		b.WriteString(strings.Repeat("x", 76))
		b.WriteString("\n")
	}
	b.WriteString("[Console]::Out.WriteLine('" + mark + "')")
	script := b.String()
	if len(script) <= 40000 {
		t.Fatalf("test bug: script is %d chars, want > 40000", len(script))
	}
	out, err := runBootstrap(t, script)
	if err != nil {
		t.Fatalf("bootstrap failed to start a %d-char script: %v (output %q)", len(script), err, out)
	}
	if !strings.Contains(out, mark) {
		t.Fatalf("the %d-char script did not run: mark %q absent from output %q", len(script), mark, out)
	}
	t.Logf("§4 second case OK: a %d-char script started and ran over stdin", len(script))
}
