package trust_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/state"
	"github.com/qtorb/rosmarops/internal/usage"
)

// Usage statistics open no connection unless the state records
// that the person turned them on. With usage off, and with no answer at all,
// a review (interactive or scheduled) opens no inet socket; with usage on, the
// only connection the program asks for is app.rosmarops.com:443, through a
// local proxy that only records (it never reaches the internet). Linux +
// strace, like the problem reports' tests.

// publishedBuild builds the program as a published version: only those ask
// about usage statistics and send them (a build without -X main.version says
// 0.1.0-dev and never does), so the usage tests build one.
var publishedBuild = []string{"-ldflags", "-X main.version=9.9.9"}

// usageHome lays out a HOME with a config and a state whose usage answer is u
// (nil: no answer).
func usageHome(t *testing.T, u *state.Usage) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".rosmarops")
	if err := os.MkdirAll(filepath.Join(dir, "reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteConfig(filepath.Join(dir, "config.json"), config.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	st, err := state.DefaultState("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	st.Usage = u
	if err := state.SaveState(filepath.Join(dir, "state.json"), st); err != nil {
		t.Fatal(err)
	}
	return home
}

func usageAnswer(on bool) *state.Usage {
	return &state.Usage{Enabled: on, DecidedAt: "2026-11-18T10:00:00Z", TextVersion: usage.TextVersion, InstallID: "3b2c1d4e-5f60-4a7b-8c9d-0e1f2a3b4c5d"}
}

func TestNoNetwork_Usage_NoSendWithoutConsent(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("runtime network check is implemented for Linux")
	}
	strace, err := exec.LookPath("strace")
	if err != nil {
		t.Skip("strace not available: runtime network check not executed")
	}
	bin := buildBin(t, "./cmd/rosmarops", publishedBuild)
	traces := map[string]string{}
	for name, u := range map[string]*state.Usage{"undecided": nil, "off": usageAnswer(false)} {
		for _, args := range [][]string{{"run"}, {"run", "--unattended"}} {
			home := usageHome(t, u)
			out := filepath.Join(t.TempDir(), "run.strace")
			argv := append([]string{strace, "-f", "-qq", "-Y", "-e", "trace=connect,sendto,sendmsg", "-o", out, bin}, args...)
			cmd := exec.Command(argv[0], argv[1:]...)
			cmd.Dir = home
			cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home)
			_, _ = cmd.CombinedOutput()
			traces[name+" "+strings.Join(args, " ")] = out
			if _, err := os.Stat(filepath.Join(report.DataDir(filepath.Join(report.DefaultReportDirIn(home))), usage.LastFileName)); err == nil {
				t.Errorf("%s %v: a usage package was written", name, args)
			}
		}
	}
	assertNoInet(t, traces)
}

func TestNoNetwork_UsageSend_OnlyFixedHost(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the proxy+strace network check is implemented for Linux")
	}
	strace, err := exec.LookPath("strace")
	if err != nil {
		t.Skip("strace not available")
	}
	recorder := buildBin(t, "./tests/trust/testdata/proxyrecorder", nil)
	bin := buildBin(t, "./cmd/rosmarops", publishedBuild)
	home := usageHome(t, usageAnswer(true))

	connects, inet := runBehindRecordingProxy(t, strace, recorder, bin, home, "run", "--unattended")
	if len(connects) != 1 || connects[0] != "app.rosmarops.com:443" {
		t.Errorf("CONNECT targets = %v, want exactly [app.rosmarops.com:443]", connects)
	}
	for _, target := range inet {
		if !strings.HasPrefix(target, "127.0.0.1:") && !strings.HasPrefix(target, "[::1]:") {
			t.Errorf("an inet connection went to %q, not the local proxy", target)
		}
	}
	// The package tried is kept as it was, and it is a valid usage.v1.
	raw, err := os.ReadFile(filepath.Join(report.DataDir(filepath.Join(report.DefaultReportDirIn(home))), usage.LastFileName))
	if err != nil {
		t.Fatalf("no %s: %v", usage.LastFileName, err)
	}
	var p usage.Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	if _, err := usage.Marshal(p); err != nil || p.InstallID != usageAnswer(true).InstallID {
		t.Errorf("last package %s: %v", raw, err)
	}
	log, _ := os.ReadFile(filepath.Join(report.DataDir(filepath.Join(report.DefaultReportDirIn(home))), "rosmarops-sends.log"))
	if !strings.Contains(string(log), usage.SendsLabel) {
		t.Errorf("sends log has no %s line:\n%s", usage.SendsLabel, log)
	}
}

// A usage send that fails (the proxy at 127.0.0.1:1 refuses the
// connection, as a network with no way out would) leaves the review the same: the same report status
// and the same exit code as a review with usage off.
func TestUsageSendFailureChangesNothing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sandboxed black-box run is implemented for Unix-like hosts")
	}
	bin := buildBin(t, "./cmd/rosmarops", publishedBuild)
	result := func(home string) (string, int) {
		code := 0
		cmd := exec.Command(bin, "run", "--unattended")
		cmd.Dir = home
		cmd.Env = append(os.Environ(), "HOME="+home, "USERPROFILE="+home, "HTTPS_PROXY=http://127.0.0.1:1", "HTTP_PROXY=http://127.0.0.1:1")
		if err := cmd.Run(); err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			}
		}
		raw, err := os.ReadFile(filepath.Join(report.DataDir(filepath.Join(report.DefaultReportDirIn(home))), report.V2FileName))
		if err != nil {
			t.Fatalf("no %s: %v", report.V2FileName, err)
		}
		var v struct {
			Status string `json:"status"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			t.Fatal(err)
		}
		return v.Status, code
	}
	onStatus, onCode := result(usageHome(t, usageAnswer(true)))
	offStatus, offCode := result(usageHome(t, usageAnswer(false)))
	if onStatus != offStatus || onCode != offCode {
		t.Errorf("usage on with a failed send: status %s, exit %d; usage off: status %s, exit %d", onStatus, onCode, offStatus, offCode)
	}
}

// The company's scheduled review (the no-network company runtime test) runs
// with a state that has no usage answer: after two scheduled reviews the
// state still has none and no usage summary was written, so that test checks
// the company review alone.
func TestCompanyRuntimeHasNoUsage(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the company sandbox runs on Linux")
	}
	s := newCompanySandbox(t, "0.16.0", companyJSON(true))
	for i := 0; i < 2; i++ {
		cmd := exec.Command(s.binary, "run", "--unattended")
		cmd.Dir, cmd.Env = s.work, s.env
		_, _ = cmd.CombinedOutput()
	}
	raw, err := os.ReadFile(filepath.Join(s.home, ".rosmarops", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]json.RawMessage
	if err := json.Unmarshal(raw, &st); err != nil {
		t.Fatal(err)
	}
	if _, ok := st["usage"]; ok {
		t.Errorf("the company review's state has a usage answer: %s", st["usage"])
	}
	if _, err := os.Stat(filepath.Join(report.DataDir(filepath.Join(report.DefaultReportDirIn(s.home))), usage.LastFileName)); err == nil {
		t.Error("a usage summary was written in the company review")
	}
}

// A build that is not a published version never sends usage statistics, even
// with the state recording them on: behind the recording proxy, a review of a
// plain `go build` (0.1.0-dev) asks for no connection at all.
func TestNoNetwork_Usage_DevBuildNeverSends(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the proxy+strace network check is implemented for Linux")
	}
	strace, err := exec.LookPath("strace")
	if err != nil {
		t.Skip("strace not available")
	}
	recorder := buildBin(t, "./tests/trust/testdata/proxyrecorder", nil)
	bin := buildBin(t, "./cmd/rosmarops", nil)
	home := usageHome(t, usageAnswer(true))
	connects, inet := runBehindRecordingProxy(t, strace, recorder, bin, home, "run", "--unattended")
	if len(connects) != 0 || len(inet) != 0 {
		t.Errorf("a development build connected: CONNECT %v, inet %v", connects, inet)
	}
	if _, err := os.Stat(filepath.Join(report.DataDir(report.DefaultReportDirIn(home)), usage.LastFileName)); err == nil {
		t.Errorf("a development build built a usage package")
	}
}
