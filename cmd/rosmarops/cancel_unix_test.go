//go:build !windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/problems"
)

// BV-311 T3, with real signals: SIGINT (Ctrl+C) and SIGHUP (closing the
// terminal) each cancel a review through watchCancel.
func TestCancelOnSignal(t *testing.T) {
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGHUP} {
		dir := t.TempDir()
		nb := problems.New(dir)
		if _, err := nb.StartReview(time.Now()); err != nil {
			t.Fatal(err)
		}
		cat, _ := catalog.Load("en")
		exited := make(chan int, 1)
		prev := cancelExit
		cancelExit = func(code int) { exited <- code; select {} }
		var out bytes.Buffer
		// Its handler stays blocked in the fake exit, as the process would end.
		watchCancel(nb.EndReview, &out, cat)
		if err := syscall.Kill(os.Getpid(), sig); err != nil {
			t.Fatal(err)
		}
		select {
		case code := <-exited:
			if code != exitCancelled {
				t.Errorf("%v: exit %d", sig, code)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%v: no cancellation", sig)
		}
		mark := filepath.Join(dir, problems.RunMarkName)
		if _, err := os.Stat(mark); !os.IsNotExist(err) {
			t.Errorf("%v: the mark is still there", sig)
		}
		if out.String() != "Review cancelled. The previous report has not changed.\n" {
			t.Errorf("%v: console %q", sig, out.String())
		}
		cancelExit = prev
	}
}
