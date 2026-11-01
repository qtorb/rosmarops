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
	run := func(onNumber func(int), onOpen func(string) error) error {
		onNumber(42)
		_ = onOpen("https://app.rosmarops.com/connect#v1.x.y")
		return nil
	}
	connected := driveConnectWindow(out, &writerTo{&in}, run)
	if !connected {
		t.Error("want connected on a successful protocol")
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
	run := func(func(int), func(string) error) error { ran = true; return nil }
	if driveConnectWindow(strings.NewReader("no\n"), &writerTo{&strings.Builder{}}, run) {
		t.Error("want not connected on «Not now»")
	}
	if ran {
		t.Error("the protocol ran despite «Not now»")
	}
}

// An expired/cancelled protocol tells the window "expired" and reports not
// connected.
func TestDriveConnectWindow_Expired(t *testing.T) {
	var in strings.Builder
	run := func(func(int), func(string) error) error { return spacenet.ErrConnectExpired }
	if driveConnectWindow(strings.NewReader("connect\nok\n"), &writerTo{&in}, run) {
		t.Error("want not connected on expiry")
	}
	if !strings.Contains(in.String(), "expired") {
		t.Errorf("expired state not sent: %q", in.String())
	}
}

// Any other error tells the window "failed".
func TestDriveConnectWindow_Failed(t *testing.T) {
	var in strings.Builder
	run := func(func(int), func(string) error) error { return errors.New("boom") }
	if driveConnectWindow(strings.NewReader("connect\nok\n"), &writerTo{&in}, run) {
		t.Error("want not connected on failure")
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
