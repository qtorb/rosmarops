//go:build !windows

package main

import (
	"io"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
)

func esCat() (catalog.Catalog, error) { return catalog.Load("es") }

// TestAskProblemTerminalTimeout is CTO-32 C3: the terminal question ends as
// «Not now» (asked, nothing to send) when there is no answer within the cap.
func TestAskProblemTerminalTimeout(t *testing.T) {
	ask := problemAsk{cat: enCat(t), summary: "the check ran out of time", preview: "{}"}
	// A reader that never returns (an open pipe nobody writes to) models a
	// terminal with nobody answering.
	pr, pw := io.Pipe()
	defer pw.Close()

	done := make(chan problemAnswer, 1)
	go func() {
		ans, asked := askProblemTerminal(ask, pr, io.Discard, 50*time.Millisecond)
		if !asked {
			t.Error("timeout should still count as asked (Not now), not «no surface»")
		}
		done <- ans
	}()
	select {
	case ans := <-done:
		if ans.send || ans.remember {
			t.Errorf("timeout must be «Not now»: %+v", ans)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("askProblemTerminal did not return after its timeout")
	}
}

// TestTerminalYesIsLanguageSpecific is CTO-32 C4.4: «s» sends in Spanish, «y» in
// English; anything else is «Not now».
func TestTerminalYesIsLanguageSpecific(t *testing.T) {
	en := enCat(t)
	es, err := esCat()
	if err != nil {
		t.Fatal(err)
	}
	if !yes(en, "y") || yes(en, "s") {
		t.Error("English should accept y, not s")
	}
	if !yes(es, "s") || yes(es, "y") {
		t.Error("Spanish should accept s, not y")
	}
	if yes(en, "maybe") || yes(es, "") {
		t.Error("any other answer is Not now")
	}
}
