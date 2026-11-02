//go:build windows

package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestConnectWindowSelfTest runs the real WinForms window on windows-latest and
// proves the BV-146 reader. It is gated on ROSMAROPS_CONNECT_WINDOW_SELFTEST_RUN
// so the normal `go test ./...` on the verify-windows job never launches a GUI;
// the diagnostic-windows.yml connect-window job (manual, and on a pull request
// that touches connectwindow*.go) sets it.
//
// Three subtests, all passing:
//   - "streamreader alive": the fix. With no state line sent, the UI Timer keeps
//     ticking (the interface thread is alive); the number and the result then
//     show promptly.
//   - "consolein_async freezes" and "backgroundworker freezes": the two broken
//     readers BV-146 replaces. Each blocks the interface thread, so the self-test
//     sees almost no ticks — i.e. the alive check above would FAIL on them. This
//     is how we prove, on Windows (the only place it is observable), that the old
//     BackgroundWorker and BV-145's [Console]::In.ReadLineAsync() do freeze.
func TestConnectWindowSelfTest(t *testing.T) {
	if os.Getenv("ROSMAROPS_CONNECT_WINDOW_SELFTEST_RUN") == "" {
		t.Skip("set ROSMAROPS_CONNECT_WINDOW_SELFTEST_RUN=1 on a Windows runner with a desktop session")
	}
	t.Run("streamreader alive", testWindowAlive)
	t.Run("consolein_async freezes", func(t *testing.T) { testWindowFreezes(t, "consolein_async") })
	t.Run("backgroundworker freezes", func(t *testing.T) { testWindowFreezes(t, "backgroundworker") })
}

// testWindowAlive is the real proof of the fix: SHOWN, then at least 5 Timer
// ticks during a 2 s window in which the test sends NOTHING (the interface thread
// is free, not blocked on a read), then the number and the result show in under
// a second each once the lines arrive.
func testWindowAlive(t *testing.T) {
	lines, send, stop := runSelfWindow(t, "")
	defer stop()
	if err := send(selfConfig()); err != nil {
		t.Fatal(err)
	}
	if !waitFor(lines, 10*time.Second, func(l string) bool { return l == "SHOWN" }) {
		t.Fatal("no SHOWN: the window did not paint")
	}
	if ticks := countLines(lines, 2*time.Second, "TICK"); ticks < 5 {
		t.Fatalf("only %d TICK in 2 s with no input: the interface thread is not looping", ticks)
	}
	if err := send("number 42\n"); err != nil {
		t.Fatal(err)
	}
	if !waitFor(lines, time.Second, func(l string) bool { return l == "NUMLABEL=42" }) {
		t.Fatal("NUMLABEL=42 not shown within 1 s of sending the number")
	}
	if err := send("done\n"); err != nil {
		t.Fatal(err)
	}
	if !waitFor(lines, time.Second, func(l string) bool { return l == "RESULT=DONE_OK" }) {
		t.Fatal("RESULT=DONE_OK not shown within 1 s of sending done")
	}
}

// testWindowFreezes proves a broken reader blocks the interface thread: SHOWN
// still arrives (it is emitted before the blocking read starts), but with no
// input sent the Timer cannot tick, so fewer than 5 TICK appear in 2 s — which
// is exactly the alive check testWindowAlive would fail on. If a reader did NOT
// freeze here it would wrongly pass the self-test, so this subtest fails loudly.
func testWindowFreezes(t *testing.T, readmode string) {
	lines, send, stop := runSelfWindow(t, readmode)
	defer stop()
	if err := send(selfConfig()); err != nil {
		t.Fatal(err)
	}
	// SHOWN is emitted before Start-Reading blocks, so it should still arrive; if
	// it does not, the window froze even earlier, which is also a freeze.
	waitFor(lines, 10*time.Second, func(l string) bool { return l == "SHOWN" })
	if ticks := countLines(lines, 2*time.Second, "TICK"); ticks >= 5 {
		t.Fatalf("%s: %d TICK in 2 s — the interface thread did not freeze, so this broken reader would pass the self-test", readmode, ticks)
	}
}

// runSelfWindow launches the window script in self-test mode with the given read
// mode (empty for the fix) and returns the stdout lines, a send func for stdin,
// and a stop func (idempotent).
func runSelfWindow(t *testing.T, readmode string) (<-chan string, func(string) error, func()) {
	t.Helper()
	ps1 := filepath.Join(t.TempDir(), "connectwindow.ps1")
	if err := os.WriteFile(ps1, []byte(connectWindowScript), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA", "-File", ps1)
	cmd.Env = append(os.Environ(), "ROSMAROPS_CONNECT_WINDOW_SELFTEST=1", "ROSMAROPS_CONNECT_WINDOW_READMODE="+readmode)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	lines := make(chan string, 256)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- strings.TrimSpace(sc.Text())
		}
		close(lines)
	}()
	var once sync.Once
	stop := func() {
		once.Do(func() {
			_ = stdin.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
	}
	send := func(s string) error { _, e := stdin.Write([]byte(s)); return e }
	return lines, send, stop
}

// selfConfig is the base64 config line (with newline) the window reads first.
func selfConfig() string {
	cfg := connectWindowConfig{
		Title: "RosmarOps", Q: "Q", Text: "T", Yes: "Y", No: "N", Number: "Pick this number",
		Wait: "Wait", Done: "DONE_OK", Expired: "EXP", Failed: "FAIL", OK: "OK",
	}
	return connectWindowInput(cfg) + "\n"
}

// waitFor reports whether a line satisfying pred arrives within d. Non-matching
// lines (e.g. TICK) are discarded.
func waitFor(lines <-chan string, d time.Duration, pred func(string) bool) bool {
	deadline := time.After(d)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				return false
			}
			if pred(l) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// countLines counts lines equal to want that arrive within d.
func countLines(lines <-chan string, d time.Duration, want string) int {
	n := 0
	deadline := time.After(d)
	for {
		select {
		case l, ok := <-lines:
			if !ok {
				return n
			}
			if l == want {
				n++
			}
		case <-deadline:
			return n
		}
	}
}
