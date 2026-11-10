package trust_test

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestNoNetwork_ProblemNotebook_NoSendWithoutConsent is the BV-210 §7 network
// guarantee, as an extension of the runtime no-network checks: with a problem
// recorded in the local notebook but no «Send» and no «send without asking»
// checkbox, the program opens no inet socket. A scheduled review (unattended,
// nobody present) never asks and never sends — it leaves the problems for next
// time. ROSMAROPS_DEMO_PROBLEM=a seeds one problem without touching any real
// config. The complementary guarantee — with consent, only app.rosmarops.com:443
// and only /api/v1/problems — is proved by the host-pin (the one spacenet client
// refuses every other host, TestProblemSendClientRefusesOtherHosts) and by the
// send path test (POST /api/v1/problems, no credential or cookie). Linux + strace.
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

	traceDir := t.TempDir()
	traces := map[string]string{}
	for _, step := range []string{"run", "unattended"} {
		out := filepath.Join(traceDir, step+".strace")
		args := []string{"run"}
		if step == "unattended" {
			args = []string{"run", "--unattended"}
		}
		argv := append([]string{strace, "-f", "-qq", "-Y", "-e", "trace=connect,sendto,sendmsg", "-o", out}, append([]string{s.binary}, args...)...)
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = s.work
		// Seed one problem for this run; the checkbox stays off and nobody is
		// present, so nothing is sent.
		cmd.Env = append(append([]string{}, s.env...), "ROSMAROPS_DEMO_PROBLEM=a")
		_, _ = cmd.CombinedOutput() // a yellow/red run exits non-zero; the trace is what matters
		traces[step] = out
	}
	assertNoInet(t, traces)
}
