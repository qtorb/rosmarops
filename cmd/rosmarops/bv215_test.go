package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/spacenet"
	"github.com/qtorb/rosmarops/internal/state"
)

// bv215Offer runs the end-of-double-click console offer (connectInConsole,
// macOS and Linux) with input on the console. connectProtocol is the fake
// network: spacenet.Connect makes no request before connect/start, so a call
// to it is the first network request (start) and nothing else reaches the
// network. It returns the protocol calls, the state after and the console.
func bv215Offer(t *testing.T, input string) (calls int, st state.State, out string) {
	t.Helper()
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
	stPath := writeState(t, "")
	paths := spacePaths{store: filepath.Join(t.TempDir(), "kit.none"), state: stPath, reports: t.TempDir()}

	prevIn, prevOut, prevHook, prevProto := kitInput, kitOutput, connectOfferHook, connectProtocol
	t.Cleanup(func() { kitInput, kitOutput, connectOfferHook, connectProtocol = prevIn, prevOut, prevHook, prevProto })
	var buf bytes.Buffer
	kitInput, kitOutput = strings.NewReader(input), &buf
	// The console offer itself (connectOffer minus its interactive-stdin gate).
	connectOfferHook = func(p spacePaths, c catalog.Catalog) connectResult { return connectInConsole(c, p) }
	connectProtocol = func(context.Context, spacenet.Permission, string, func(int), func(string, string) error) (spacenet.Material, func(context.Context) error, error) {
		calls++
		return spacenet.Material{}, nil, spacenet.ErrConnectExpired
	}

	offerConnectAfterReport(paths, cat, stPath, now)
	st, err = state.LoadState(stPath)
	if err != nil {
		t.Fatal(err)
	}
	return calls, st, buf.String()
}

// TestBV215ConsoleConsent (invariant 1): off Windows the connect offer asks and
// waits. Anything but a yes — an empty line, «n», the end of the input, any
// other answer — makes no network request, records no permission and saves the
// 7-day decline. It fails if the program connects again without reading the
// console.
func TestBV215ConsoleConsent(t *testing.T) {
	cat, _ := catalog.Load("en")
	prompt := cat.Text("connect.console.prompt", nil)
	for _, in := range []string{"\n", "n\n", "", "no\n", "x\n", "   \n", "nope"} {
		calls, st, out := bv215Offer(t, in)
		if calls != 0 {
			t.Errorf("input %q: %d network calls, want 0 (connected without a yes)", in, calls)
		}
		if st.SpacePermission {
			t.Errorf("input %q: SpacePermission recorded without a yes", in)
		}
		if st.ConnectOfferDeclinedAt != "2026-11-11T12:00:00Z" {
			t.Errorf("input %q: decline not saved (ConnectOfferDeclinedAt=%q)", in, st.ConnectOfferDeclinedAt)
		}
		if !strings.Contains(out, prompt) {
			t.Errorf("input %q: the console prompt was not shown:\n%s", in, out)
		}
	}
	// A yes, in any of its forms, starts the connection (connect/start).
	for _, in := range []string{"s\n", "y\n", "Y\n", " yes \n", "si\n", "sí\n", "SÍ\n", "s"} {
		calls, st, _ := bv215Offer(t, in)
		if calls != 1 {
			t.Errorf("input %q: %d network calls, want 1 (start)", in, calls)
		}
		if st.ConnectOfferDeclinedAt != "" {
			t.Errorf("input %q: a yes saved a decline", in)
		}
	}
}

// TestBV215ConsoleYes: only y, yes, s, si and sí, trimmed and in any case.
func TestBV215ConsoleYes(t *testing.T) {
	for _, in := range []string{"y", "yes", "s", "si", "sí", " Y\n", "YES\r\n", "Sí"} {
		if !consoleYes(in) {
			t.Errorf("consoleYes(%q) = false, want true", in)
		}
	}
	for _, in := range []string{"", "\n", "n", "no", "ye", "yess", "sii", "oui", "1", "y y"} {
		if consoleYes(in) {
			t.Errorf("consoleYes(%q) = true, want false", in)
		}
	}
}
