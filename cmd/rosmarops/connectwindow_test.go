package main

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/spacenet"
)

// When the window writes "connect", the driver runs the protocol, forwards the
// number to the window, writes the final state, and reports connected on
// success.
func TestDriveConnectWindow_Connected(t *testing.T) {
	out := strings.NewReader("connect\nok\n")
	var in strings.Builder
	var gotNumber int
	run := func(onNumber func(int), onOpen func(string, string) error) error {
		onNumber(42)
		_ = onOpen("https://app.rosmarops.com/connect#v1.x.y", "K7M2-QX9P")
		return nil
	}
	got := driveConnectWindow(out, &writerTo{&in}, testCat(t), nil, run)
	if got != connectConnected {
		t.Errorf("want connectConnected on a successful protocol, got %d", got)
	}
	if !strings.Contains(in.String(), "number 42") {
		t.Errorf("number not forwarded to the window: %q", in.String())
	}
	if !strings.Contains(in.String(), "done") {
		t.Errorf("final state not sent: %q", in.String())
	}
	_ = gotNumber
}

// «Not now» (anything but "connect") does not run the protocol and reports not
// connected.
func TestDriveConnectWindow_NotNow(t *testing.T) {
	ran := false
	run := func(func(int), func(string, string) error) error { ran = true; return nil }
	if got := driveConnectWindow(strings.NewReader("no\n"), &writerTo{&strings.Builder{}}, testCat(t), nil, run); got != connectDeclined {
		t.Errorf("want connectDeclined on «Not now», got %d", got)
	}
	if ran {
		t.Error("the protocol ran despite «Not now»")
	}
}

// An expired/cancelled protocol tells the window "expired" and reports not
// connected.
func TestDriveConnectWindow_Expired(t *testing.T) {
	var in strings.Builder
	run := func(func(int), func(string, string) error) error { return spacenet.ErrConnectExpired }
	if got := driveConnectWindow(strings.NewReader("connect\nok\n"), &writerTo{&in}, testCat(t), nil, run); got != connectFailed {
		t.Errorf("want connectFailed on expiry, got %d", got)
	}
	if !strings.Contains(in.String(), "expired") {
		t.Errorf("expired state not sent: %q", in.String())
	}
}

// Any other error tells the window "failed".
func TestDriveConnectWindow_Failed(t *testing.T) {
	var in strings.Builder
	run := func(func(int), func(string, string) error) error { return errors.New("boom") }
	if got := driveConnectWindow(strings.NewReader("connect\nok\n"), &writerTo{&in}, testCat(t), nil, run); got != connectFailed {
		t.Errorf("want connectFailed on failure, got %d", got)
	}
	if !strings.Contains(in.String(), "failed") {
		t.Errorf("failed state not sent: %q", in.String())
	}
}

// writerTo adapts a *strings.Builder to io.Writer (Builder already is one; this
// keeps the test explicit about the stream direction).
type writerTo struct{ b *strings.Builder }

func (w *writerTo) Write(p []byte) (int, error) { return w.b.Write(p) }

var _ io.Writer = (*writerTo)(nil)
