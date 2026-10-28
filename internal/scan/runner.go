// Package scan orchestrates the scanners of a run under the global deadline.
package scan

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/finding"
)

// GlobalTimeout is the hard cap on a whole run (CLAUDE.md invariant §6).
// Hardcoded, not configurable.
const GlobalTimeout = 300 * time.Second

// GlobalTimeoutScope is the ScanError scope recorded when GlobalTimeout
// expires before every scanner has finished.
const GlobalTimeoutScope = "global_timeout"

// abandonGrace is how long Run waits, after the deadline, for the scanner in
// progress to return. Scanners honour ctx and their subprocesses are killed
// with it, so they return almost at once; one that does not is abandoned
// and its results are discarded. Tests in this package shorten it.
var abandonGrace = 5 * time.Second

// Step is one scanner. Run does the scan under ctx and returns a function
// that records its results; that function is called on the caller's
// goroutine, only if Run returned within the deadline plus abandonGrace.
// Run must not change state shared with the caller itself.
type Step struct {
	Name string
	Run  func(ctx context.Context) (record func())
}

// Run runs steps in order under ctx, whose deadline is the global one. When
// the deadline expires, the step in progress is interrupted (its ctx is
// done), the steps after it are not started, and Run returns a ScanError
// with scope GlobalTimeoutScope naming the interrupted step. A step that
// returns after the deadline still records what it collected. Otherwise Run
// returns nil.
func Run(ctx context.Context, steps []Step) *finding.ScanError {
	for i, s := range steps {
		if ctx.Err() != nil {
			return timeoutError(ctx, "", steps[i:])
		}
		stepStart := time.Now()
		done := make(chan func(), 1)
		go func(s Step) { done <- s.Run(ctx) }(s)

		var record func()
		select {
		case record = <-done:
		case <-ctx.Done():
			grace := time.NewTimer(abandonGrace)
			select {
			case record = <-done:
				grace.Stop()
			case <-grace.C:
				slog.Warn("scan: scanner did not stop at the global deadline; results discarded", "scanner", s.Name)
			}
		}
		if record != nil {
			record()
		}
		// Per-step duration (BV-101, decision 5): technical codes only, no
		// catalog text. Logged for every step, including one abandoned at the
		// deadline (record == nil).
		slog.Info("scan: step done", "step", s.Name, "duration_ms", time.Since(stepStart).Milliseconds())
		if ctx.Err() != nil {
			return timeoutError(ctx, s.Name, steps[i+1:])
		}
	}
	return nil
}

// timeoutError builds the global_timeout ScanError: the interrupted step
// (empty when the deadline passed between steps) and the steps not run.
func timeoutError(ctx context.Context, interrupted string, notRun []Step) *finding.ScanError {
	names := make([]string, 0, len(notRun))
	for _, s := range notRun {
		names = append(names, s.Name)
	}
	notRunList := strings.Join(names, ", ")
	if notRunList == "" {
		notRunList = "none"
	}
	if interrupted == "" {
		interrupted = "none"
	}
	cause := "deadline exceeded"
	if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		cause = ctx.Err().Error()
	}
	msg := fmt.Sprintf("global scan timeout (%s, %s): interrupted scanner: %s; scanners not run: %s",
		GlobalTimeout, cause, interrupted, notRunList)
	slog.Warn("scan: global timeout", "interrupted", interrupted, "not_run", names)
	return &finding.ScanError{Scope: GlobalTimeoutScope, Message: msg}
}
