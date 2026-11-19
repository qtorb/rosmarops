package main

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

// With the connect window's self-test hook set, the window's SHOWN line is
// logged with the window's name and read as «Not now»; without the hook the
// same line logs nothing.
func TestConnectSelfTestShownLogged(t *testing.T) {
	run := func(func(int), func(string, string) error) error {
		t.Fatal("the connection must not run for a window that did not press «Save»")
		return nil
	}
	log := captureLog(t)
	t.Setenv(connectSelfTestEnv, "1")
	if got := driveConnectWindow(strings.NewReader("SHOWN\nDPI=96\n"), &writerTo{&strings.Builder{}}, testCat(t), nil, run); got != connectDeclined {
		t.Fatalf("result %d, want declined", got)
	}
	if !strings.Contains(log.String(), "window self-test: shown") || !strings.Contains(log.String(), "window=connect") {
		t.Fatalf("log has no self-test line for the connect window:\n%s", log)
	}

	log.Reset()
	t.Setenv(connectSelfTestEnv, "")
	_ = driveConnectWindow(strings.NewReader("SHOWN\n"), &writerTo{&strings.Builder{}}, testCat(t), nil, run)
	if strings.Contains(log.String(), "self-test") {
		t.Fatalf("without the hook nothing is logged, got:\n%s", log)
	}
}

// The findings window's hook logs its own name; under the hook, any other
// first line is logged as it came, never as «shown».
func TestFindingsSelfTestShownLogged(t *testing.T) {
	log := captureLog(t)
	t.Setenv(findingsSelfTestEnv, "1")
	noteSelfTestShown("SHOWN\r\n", "findings", findingsSelfTestEnv)
	if !strings.Contains(log.String(), "window=findings") {
		t.Fatalf("log has no self-test line for the findings window:\n%s", log)
	}
	log.Reset()
	noteSelfTestShown("save\n", "findings", findingsSelfTestEnv)
	if strings.Contains(log.String(), "window self-test: shown") || !strings.Contains(log.String(), `line="save\n"`) {
		t.Fatalf("an answer is not «shown», and is logged as it came; got:\n%s", log)
	}
}

// A window process that does not end once its input is closed no longer holds
// the run: it is ended after the limit and the end is logged.
func TestWindowThatDoesNotCloseIsEnded(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep command to stand in for a window that does not close")
	}
	log := captureLog(t)
	cmd := exec.Command(sleep, "60")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	waitWindowExit(cmd, "connect", 200*time.Millisecond)
	if d := time.Since(start); d > 10*time.Second {
		t.Fatalf("waited %s for a window that does not close", d)
	}
	if !strings.Contains(log.String(), "window did not close; ending it") {
		t.Fatalf("the end is not logged:\n%s", log)
	}
}

// Once «Save to my space» is pressed, a window whose OK is never pressed is
// closed after connectPressedLimit, and the connection's own result stands.
func TestConnectPressedWindowIsClosedAfterLimit(t *testing.T) {
	answer := make(chan time.Time)
	pressedFire := make(chan time.Time, 1)
	after := func(d time.Duration) <-chan time.Time {
		if d == connectPressedLimit {
			return pressedFire
		}
		return answer
	}
	done := make(chan connectResult, 1)
	pressed := make(chan struct{})
	close(pressed)
	killed := false
	kill := func() { killed = true; done <- connectConnected }
	go func() {
		time.Sleep(10 * time.Millisecond)
		pressedFire <- time.Time{}
	}()
	if got := awaitConnectWindow(done, pressed, connectAnswerLimit, after, kill); got != connectConnected || !killed {
		t.Fatalf("result %d, killed %v; want the connection's result and the window closed", got, killed)
	}
}

// The connect window closes when its input ends, as the findings window does,
// so it cannot stay open once RosmarOps is done with it.
func TestConnectWindowClosesAtEndOfInput(t *testing.T) {
	if !strings.Contains(connectWindowScript, "if ($line -eq $script:eof) { $timer.Stop(); $form.Close(); return }") {
		t.Fatal("the connect window does not close when its input ends")
	}
}
