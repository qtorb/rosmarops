package trust_test

// CTO-13 §3 test 4 (CTO-15 PR3): with the person's permission and a credential,
// the release binary, run behind an HTTPS proxy that only records, asks to
// CONNECT to app.rosmarops.com:443 and nowhere else — and strace confirms the
// only inet connection it opens is to that proxy. A binary built with a hostile
// -X toward another host cannot escape: it still asks only for
// app.rosmarops.com:443 (the destination is a constant, not a linker-settable
// variable), which is the negative case.
//
// The recording proxy is a separate helper program under testdata, so this test
// package imports no network package (the no-network static check stays sound).

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/config"
	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/results"
	"github.com/qtorb/rosmarops/internal/state"
)

func TestNoNetwork_ProxyOnlyConnectsToFixedHost(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the proxy+strace network check is implemented for Linux")
	}
	strace, err := exec.LookPath("strace")
	if err != nil {
		t.Skip("strace not available")
	}
	recorder := buildBin(t, "./tests/trust/testdata/proxyrecorder", nil)

	normal := buildBin(t, "./cmd/rosmarops", nil)
	hostile := buildBin(t, "./cmd/rosmarops", []string{"-ldflags", "-X github.com/qtorb/rosmarops/internal/spacenet.baseURL=https://evil.invalid"})

	for _, tc := range []struct{ name, bin string }{
		{"release binary", normal},
		{"binary with hostile -X (negative case)", hostile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			connects, inet := runBehindRecordingProxy(t, strace, recorder, tc.bin)
			if len(connects) != 1 || connects[0] != "app.rosmarops.com:443" {
				t.Errorf("CONNECT targets = %v, want exactly [app.rosmarops.com:443]", connects)
			}
			for _, target := range inet {
				if !strings.HasPrefix(target, "127.0.0.1:") && !strings.HasPrefix(target, "[::1]:") {
					t.Errorf("an inet connection went to %q, not the local proxy", target)
				}
			}
		})
	}
}

// buildBin builds a package (by its path, which may be under testdata) with
// extra build args and returns the binary path.
func buildBin(t *testing.T, pkg string, extra []string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bin")
	args := append([]string{"build"}, extra...)
	args = append(args, "-o", bin, pkg)
	cmd := exec.Command("go", args...)
	cmd.Dir = repoRoot(t)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s %v: %v\n%s", pkg, extra, err, out)
	}
	return bin
}

// runBehindRecordingProxy starts the recorder, seeds a connected HOME, runs
// `rosmarops run` behind the recorder under strace, and returns the CONNECT
// targets the recorder saw and the inet connect targets strace saw.
func runBehindRecordingProxy(t *testing.T, strace, recorder, bin string) (connects, inet []string) {
	t.Helper()
	rec := exec.Command(recorder)
	stdout, err := rec.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rec.Process.Kill(); _ = rec.Wait() }()

	var mu sync.Mutex
	addrCh := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			f := strings.Fields(sc.Text())
			if len(f) != 2 {
				continue
			}
			switch f[0] {
			case "ADDR":
				select {
				case addrCh <- f[1]:
				default:
				}
			case "CONNECT":
				mu.Lock()
				connects = append(connects, f[1])
				mu.Unlock()
			}
		}
	}()
	var proxyAddr string
	select {
	case proxyAddr = <-addrCh:
	case <-time.After(15 * time.Second):
		t.Fatal("recorder did not report its address")
	}

	home := seedConnectedHome(t)
	traceFile := filepath.Join(t.TempDir(), "run.strace")
	argv := []string{strace, "-f", "-qq", "-Y", "-e", "trace=connect", "-o", traceFile, bin, "run"}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = home
	cmd.Env = append(os.Environ(),
		"HOME="+home, "USERPROFILE="+home,
		"HTTPS_PROXY=http://"+proxyAddr, "HTTP_PROXY=http://"+proxyAddr,
	)
	_, _ = cmd.CombinedOutput() // a deferred send (502 from the proxy) exits non-zero; the trace is what matters

	// Give the recorder a moment to flush the CONNECT it just received.
	time.Sleep(300 * time.Millisecond)
	mu.Lock()
	out := append([]string(nil), connects...)
	mu.Unlock()
	return out, inetConnects(t, traceFile)
}

// inetConnects returns the AF_INET/AF_INET6 connect targets ("ip:port") in a
// strace -e trace=connect log.
func inetConnects(t *testing.T, traceFile string) []string {
	t.Helper()
	data, err := os.ReadFile(traceFile)
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	var out []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, "AF_INET") {
			continue
		}
		ip := straceField(line, "inet_addr(\"", "\"")
		v6 := false
		if ip == "" {
			ip, v6 = straceField(line, "inet_pton(AF_INET6, \"", "\""), true
		}
		port := straceField(line, "htons(", ")")
		switch {
		case ip != "" && port != "" && v6:
			out = append(out, "["+ip+"]:"+port)
		case ip != "" && port != "":
			out = append(out, ip+":"+port)
		default:
			out = append(out, strings.TrimSpace(line)) // unparsed: surfaced so the test still catches it
		}
	}
	return out
}

// straceField pulls the text between open and close that first follows in a
// strace line, or "".
func straceField(line, open, close string) string {
	o := strings.Index(line, open)
	if o < 0 {
		return ""
	}
	rest := line[o+len(open):]
	c := strings.Index(rest, close)
	if c < 0 {
		return ""
	}
	return rest[:c]
}

// seedConnectedHome lays out ~/.rosmarops with a config, a connected state
// (permission true), a kit with a credential, and one pending results package,
// so `rosmarops run` tries to send.
func seedConnectedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	dir := filepath.Join(home, ".rosmarops")
	reports := filepath.Join(dir, "reports")
	if err := os.MkdirAll(reports, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := config.WriteConfig(filepath.Join(dir, "config.json"), config.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	st, err := state.DefaultState("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	st.SpacePermission = true
	if err := state.SaveState(filepath.Join(dir, "state.json"), st); err != nil {
		t.Fatal(err)
	}
	salt, _ := st.Salt()
	sec, err := kit.NewSecret(salt)
	if err != nil {
		t.Fatal(err)
	}
	kidRaw := make([]byte, 16)
	rand.Read(kidRaw)
	cred := make([]byte, 32)
	rand.Read(cred)
	if err := kit.Save(filepath.Join(dir, "kit.key"), kit.Stored{KitID: hex.EncodeToString(kidRaw), Secret: sec, SpaceCredential: cred}); err != nil {
		t.Fatal(err)
	}
	pkg, err := results.Seal(sec.Key, results.Header{
		Format: "rosmarops-results", Version: 1, KitID: hex.EncodeToString(kidRaw),
		KeyID: kit.KeyID(sec.Key), HostID: "host_" + hex.EncodeToString(make([]byte, 8)),
		CreatedAt: "2026-11-01T00:00:00Z", RosmaropsVersion: "1.0.0",
	}, []byte(`{"x":1}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := results.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(reports, "rosmarops-results-2026-11-01-host_000.locked.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}
