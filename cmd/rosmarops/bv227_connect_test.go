package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/spacenet"
)

const (
	bv227URL  = "https://app.rosmarops.com/connect#v1.0123456789abcdef0123456789abcdef.AAAA"
	bv227Code = "K7M2-QX9P"
)

func testCat(t *testing.T) catalog.Catalog {
	t.Helper()
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	return cat
}

// TestBV227WindowWait (BV-227 §1, §2, §4): after «Save to my space» the window
// is told the press first, then gets one wait line with, in order,
// connect.waiting, the address, connect.code with the code as XXXX-XXXX and
// connect.time; without a code, connect.code is left out.
func TestBV227WindowWait(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		for _, code := range []string{bv227Code, ""} {
			var in strings.Builder
			var order []string
			pressed := func() { order = append(order, "pressed") }
			run := func(_ func(int), onOpen func(string, string) error) error {
				order = append(order, "run")
				return onOpen(bv227URL, code)
			}
			if got := driveConnectWindow(strings.NewReader("connect\nok\n"), &writerTo{&in}, cat, pressed, run); got != connectConnected {
				t.Fatalf("%s: result %d", lang, got)
			}
			if strings.Join(order, ",") != "pressed,run" {
				t.Errorf("%s: order %v: the press must come before the connection", lang, order)
			}
			var line string
			for _, l := range strings.Split(in.String(), "\n") {
				if strings.HasPrefix(l, "wait ") {
					line = strings.TrimPrefix(l, "wait ")
				}
			}
			raw, err := base64.StdEncoding.DecodeString(line)
			if err != nil {
				t.Fatalf("%s: wait line %q: %v", lang, in.String(), err)
			}
			var w connectWait
			if err := json.Unmarshal(raw, &w); err != nil {
				t.Fatal(err)
			}
			wantCode := ""
			if code != "" {
				wantCode = cat.Text("connect.code", map[string]string{"code": bv227Code})
			}
			if w.Waiting != cat.Text("connect.waiting", nil) || w.URL != bv227URL || w.Code != wantCode || w.Time != cat.Text("connect.time", nil) {
				t.Errorf("%s: wait %+v", lang, w)
			}
			// The JSON fields keep the signed order: waiting, url, code, time.
			if i := []int{bytes.Index(raw, []byte(`"waiting"`)), bytes.Index(raw, []byte(`"url"`)), bytes.Index(raw, []byte(`"code"`)), bytes.Index(raw, []byte(`"time"`))}; !(i[0] < i[1] && i[1] < i[2] && i[2] < i[3]) {
				t.Errorf("%s: field order %s", lang, raw)
			}
		}
	}
}

// TestBV227ConsoleWaiting (BV-227 §4): on macOS and Linux the console prints,
// in order, connect.waiting, the address, the code line and connect.time.
func TestBV227ConsoleWaiting(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		var out bytes.Buffer
		printConnectWaiting(&out, cat, bv227URL, bv227Code)
		want := strings.Join([]string{
			cat.Text("connect.waiting", nil), bv227URL,
			cat.Text("connect.code", map[string]string{"code": bv227Code}), cat.Text("connect.time", nil),
		}, "\n") + "\n"
		if out.String() != want {
			t.Errorf("%s: console\n%s\nwant\n%s", lang, out.String(), want)
		}
		if !strings.Contains(out.String(), "K7M2-QX9P") {
			t.Errorf("%s: the code is not shown as XXXX-XXXX", lang)
		}
		out.Reset()
		printConnectWaiting(&out, cat, bv227URL, "")
		if strings.Contains(out.String(), strings.SplitN(cat.Text("connect.code", nil), "{", 2)[0]) {
			t.Errorf("%s: a code line without a code", lang)
		}
	}
}

// TestBV227TimerStartsAtPress (BV-227 §3): a window open 9 minutes before the
// press does not close 1 minute after it; before any press, 10 minutes without
// an answer still close it as «Not now».
func TestBV227TimerStartsAtPress(t *testing.T) {
	// A fake clock: the answer timeout is a channel the test fires.
	fire := make(chan time.Time, 1)
	after := func(time.Duration) <-chan time.Time { return fire }

	// Pressed after 9 minutes: the answer timeout then fires (minute 10), and
	// the window must not be killed; the connection's own result arrives later.
	done := make(chan connectResult, 1)
	pressed := make(chan struct{})
	killed := false
	go func() {
		close(pressed)                    // minute 9: «Save to my space»
		time.Sleep(10 * time.Millisecond) // let the waiter see the press
		fire <- time.Time{}               // minute 10: the old limit
		time.Sleep(10 * time.Millisecond) // nothing must happen
		done <- connectConnected          // minute 12: confirmed in the browser
	}()
	if got := awaitConnectWindow(done, pressed, 10*time.Minute, after, func() { killed = true }); got != connectConnected || killed {
		t.Errorf("pressed at minute 9: result %d, killed %v; want connected, not killed", got, killed)
	}

	// Never pressed: the 10 minutes close the window as «Not now».
	done = make(chan connectResult, 1)
	fire = make(chan time.Time, 1)
	killed = false
	fire <- time.Time{}
	kill := func() { killed = true; done <- connectDeclined }
	if got := awaitConnectWindow(done, make(chan struct{}), 10*time.Minute, func(time.Duration) <-chan time.Time { return fire }, kill); got != connectDeclined || !killed {
		t.Errorf("never pressed: result %d, killed %v; want declined and killed", got, killed)
	}
}

// TestBV227CodeNotKept (BV-227 §1): the code goes to the screen only, never to
// the log, the state or the kit store.
func TestBV227CodeNotKept(t *testing.T) {
	log := captureLog(t)
	stPath := writeState(t, "")
	store := filepath.Join(t.TempDir(), "kit.none")
	paths := spacePaths{store: store, state: stPath, reports: t.TempDir()}
	orig := connectProtocol
	t.Cleanup(func() { connectProtocol = orig })
	var shown bytes.Buffer
	connectProtocol = func(_ context.Context, _ spacenet.Permission, _ string, _ func(int), onOpen func(string, string) error) (spacenet.Material, func(context.Context) error, error) {
		_ = onOpen(bv227URL, bv227Code)
		return spacenet.Material{}, nil, spacenet.ErrConnectExpired
	}
	cat := testCat(t)
	err := spaceConnect(context.Background(), paths, func(int) {}, func(u, code string) error {
		printConnectWaiting(&shown, cat, u, code)
		return nil
	})
	if err == nil {
		t.Fatal("the stub expiry did not come back")
	}
	if !strings.Contains(shown.String(), bv227Code) {
		t.Fatalf("the code was not shown: %q", shown.String())
	}
	for _, c := range []string{bv227Code, strings.ReplaceAll(bv227Code, "-", "")} {
		if strings.Contains(log.String(), c) {
			t.Errorf("the log has the code: %s", log.String())
		}
		if raw, _ := os.ReadFile(stPath); bytes.Contains(raw, []byte(c)) {
			t.Error("the state has the code")
		}
		if raw, err := os.ReadFile(store); err == nil && bytes.Contains(raw, []byte(c)) {
			t.Error("the kit store has the code")
		}
		filepath.WalkDir(paths.reports, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if raw, _ := os.ReadFile(p); bytes.Contains(raw, []byte(c)) {
					t.Errorf("%s has the code", p)
				}
			}
			return nil
		})
	}
}

// TestBV227FailureOfferedAgain (BV-227 §5): a failed connection is offered
// again at the very next opening, with no pause.
func TestBV227FailureOfferedAgain(t *testing.T) {
	cat := testCat(t)
	stPath := writeState(t, "")
	paths := spacePaths{store: filepath.Join(t.TempDir(), "kit.none"), state: stPath, reports: t.TempDir()}
	restore := connectOfferHook
	t.Cleanup(func() { connectOfferHook = restore })
	calls := 0
	connectOfferHook = func(spacePaths, catalog.Catalog) connectResult { calls++; return connectFailed }
	now := time.Date(2026, 11, 13, 7, 0, 0, 0, time.UTC)
	offerConnectAfterReport(paths, cat, stPath, now)
	offerConnectAfterReport(paths, cat, stPath, now.Add(time.Minute))
	if calls != 2 {
		t.Errorf("after a failure the offer was shown %d times in two openings, want 2", calls)
	}
}
