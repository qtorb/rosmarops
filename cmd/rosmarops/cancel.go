package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// exitCancelled is the exit code of a review the person cancelled (Ctrl+C,
// or closing the console or terminal): the conventional 128 + SIGINT.
const exitCancelled = 130

// cancelExit ends the process after a cancellation; tests replace it.
var cancelExit = os.Exit

// reviewCancel lets a person stop a review with Ctrl+C or by closing the
// console or terminal (cancelSignals) without it counting as a review that
// did not finish: the in-progress mark is cleared, so the next review records
// no run_unfinished.
//
// Before the review starts writing its results (the graph, the report, the
// memory, the problem notebook and the state), a cancellation stops at once:
// nothing of this review is written, the previous report stays as it was,
// and with a console the person is told so. Once the writes have begun they
// are never cut in half: the cancellation waits until they end, and then
// stops, saying nothing, since the report has changed.
type reviewCancel struct {
	clearMark func() error
	out       io.Writer // the console; nil with none
	cat       catalog.Catalog

	mu      sync.Mutex
	writing bool // the results are being written
	written bool // the results were written
	pending bool // a cancellation arrived while writing
	ch      chan os.Signal
}

// watchCancel starts listening for the cancellation signals of this system.
// stop ends it. A nil *reviewCancel (a scheduled review, which keeps its
// behaviour) does nothing.
func watchCancel(clearMark func() error, out io.Writer, cat catalog.Catalog) *reviewCancel {
	c := &reviewCancel{clearMark: clearMark, out: out, cat: cat, ch: make(chan os.Signal, 1)}
	signal.Notify(c.ch, cancelSignals...)
	go func() {
		if _, ok := <-c.ch; ok {
			c.cancel()
		}
	}()
	return c
}

// stop stops listening: from here a signal ends the program as it always
// did, with the review already finished and its mark cleared.
func (c *reviewCancel) stop() {
	if c == nil {
		return
	}
	signal.Stop(c.ch)
	close(c.ch)
}

// cancel is a cancellation: at once before the writes, after them while
// they run.
func (c *reviewCancel) cancel() {
	c.mu.Lock()
	if c.writing {
		c.pending = true
		c.mu.Unlock()
		return
	}
	c.finish() // the lock is kept: nothing else of the review runs
}

// beginWrites marks the start of the writes no cancellation cuts.
func (c *reviewCancel) beginWrites() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.writing = true
	c.mu.Unlock()
}

// endWrites marks their end, and ends a cancellation that waited for it.
func (c *reviewCancel) endWrites() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.writing, c.written = false, true
	if !c.pending {
		c.mu.Unlock()
		return
	}
	c.finish()
}

// finish clears the in-progress mark, tells a person at the console that the
// previous report did not change (only when this review wrote nothing), and
// exits. Called with the lock held.
func (c *reviewCancel) finish() {
	_ = c.clearMark()
	if c.out != nil && !c.written {
		fmt.Fprintln(c.out, c.cat.Text("console.run.cancelled", nil))
	}
	cancelExit(exitCancelled)
}
