//go:build windows

package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestConnectWindowSelfTest runs the real WinForms window on windows-latest and
// proves the BV-145 fix: the window paints (Shown) and, fed "number 42", shows
// the number within 3 s without the interface thread freezing; "done" then
// shows the result. It is gated on ROSMAROPS_CONNECT_WINDOW_SELFTEST_RUN so the
// normal `go test ./...` on the verify-windows job never launches a GUI; the
// manual diagnostic-windows.yml job sets it. In self-test the script drives
// itself (no click) and echoes SHOWN / NUMLABEL=<n> / RESULT=<text>.
func TestConnectWindowSelfTest(t *testing.T) {
	if os.Getenv("ROSMAROPS_CONNECT_WINDOW_SELFTEST_RUN") == "" {
		t.Skip("set ROSMAROPS_CONNECT_WINDOW_SELFTEST_RUN=1 on a Windows runner with a desktop session")
	}

	ps1 := filepath.Join(t.TempDir(), "connectwindow.ps1")
	if err := os.WriteFile(ps1, []byte(connectWindowScript), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-STA", "-File", ps1)
	cmd.Env = append(os.Environ(), "ROSMAROPS_CONNECT_WINDOW_SELFTEST=1")
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
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	cfg := connectWindowConfig{
		Title: "RosmarOps", Q: "Q", Text: "T", Yes: "Y", No: "N", Number: "Pick this number",
		Wait: "Wait", Done: "DONE_OK", Expired: "EXP", Failed: "FAIL", OK: "OK",
	}
	// Config line, then the two state lines the program would send.
	if _, err := stdin.Write([]byte(connectWindowInput(cfg) + "\nnumber 42\ndone\n")); err != nil {
		t.Fatal(err)
	}

	lines := make(chan string, 16)
	go func() {
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			lines <- strings.TrimSpace(sc.Text())
		}
		close(lines)
	}()

	var sawShown, sawNumber, sawResult bool
	deadline := time.After(3 * time.Second)
	for !sawNumber || !sawResult {
		select {
		case line, ok := <-lines:
			if !ok {
				t.Fatalf("window exited early; saw shown=%v number=%v result=%v", sawShown, sawNumber, sawResult)
			}
			switch {
			case line == "SHOWN":
				sawShown = true
			case line == "NUMLABEL=42":
				sawNumber = true
			case line == "RESULT=DONE_OK":
				sawResult = true
			}
		case <-deadline:
			t.Fatalf("timed out after 3 s; saw shown=%v number=%v result=%v", sawShown, sawNumber, sawResult)
		}
	}
	if !sawShown {
		t.Error("the window did not report Shown: it may not have painted")
	}
}
