package trust_test

// Trust test (CLAUDE.md invariant 2): running the real binary — init,
// baseline, run — writes nothing outside <HOME>/.rosmarops/, including writes
// made by the child processes it starts (npm, pnpm, git, code) when present.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/report"
)

func TestNoWritesOutOfScope(t *testing.T) {
	s := newSandbox(t)
	allowed := filepath.Join(s.home, ".rosmarops")

	before := snapshot(t, s.root)
	s.runAll(t, nil)
	after := snapshot(t, s.root)

	var culprits []string
	for _, change := range changedPaths(before, after) {
		_, path, _ := strings.Cut(change, ": ")
		if !pathUnder(path, allowed) {
			culprits = append(culprits, change)
		}
	}
	if len(culprits) > 0 {
		t.Fatalf("%d path(s) changed outside %s:\n%s", len(culprits), allowed, strings.Join(culprits, "\n"))
	}
}

// CTO-49 v2 (a): once installed, a review never writes config.json. After the
// install (init) and the setup in runAll, a review from the console, a
// scheduled review and a double click leave ~/.rosmarops/config.json as it was.
func TestReviewDoesNotWriteConfig(t *testing.T) {
	s := newSandbox(t)
	s.runAll(t, nil)

	before := snapshot(t, s.root)
	for _, args := range [][]string{{"run"}, {"run", "--unattended"}, {}} {
		cmd := exec.Command(s.binary, args...)
		cmd.Dir = s.work
		cmd.Env = s.env
		out, err := cmd.CombinedOutput()
		var exitErr *exec.ExitError
		if err != nil && !errors.As(err, &exitErr) {
			t.Fatalf("rosmarops %v: %v\n%s", args, err, out)
		}
	}
	after := snapshot(t, s.root)
	for _, change := range changedPaths(before, after) {
		if _, path, _ := strings.Cut(change, ": "); filepath.Base(path) == "config.json" {
			t.Errorf("a review wrote a configuration: %s", change)
		}
	}
}

// CTO-49 v2 (b): dismissing a finding with `rosmarops findings` writes the
// configuration only at ~/.rosmarops/config.json. The other paths it changes
// are the ones writeDismissed re-points (state.json and the accepted copy in
// the reports' data/), all inside ~/.rosmarops.
func TestFindingsDismissWritesOnlyHomeConfig(t *testing.T) {
	s := newSandbox(t)
	s.runAll(t, nil)
	rosmarops := filepath.Join(s.home, ".rosmarops")
	allowed := map[string]bool{
		s.configPath():                         true,
		filepath.Join(rosmarops, "state.json"): true,
		filepath.Join(rosmarops, "reports", report.DataSubdir, "config.accepted.json"): true,
	}

	before := snapshot(t, s.root)
	out := runFindingsInPTY(t, s, "1\n")
	after := snapshot(t, s.root)

	changes := changedPaths(before, after)
	if len(changes) == 0 {
		t.Fatalf("dismissing finding 1 changed nothing; output:\n%s", out)
	}
	wroteConfig := false
	for _, change := range changes {
		_, path, _ := strings.Cut(change, ": ")
		if !allowed[path] && !strings.HasPrefix(filepath.Base(path), ".rosmarops-config-") {
			t.Errorf("dismissing wrote %s", change)
		}
		if path == s.configPath() {
			wroteConfig = true
		}
	}
	if !wroteConfig {
		t.Fatalf("~/.rosmarops/config.json not written; output:\n%s", out)
	}
	raw, err := os.ReadFile(s.configPath())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"dismissed"`) {
		t.Errorf("config.json has no dismissed list:\n%s", raw)
	}
}

// CTO-49 v2 (c): with a ./rosmarops.config.json in the working directory, the
// configuration in use is outside ~/.rosmarops: `rosmarops findings` writes no
// file at all, even when the person answers, and says why
// (findings.config_outside).
func TestFindingsConfigOutsideWritesNothing(t *testing.T) {
	s := newSandbox(t)
	s.runAll(t, nil)
	// The same bytes as the accepted configuration, so the integrity check
	// passes and the only difference is where the configuration comes from.
	raw, err := os.ReadFile(s.configPath())
	if err != nil {
		t.Fatal(err)
	}
	local := filepath.Join(s.work, "rosmarops.config.json")
	mustWrite(t, local, string(raw))

	before := snapshot(t, s.root)
	out := runFindingsInPTY(t, s, "1\n")
	after := snapshot(t, s.root)

	if changes := changedPaths(before, after); len(changes) > 0 {
		t.Errorf("findings with a configuration outside ~/.rosmarops wrote:\n%s", strings.Join(changes, "\n"))
	}
	work, err := filepath.EvalSymlinks(s.work)
	if err != nil {
		t.Fatal(err)
	}
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	want := cat.Text("findings.config_outside", map[string]string{"path": filepath.Join(work, "rosmarops.config.json")})
	if !strings.Contains(out, want) {
		t.Errorf("output lacks findings.config_outside %q:\n%s", want, out)
	}
}

// runFindingsInPTY runs `rosmarops findings --lang en` with a terminal as its
// standard input and output, types input, and returns what it printed.
func runFindingsInPTY(t *testing.T, s *sandbox, input string) string {
	t.Helper()
	master, slave, err := openPTY()
	if err != nil {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	defer master.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.binary, "findings", "--lang", "en")
	cmd.Dir = s.work
	cmd.Env = s.env
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	if err := cmd.Start(); err != nil {
		slave.Close()
		t.Fatal(err)
	}
	slave.Close() // the child keeps its own copy
	if _, err := master.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		_, _ = io.Copy(&buf, master) // ends with EIO once the child exits
		close(done)
	}()
	if err := cmd.Wait(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("rosmarops findings: %v\n%s", err, buf.String())
		}
		t.Logf("rosmarops findings exit code %d", exitErr.ExitCode())
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
	}
	return buf.String()
}
