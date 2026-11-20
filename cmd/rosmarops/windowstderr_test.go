package main

import (
	"strings"
	"testing"
)

// Without the self-test hook nothing changes: no writer (the window's error
// output stays discarded) and nothing is logged.
func TestSelfTestStderr_WithoutHook(t *testing.T) {
	t.Setenv(findingsSelfTestEnv, "")
	log := captureLog(t)
	s := selfTestStderr(findingsSelfTestEnv)
	if s != nil {
		t.Fatal("a writer for the error output without the self-test hook")
	}
	logSelfTestStderr(s, "findings")
	if log.Len() != 0 {
		t.Errorf("something logged without the hook: %q", log.String())
	}
}

// With the hook, the error output is logged when the window ends: at most
// 4 KB, and without the user's folders or any environment value.
func TestSelfTestStderr_WithHook(t *testing.T) {
	t.Setenv(findingsSelfTestEnv, "1")
	t.Setenv("USERPROFILE", `C:\Users\Persona Ejemplo`)
	t.Setenv("COMPUTERNAME", "MAQUINA-DE-PRUEBA")
	log := captureLog(t)
	s := selfTestStderr(findingsSelfTestEnv)
	if s == nil {
		t.Fatal("no writer under the self-test hook")
	}
	msg := "Exception at c:\\users\\persona ejemplo\\AppData\\x.ps1 on MAQUINA-DE-PRUEBA and C:/Users/Persona Ejemplo/y\n"
	if n, err := s.Write([]byte(msg)); err != nil || n != len(msg) {
		t.Fatalf("write: %d %v", n, err)
	}
	logSelfTestStderr(s, "findings")
	got := log.String()
	if !strings.Contains(got, "window self-test: error output") || !strings.Contains(got, "window=findings") {
		t.Fatalf("not logged: %q", got)
	}
	for _, leak := range []string{"Persona", "persona", "MAQUINA"} {
		if strings.Contains(got, leak) {
			t.Errorf("the log carries %q from this computer: %q", leak, got)
		}
	}
	if !strings.Contains(got, "%USERPROFILE%") || !strings.Contains(got, "%COMPUTERNAME%") {
		t.Errorf("the values are not named: %q", got)
	}

	// The first 4 KB only, and the writer never fails.
	big := selfTestStderr(findingsSelfTestEnv)
	chunk := []byte(strings.Repeat("e", 3000))
	for i := 0; i < 3; i++ {
		if n, err := big.Write(chunk); err != nil || n != len(chunk) {
			t.Fatalf("write %d: %d %v", i, n, err)
		}
	}
	if len(big.buf) != selfTestStderrLimit || !big.cut {
		t.Errorf("kept %d bytes (cut %v), want %d and cut", len(big.buf), big.cut, selfTestStderrLimit)
	}
}

// An empty error output logs nothing, even with the hook.
func TestSelfTestStderr_Empty(t *testing.T) {
	t.Setenv(findingsSelfTestEnv, "1")
	log := captureLog(t)
	logSelfTestStderr(selfTestStderr(findingsSelfTestEnv), "findings")
	if log.Len() != 0 {
		t.Errorf("logged an empty error output: %q", log.String())
	}
}
