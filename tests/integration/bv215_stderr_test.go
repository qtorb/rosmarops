package integration_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDoubleClickNoSlogOnStderr (BV-215): the first double click writes no
// slog line to the console — before the fix, on macOS, systemLang's «language:
// system display language … not applicable» line was printed to stderr because
// it ran before the log was pointed at the run log. The line belongs in the
// log: on macOS the test also checks it is there.
func TestDoubleClickNoSlogOnStderr(t *testing.T) {
	bin, root, home, env := firstRunEnv(t)
	cmd := exec.Command(bin)
	cmd.Dir, cmd.Env = filepath.Join(root, "work"), env
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("double click: %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if s := stderr.String(); strings.Contains(s, "level=") || strings.Contains(s, "msg=") || strings.Contains(s, " INFO ") {
		t.Errorf("the first double click wrote slog to the console (stderr):\n%s", s)
	}
	if runtime.GOOS == "darwin" {
		log, _ := os.ReadFile(filepath.Join(home, ".rosmarops", "reports", "data", "last-run-log.txt"))
		if !strings.Contains(string(log), "language: system display language") {
			t.Errorf("the display-language line is not in the run log:\n%s", log)
		}
	}
}
