package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/problems"
)

type cancelExited struct{ code int }

// cancelHarness is a review with its in-progress mark set, whose
// cancellation exit is caught.
func cancelHarness(t *testing.T, lang string, console bool) (*reviewCancel, *problems.Notebook, string, *bytes.Buffer) {
	t.Helper()
	dir := t.TempDir()
	nb := problems.New(dir)
	if prev, err := nb.StartReview(time.Now()); err != nil || prev != "" {
		t.Fatalf("StartReview: %q, %v", prev, err)
	}
	cat, err := catalog.Load(lang)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	c := &reviewCancel{clearMark: nb.EndReview, cat: cat}
	if console {
		c.out = &out
	}
	prev := cancelExit
	t.Cleanup(func() { cancelExit = prev })
	cancelExit = func(code int) { panic(cancelExited{code}) }
	return c, nb, filepath.Join(dir, problems.RunMarkName), &out
}

// exitOf runs f and returns the exit code it ended with, -1 if none.
func exitOf(f func()) (code int) {
	code = -1
	defer func() {
		if r := recover(); r != nil {
			e, ok := r.(cancelExited)
			if !ok {
				panic(r)
			}
			code = e.code
		}
	}()
	f()
	return code
}

// BV-311 T3: a cancellation before the review writes anything clears the
// mark, says so at the console in the person's language, and exits; the
// next review records no run_unfinished.
func TestCancelBeforeWrites(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		c, nb, mark, out := cancelHarness(t, lang, true)
		if code := exitOf(c.cancel); code != exitCancelled {
			t.Fatalf("%s: exit %d, want %d", lang, code, exitCancelled)
		}
		if _, err := os.Stat(mark); !os.IsNotExist(err) {
			t.Errorf("%s: the in-progress mark is still there", lang)
		}
		want := map[string]string{
			"es": "Revisión cancelada. El informe anterior no ha cambiado.\n",
			"en": "Review cancelled. The previous report has not changed.\n",
		}[lang]
		if out.String() != want {
			t.Errorf("%s: console %q, want %q", lang, out.String(), want)
		}
		if prev, _ := nb.StartReview(time.Now()); prev != "" {
			t.Errorf("%s: the next review sees an unfinished one (%s)", lang, prev)
		}
	}
}

// With no console, nothing is written to it.
func TestCancelNoConsole(t *testing.T) {
	c, _, mark, out := cancelHarness(t, "en", false)
	if code := exitOf(c.cancel); code != exitCancelled {
		t.Fatalf("exit %d", code)
	}
	if out.Len() != 0 {
		t.Errorf("wrote %q with no console", out.String())
	}
	if _, err := os.Stat(mark); !os.IsNotExist(err) {
		t.Error("the in-progress mark is still there")
	}
}

// A cancellation while the results are written waits for the writes to end,
// then exits with the mark cleared and no line: the report has changed.
func TestCancelDuringWrites(t *testing.T) {
	c, nb, mark, out := cancelHarness(t, "en", true)
	c.beginWrites()
	if code := exitOf(c.cancel); code != -1 {
		t.Fatalf("exited (%d) in the middle of the writes", code)
	}
	if _, err := os.Stat(mark); err != nil {
		t.Fatalf("the mark went before the writes ended: %v", err)
	}
	if code := exitOf(c.endWrites); code != exitCancelled {
		t.Fatalf("exit %d after the writes, want %d", code, exitCancelled)
	}
	if out.Len() != 0 {
		t.Errorf("said %q, but the report changed", out.String())
	}
	if prev, _ := nb.StartReview(time.Now()); prev != "" {
		t.Errorf("the next review sees an unfinished one (%s)", prev)
	}
}

// Writes with no cancellation do not exit; a review that is killed (no
// cancellation) keeps its mark, and the next review records it.
func TestNoCancelKeepsRunUnfinished(t *testing.T) {
	c, nb, _, _ := cancelHarness(t, "en", true)
	c.beginWrites()
	if code := exitOf(c.endWrites); code != -1 {
		t.Fatalf("exited (%d) with no cancellation", code)
	}
	// Killed here: the mark stays.
	if prev, _ := nb.StartReview(time.Now()); prev == "" {
		t.Error("a review killed without a cancellation is not seen as unfinished")
	}
}

// A nil watch (a scheduled review) does nothing.
func TestCancelNilWatch(t *testing.T) {
	var c *reviewCancel
	c.beginWrites()
	c.endWrites()
	c.stop()
}

// The signals listened to are the four of this system's console: on
// Windows Ctrl+C and closing the console, elsewhere Ctrl+C and closing the
// terminal.
func TestCancelSignals(t *testing.T) {
	if len(cancelSignals) != 2 || cancelSignals[0] != os.Interrupt {
		t.Fatalf("cancelSignals = %v", cancelSignals)
	}
}
