package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/spacenet"
	"github.com/qtorb/rosmarops/internal/state"
)

// The connect offer at the end of a double-click review (BV-137 point 5;
// BV-158 part 4, decision A). When this computer is not connected, and the
// offer was not declined in the last 7 days, RosmarOps offers to connect after
// the report has opened: on Windows a WinForms window, on macOS the console
// question `rosmarops connect` asks. «Connect» runs today's protocol; «Not now»
// or closing is saved so the offer is not shown again for 7 days. A connection
// failure changes nothing (not even the exit code). It carries no secret, and
// it never runs in a scheduled review or in `run`.
//
// This file holds the cross-platform offer gate and the Go driver that talks to
// the window over its standard streams (tested with pipes). The window itself
// (the PowerShell WinForms script) is in connectwindow_windows.go; the console
// offer is in connectwindow_other.go.

// connectResult is the outcome of showing the connect offer.
type connectResult int

const (
	connectNotOffered connectResult = iota // already connected, or no way to offer
	connectDeclined                        // «Not now», closed, or no answer in time
	connectConnected                       // connected now
	connectFailed                          // the person agreed but the protocol did not finish
)

// connectOfferHook is the double click's call to the platform offer. It is a
// seam: production is connectOffer (a real window on Windows, the console
// question on macOS); the cmd tests disable it (TestMain), since a real window
// or a console prompt would hang a headless test with nobody to answer it.
var connectOfferHook = connectOffer

// offerConnectAfterReport shows the connect offer once, when this computer is
// not connected and the offer was not declined in the last 7 days (decision A).
// «Not now» or closing is saved to the state so it is not shown again for 7
// days. A connection failure is logged and otherwise ignored. The caller only
// invokes it from the double click (window mode, never --unattended, never
// `run`). now is the clock (the tests pass a fixed one).
func offerConnectAfterReport(paths spacePaths, cat catalog.Catalog, stPath string, now time.Time) {
	if isSpaceConnected(paths) {
		return // permission AND a credential: already connected, no offer
	}
	st, err := state.LoadState(stPath)
	if err != nil {
		slog.Info("connect offer: state not read; not offering", "err", err)
		return
	}
	if !connectOfferDue(st.ConnectOfferDeclinedAt, now) {
		return // declined less than 7 days ago
	}
	switch connectOfferHook(paths, cat) {
	case connectDeclined:
		st.ConnectOfferDeclinedAt = now.UTC().Format(time.RFC3339)
		if err := state.SaveState(stPath, st); err != nil {
			slog.Warn("connect offer: the decline could not be saved", "err", err)
		}
	case connectConnected:
		slog.Info("connect offer: this computer is now connected")
		// BV-176: send this run's package now, by the normal send path, and say
		// the result in the console. The report already open is not rewritten.
		sendAfterConnect(paths, cat, now)
	}
}

// connectOfferDue reports whether the offer may be shown: never declined, or
// declined 7 days ago or more. An empty or unparseable time counts as due.
func connectOfferDue(declinedAt string, now time.Time) bool {
	if declinedAt == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339, declinedAt)
	if err != nil {
		return true
	}
	return now.Sub(t) >= 7*24*time.Hour
}

// driveConnectWindow drives the window over its streams. It reads the first
// line the window writes: "connect" starts the protocol, anything else (or a
// closed stream) means the person declined. On "connect" it calls pressed (the
// 10 minutes start now, BV-227 §3), then runs run, wiring onNumber and onOpen to
// the window: onOpen opens the browser and sends one "wait <base64 JSON>" line
// with the address and the connect.code line (BV-227 §1, §2), which the window
// shows while it waits. Then it tells the window the final state and waits for
// its "ok". It returns the outcome. The code goes only to the window, never to
// the log.
func driveConnectWindow(out io.Reader, in io.Writer, cat catalog.Catalog, pressed func(), run func(onNumber func(int), onOpen func(string, string) error) error) connectResult {
	r := bufio.NewReader(out)
	first, _ := r.ReadString('\n')
	if strings.TrimRight(first, "\r\n") != "connect" {
		return connectDeclined
	}
	if pressed != nil {
		pressed()
	}
	onNumber := func(n int) { fmt.Fprintf(in, "number %d\n", n) }
	onOpen := func(u, code string) error {
		fmt.Fprintf(in, "wait %s\n", connectWaitLine(cat, u, code))
		openSpaceURL(u)
		return nil
	}
	stateLine := "done"
	result := connectConnected
	switch err := run(onNumber, onOpen); {
	case err == nil:
	case errors.Is(err, spacenet.ErrConnectExpired),
		errors.Is(err, spacenet.ErrConnectCancelled),
		errors.Is(err, context.DeadlineExceeded):
		stateLine, result = "expired", connectFailed
	default:
		stateLine, result = "failed", connectFailed
	}
	fmt.Fprintf(in, "%s\n", stateLine)
	_, _ = r.ReadString('\n') // the window's "ok", or its stream ending
	return result
}

// connectWait is what the window shows while it waits (BV-227 §4), in this
// order: connect.waiting, the address (selectable), connect.code with the code
// as XXXX-XXXX ("" when the server gave none) and connect.time.
type connectWait struct {
	Waiting string `json:"waiting"`
	URL     string `json:"url"`
	Code    string `json:"code"`
	Time    string `json:"time"`
}

// connectWaitLine is the base64 of connectWait's JSON, so no code page touches
// the texts on the way to the window.
func connectWaitLine(cat catalog.Catalog, u, code string) string {
	w := connectWait{Waiting: cat.Text("connect.waiting", nil), URL: u, Time: cat.Text("connect.time", nil)}
	if code != "" {
		w.Code = cat.Text("connect.code", map[string]string{"code": code})
	}
	b, _ := json.Marshal(w)
	return base64.StdEncoding.EncodeToString(b)
}

// awaitConnectWindow waits for the window's outcome (CTO-15 PR5, BV-227 §3).
// Before «Save to my space» is pressed, a window nobody answers within
// answerWithin closes as «Not now» (kill closes it, so done then delivers).
// Once pressed, that limit no longer applies: the 10 minutes are the
// connection's own (spacenet's poll deadline and the request's life), which
// start at the press, and done is awaited. after is time.After in production.
func awaitConnectWindow(done <-chan connectResult, pressed <-chan struct{}, answerWithin time.Duration, after func(time.Duration) <-chan time.Time, kill func()) connectResult {
	timeout := after(answerWithin)
	for {
		select {
		case r := <-done:
			return r
		case <-pressed:
			pressed, timeout = nil, nil
		case <-timeout:
			slog.Info("connect window: no answer in 10 minutes; closing as «Not now» and continuing")
			kill()
			<-done
			return connectDeclined
		}
	}
}
