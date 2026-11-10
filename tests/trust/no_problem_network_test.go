package trust_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/problems"
	"github.com/qtorb/rosmarops/internal/report"
)

// TestNoNetwork_ProblemNotebook_NoSendWithoutConsent is the BV-210 §7 / CTO-32
// C2(c) network guarantee: with a problem in the local notebook but no «Send»
// and no «send without asking» checkbox, the program opens no inet socket. A
// scheduled review (unattended, nobody present) never asks and never sends — it
// leaves the problems for next time. The problem is seeded directly in the
// notebook (not via the demo variable, which the release binary ignores), and
// the test fails if the notebook has no unsent line after a step, so a broken
// seed cannot let it pass in a vacuum. The complementary guarantee — with
// consent, only app.rosmarops.com:443 and only /api/v1/problems — is proved by
// TestNoNetwork_ProblemSend_OnlyFixedHost, the spacenet path test, and the
// host-pin. Linux + strace.
func TestNoNetwork_ProblemNotebook_NoSendWithoutConsent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("runtime network check is implemented for Linux")
	}
	strace, err := exec.LookPath("strace")
	if err != nil {
		t.Skip("strace not available: runtime network check not executed")
	}
	s := newSandbox(t)
	// A full first run creates the config and state (permission and checkbox
	// both false/absent by default).
	s.runAll(t, nil)

	dataDir := report.DataDir(filepath.Join(s.home, ".rosmarops", "reports"))
	seedProblem := func() {
		if _, err := problems.New(dataDir).Record(problems.Entry{
			Code: problems.StepFailed, Step: "mcp", Day: problems.Day(time.Now()),
		}); err != nil {
			t.Fatalf("seed problem: %v", err)
		}
	}

	traceDir := t.TempDir()
	traces := map[string]string{}
	for _, step := range []string{"run", "unattended"} {
		seedProblem()
		out := filepath.Join(traceDir, step+".strace")
		args := []string{"run"}
		if step == "unattended" {
			args = []string{"run", "--unattended"}
		}
		argv := append([]string{strace, "-f", "-qq", "-Y", "-e", "trace=connect,sendto,sendmsg", "-o", out}, append([]string{s.binary}, args...)...)
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = s.work
		cmd.Env = s.env
		_, _ = cmd.CombinedOutput() // a yellow/red run exits non-zero; the trace is what matters
		traces[step] = out

		// The seed must have been a real unsent problem (CTO-32 C2c): no send
		// happened, so at least one unsent line remains.
		unsent, err := problems.New(dataDir).Unsent(problems.MaxPerSend)
		if err != nil {
			t.Fatalf("read notebook after %s: %v", step, err)
		}
		if len(unsent) == 0 {
			t.Fatalf("after %s: no unsent problem in the notebook; the seed is not exercising the path", step)
		}
	}
	assertNoInet(t, traces)
}

// problemReleaseIgnoresDemoVar is CTO-32 C5: the release binary (built without
// the demoproblem tag, as newSandbox builds it) does not read
// ROSMAROPS_DEMO_PROBLEM, so setting it seeds no problem.
func TestProblemReleaseIgnoresDemoVar(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sandboxed black-box run is implemented for Unix-like hosts")
	}
	s := newSandbox(t)
	s.runAll(t, nil)

	dataDir := report.DataDir(filepath.Join(s.home, ".rosmarops", "reports"))
	// Clear any notebook the first run may have made.
	_ = os.Remove(filepath.Join(dataDir, problems.NotebookName))

	cmd := exec.Command(s.binary, "run", "--unattended")
	cmd.Dir = s.work
	cmd.Env = append(append([]string{}, s.env...), "ROSMAROPS_DEMO_PROBLEM=a")
	_, _ = cmd.CombinedOutput()

	// The release binary ignored the variable: no demo problem was recorded.
	unsent, err := problems.New(dataDir).Unsent(problems.MaxPerSend)
	if err != nil {
		t.Fatalf("read notebook: %v", err)
	}
	for _, e := range unsent {
		if e.Code == problems.StepFailed && e.Step == "mcp" {
			t.Fatalf("release binary honoured ROSMAROPS_DEMO_PROBLEM; it must not")
		}
	}
}
