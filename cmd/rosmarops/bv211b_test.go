package main

import (
	"strings"
	"testing"
)

// TestBV211bWriteWindowScript checks the platform-neutral launcher writes the
// script followed by the sentinel line, so the bootstrap runs it and the
// window's own data follows on the same standard input (CTO-34). Neutral, so it
// runs on every system.
func TestBV211bWriteWindowScript(t *testing.T) {
	var b strings.Builder
	if err := writeWindowScript(&b, "SCRIPT-BODY"); err != nil {
		t.Fatalf("writeWindowScript: %v", err)
	}
	want := "SCRIPT-BODY\n" + windowEndOfScript + "\n"
	if b.String() != want {
		t.Fatalf("writeWindowScript wrote %q, want %q", b.String(), want)
	}
}

// TestBV211bWindowArgvExact is CTO-34 §3(a): every window's command line is
// exactly the fixed bootstrap invocation.
func TestBV211bWindowArgvExact(t *testing.T) {
	ps := `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`
	got := windowArgv(ps)
	want := []string{ps, "-NoProfile", "-NonInteractive", "-STA", "-Command", windowBootstrap}
	if len(got) != len(want) {
		t.Fatalf("argv has %d parts, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestBV211bCommandLineSmall is CTO-34 §3(b)/(c): the command line, joined as
// Windows would with EscapeArg, is well under 8000 characters and the same for
// 1 or 500 findings — the data is never on it, only on standard input — and no
// argument uses -ExecutionPolicy, -EncodedCommand, -enc, -File or a .ps1 file.
func TestBV211bCommandLineSmall(t *testing.T) {
	ps := `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`
	argv := windowArgv(ps)
	// The argv carries no findings, so it is byte-identical however many a
	// window would show; measuring it once covers 1 and 500.
	line := joinEscapeArg(argv)
	t.Logf("CTO-34 §3(b): the window command line is %d characters, the same for 1 or 500 findings (the data is never on it)", len(line))
	if len(line) >= 8000 {
		t.Fatalf("command line should be well under 8000, got %d", len(line))
	}
	for _, a := range argv {
		for _, bad := range []string{"-ExecutionPolicy", "-EncodedCommand", "-enc", "-File", ".ps1"} {
			if strings.Contains(a, bad) {
				t.Fatalf("argument %q must not contain %q", a, bad)
			}
		}
	}
}

// TestBV211bScriptsAsciiNoSentinel is CTO-34 §3(d): the four window scripts are
// ASCII only and none contains the end-of-script sentinel.
func TestBV211bScriptsAsciiNoSentinel(t *testing.T) {
	scripts := map[string]string{
		"dismissWindowScript": dismissWindowScript,
		"connectWindowScript": connectWindowScript,
		"problemWindowScript": problemWindowScript,
		"problemBoxScript":    problemBoxScript,
	}
	for name, sc := range scripts {
		for i, r := range sc {
			if r > 127 {
				t.Errorf("%s has a non-ASCII rune %q at byte %d", name, r, i)
				break
			}
		}
		if strings.Contains(sc, windowEndOfScript) {
			t.Errorf("%s contains the end-of-script sentinel", name)
		}
	}
}

// joinEscapeArg joins argv the way Windows builds a command line, escaping each
// argument as syscall.EscapeArg does (reimplemented here so the test runs on
// every OS, not only Windows).
func joinEscapeArg(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = escapeArgForTest(a)
	}
	return strings.Join(parts, " ")
}

// escapeArgForTest mirrors Go's syscall.EscapeArg.
func escapeArgForTest(s string) string {
	if s == "" {
		return `""`
	}
	n := len(s)
	hasSpace := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"', '\\':
			n++
		case ' ', '\t':
			hasSpace = true
		}
	}
	if hasSpace {
		n += 2
	}
	if n == len(s) {
		return s
	}
	qs := make([]byte, n)
	j := 0
	if hasSpace {
		qs[j] = '"'
		j++
	}
	slashes := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		default:
			slashes = 0
			qs[j] = s[i]
		case '\\':
			slashes++
			qs[j] = s[i]
		case '"':
			for ; slashes > 0; slashes-- {
				qs[j] = '\\'
				j++
			}
			qs[j] = '\\'
			j++
			qs[j] = s[i]
		}
		j++
	}
	if hasSpace {
		for ; slashes > 0; slashes-- {
			qs[j] = '\\'
			j++
		}
		qs[j] = '"'
		j++
	}
	return string(qs[:j])
}
