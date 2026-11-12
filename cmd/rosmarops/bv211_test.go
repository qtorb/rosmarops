package main

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// TestRunLogNotToConsole is BV-211 §1: a manual «run» routes slog to the run
// log (a deferredLog), never to stdout or stderr, so «INFO …» lines never mix
// with the console's progress. installRunLog is what runRun uses.
func TestRunLogNotToConsole(t *testing.T) {
	orig := slog.Default()
	t.Cleanup(func() { slog.SetDefault(orig) })

	log := installRunLog()
	t.Cleanup(func() { _ = log.Close() })
	slog.Info("telemetry-probe-line", "k", "v")

	// Not attached to a reports dir yet, so the line is buffered in the log,
	// which proves slog's sink is the file, not the console.
	if !bytes.Contains(log.buf, []byte("telemetry-probe-line")) {
		t.Fatalf("slog did not go to the run log; buffer: %q", log.buf)
	}
}

// TestQuickEditOff is BV-211 §2: the written mode clears QuickEdit, sets
// ENABLE_EXTENDED_FLAGS, and leaves the other bits alone.
func TestQuickEditOff(t *testing.T) {
	const other = 0x0001 | 0x0002 // echo + line input, say
	got := quickEditOff(enableQuickEditMode | other)
	if got&enableQuickEditMode != 0 {
		t.Error("QuickEdit should be cleared")
	}
	if got&enableExtendedFlags == 0 {
		t.Error("ENABLE_EXTENDED_FLAGS should be set")
	}
	if got&other != other {
		t.Error("the other input-mode bits must be preserved")
	}
}

// TestDisableQuickEditRestores is BV-211 §2, the unit with a fake console: it
// turns QuickEdit off, and the returned function restores the original mode. On
// an inherited console (not ours) or an unreadable mode it changes nothing.
func TestDisableQuickEditRestores(t *testing.T) {
	const orig uint32 = enableQuickEditMode | 0x0001

	var sets []uint32
	set := func(m uint32) bool { sets = append(sets, m); return true }
	restore := disableQuickEditWith(
		func() bool { return true },
		func() (uint32, bool) { return orig, true },
		set,
	)
	if len(sets) != 1 || sets[0] != quickEditOff(orig) {
		t.Fatalf("should have turned QuickEdit off once: %v", sets)
	}
	restore()
	if len(sets) != 2 || sets[1] != orig {
		t.Fatalf("restore should write back the original mode: %v", sets)
	}

	// Not our console: nothing is touched.
	sets = nil
	disableQuickEditWith(func() bool { return false }, func() (uint32, bool) { return orig, true }, set)()
	if len(sets) != 0 {
		t.Fatalf("an inherited console must not be changed: %v", sets)
	}

	// Mode cannot be read: nothing is touched.
	sets = nil
	disableQuickEditWith(func() bool { return true }, func() (uint32, bool) { return 0, false }, set)()
	if len(sets) != 0 {
		t.Fatalf("an unreadable console must not be changed: %v", sets)
	}
}

// TestProgressElapsedAndSlow is BV-211 §3: the repositories line carries the
// elapsed time, the slow-repository note appears once when the count has not
// moved for 15 s and not again in that review (BV-221), and nothing in the console output
// is a repository name or a path.
func TestProgressElapsedAndSlow(t *testing.T) {
	cat := enCat(t)
	var buf bytes.Buffer
	base := time.Unix(1_000_000, 0)
	clk := base
	p := &progress{out: &buf, terminal: true, cat: cat, now: func() time.Time { return clk }}
	slow := cat.Text("console.progress.repos_slow", nil)

	p.repos(0, 48) // phase starts; elapsed 0:00
	clk = base.Add(3 * time.Second)
	p.tick()
	if !strings.Contains(buf.String(), "0:03") {
		t.Fatalf("elapsed time should tick in the line:\n%q", buf.String())
	}
	if strings.Contains(buf.String(), slow) {
		t.Fatal("the slow note appeared before 15 s")
	}

	clk = base.Add(15 * time.Second) // 15 s with no change
	p.tick()
	if n := strings.Count(buf.String(), slow); n != 1 {
		t.Fatalf("slow note should appear once at 15 s, got %d", n)
	}
	clk = base.Add(25 * time.Second) // still stalled
	p.tick()
	if n := strings.Count(buf.String(), slow); n != 1 {
		t.Fatalf("slow note must not repeat for the same wait, got %d", n)
	}

	// The count moves and a new 15 s stall follows: no second note (BV-221,
	// once per review).
	clk = base.Add(26 * time.Second)
	p.repos(1, 48)
	clk = base.Add(41 * time.Second)
	p.tick()
	if n := strings.Count(buf.String(), slow); n != 1 {
		t.Fatalf("a later stall must not show the note again in the same review, got %d", n)
	}

	out := buf.String()
	for _, bad := range []string{".git", `C:\`, "/home/", `\\`, ".rosmarops"} {
		if strings.Contains(out, bad) {
			t.Fatalf("console output leaked a path-like token %q:\n%q", bad, out)
		}
	}
}

// TestProgressNonTerminalFirstAndLast is BV-211 §3: off a console the
// repositories line is printed only at the start and the end (with the elapsed
// time), never rewritten, and the slow note never appears.
func TestProgressNonTerminalFirstAndLast(t *testing.T) {
	cat := enCat(t)
	var buf bytes.Buffer
	base := time.Unix(2_000_000, 0)
	clk := base
	p := &progress{out: &buf, terminal: false, cat: cat, now: func() time.Time { return clk }}

	p.repos(0, 3)
	clk = base.Add(5 * time.Second)
	p.repos(1, 3) // a middle update: must not print off a console
	clk = base.Add(9 * time.Second)
	p.repos(3, 3) // last
	lines := strings.Count(buf.String(), "Checking repositories:")
	if lines != 2 {
		t.Fatalf("off a console only the first and last line print, got %d:\n%q", lines, buf.String())
	}
	if strings.Contains(buf.String(), "\r") {
		t.Fatal("off a console the line is never rewritten in place")
	}
}
