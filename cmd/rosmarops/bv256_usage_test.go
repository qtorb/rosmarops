package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/spacenet"
	"github.com/qtorb/rosmarops/internal/state"
	"github.com/qtorb/rosmarops/internal/usage"
)

// usageTestHome is a HOME with a state whose usage answer is u, a review for
// the usage package, and the send seam counting calls.
func usageTestHome(t *testing.T, u *state.Usage, input string) (stPath string, sends *int) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	stPath, err := state.StatePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(stPath), 0o700); err != nil {
		t.Fatal(err)
	}
	st, err := state.DefaultState("0.15.0")
	if err != nil {
		t.Fatal(err)
	}
	st.Usage = u
	if err := state.SaveState(stPath, st); err != nil {
		t.Fatal(err)
	}
	n := 0
	prevSend, prevIn, prevLast, prevNow, prevVersion := sendUsageFn, runInput, lastUsageInput, usageNow, version
	t.Cleanup(func() {
		sendUsageFn, runInput, lastUsageInput, usageNow, version = prevSend, prevIn, prevLast, prevNow, prevVersion
	})
	version = "0.15.0" // a published version: only these ask and send
	sendUsageFn = func(context.Context, spacenet.UsageConsent, []byte) (int, error) { n++; return 204, nil }
	runInput = strings.NewReader(input)
	usageNow = func() time.Time { return time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC) }
	lastUsageInput = &usage.Input{Version: "0.15.0", OS: "linux", Arch: "amd64", Channel: usage.ChannelOther, Status: "green"}
	return stPath, &n
}

func loadUsage(t *testing.T, stPath string) *state.Usage {
	t.Helper()
	st, err := state.LoadState(stPath)
	if err != nil {
		t.Fatal(err)
	}
	return st.Usage
}

// The answers. Enter, s, S, y, Y are yes; n, N are no; anything else
// asks once more and then counts as no; a closed input is no.
func TestAskUsageAnswers(t *testing.T) {
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	for in, want := range map[string]bool{
		"\n": true, "s\n": true, "S\n": true, "y\n": true, "Y\n": true, "  y  \n": true,
		"n\n": false, "N\n": false, "maybe\ny\n": true, "maybe\nwhat\ny\n": false, "": false,
	} {
		var out bytes.Buffer
		if got := askUsage(strings.NewReader(in), &out, cat); got != want {
			t.Errorf("%q: %v, want %v", in, got, want)
		}
		if !strings.Contains(out.String(), cat.Text("usage.ask.prompt", nil)) {
			t.Errorf("%q: no prompt in %q", in, out.String())
		}
	}
}

// Without a console (or with --unattended, which the caller turns into
// canAskNow false), nothing is asked and nothing is sent.
func TestUsageNoConsoleNoQuestionNoSend(t *testing.T) {
	stPath, sends := usageTestHome(t, nil, "y\n")
	afterReportUsage(t.TempDir(), "en", false)
	if u := loadUsage(t, stPath); u != nil || *sends != 0 {
		t.Errorf("without a console: usage %+v, %d sends", u, *sends)
	}
}

// Enter is yes, and the yes sends at once; n is no and sends nothing.
func TestUsageAnswerThenSend(t *testing.T) {
	stPath, sends := usageTestHome(t, nil, "\n")
	reports := t.TempDir()
	afterReportUsage(reports, "en", true)
	u := loadUsage(t, stPath)
	if u == nil || !u.Enabled || u.TextVersion != usage.TextVersion || u.DecidedAt != "2026-11-18T12:00:00Z" || *sends != 1 {
		t.Fatalf("Enter: usage %+v, %d sends", u, *sends)
	}
	if _, err := os.Stat(filepath.Join(report.DataDir(reports), usage.LastFileName)); err != nil {
		t.Errorf("no %s: %v", usage.LastFileName, err)
	}

	stPath, sends = usageTestHome(t, nil, "n\n")
	afterReportUsage(t.TempDir(), "en", true)
	if u := loadUsage(t, stPath); u == nil || u.Enabled || *sends != 0 {
		t.Errorf("n: usage %+v, %d sends", u, *sends)
	}
}

// An answer to an older text is asked again; an answer to the current
// one is not (and, when on, a scheduled review still sends).
func TestUsageOlderTextAsksAgain(t *testing.T) {
	// The answer was to text 1; the program now shows text 2.
	old := &state.Usage{Enabled: false, DecidedAt: "2026-01-01T00:00:00Z", TextVersion: 1, InstallID: "3b2c1d4e-5f60-4a7b-8c9d-0e1f2a3b4c5d"}
	stPath, _ := usageTestHome(t, old, "y\n")
	prev := usageTextVersion
	usageTextVersion = 2
	afterReportUsage(t.TempDir(), "en", true)
	usageTextVersion = prev
	if u := loadUsage(t, stPath); u == nil || !u.Enabled || u.TextVersion != 2 {
		t.Errorf("older text: usage %+v", u)
	}

	on := &state.Usage{Enabled: true, DecidedAt: "2026-11-01T00:00:00Z", TextVersion: usage.TextVersion, InstallID: "3b2c1d4e-5f60-4a7b-8c9d-0e1f2a3b4c5d"}
	stPath, sends := usageTestHome(t, on, "n\n")
	afterReportUsage(t.TempDir(), "en", false)
	if u := loadUsage(t, stPath); u.DecidedAt != on.DecidedAt || *sends != 1 {
		t.Errorf("current text, scheduled: usage %+v, %d sends", u, *sends)
	}
}

// A new install id when usage goes from off to on; the same one while
// it stays on or goes off.
func TestUsageInstallIDOffToOn(t *testing.T) {
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	var st state.State
	if err := decideUsage(&st, false, now); err != nil {
		t.Fatal(err)
	}
	offID := st.Usage.InstallID
	if err := decideUsage(&st, true, now); err != nil {
		t.Fatal(err)
	}
	onID := st.Usage.InstallID
	if onID == offID || onID == "" {
		t.Errorf("off to on kept the id %q", onID)
	}
	_ = decideUsage(&st, true, now)
	if st.Usage.InstallID != onID {
		t.Error("on to on changed the id")
	}
	_ = decideUsage(&st, false, now)
	if st.Usage.InstallID != onID {
		t.Error("on to off changed the id")
	}
	_ = decideUsage(&st, true, now)
	if st.Usage.InstallID == onID {
		t.Error("off to on again kept the id")
	}
}

// `rosmarops usage`, `usage on` and `usage off` print the state's line.
func TestUsageCommand(t *testing.T) {
	stPath, _ := usageTestHome(t, nil, "")
	cat, _ := catalog.Load("en")
	run := func(args ...string) string {
		r, w, _ := os.Pipe()
		prev := os.Stdout
		os.Stdout = w
		err := runUsage(cli.Invocation{Flags: map[string]string{"lang": "en"}, Args: args})
		w.Close()
		os.Stdout = prev
		b, _ := io.ReadAll(r)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return strings.TrimSpace(string(b))
	}
	if got := run(); got != cat.Text("usage.state.undecided", nil) {
		t.Errorf("no argument: %q", got)
	}
	if got := run("on"); got != cat.Text("usage.state.on", nil) || !loadUsage(t, stPath).Enabled {
		t.Errorf("on: %q", got)
	}
	if got := run("off"); got != cat.Text("usage.state.off", nil) || loadUsage(t, stPath).Enabled {
		t.Errorf("off: %q", got)
	}
}
