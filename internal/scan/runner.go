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

// The global time limit is proportional to how many projects a run will check
// (CLAUDE.md invariant §6, signed by Albert 2026-11-03, BV-155): 15 seconds per
// project, at least 300 and at most 1200 seconds. Hardcoded, not configurable.
const (
	PerProjectBudget = 15 * time.Second
	MinGlobalTimeout = 300 * time.Second
	MaxGlobalTimeout = 1200 * time.Second
)

// GlobalTimeoutFor is the run's global time limit for the given number of
// projects to check: PerProjectBudget × projects, clamped to
// [MinGlobalTimeout, MaxGlobalTimeout]. A count that could not be established
// (negative) falls back to MinGlobalTimeout. Computed once per run, after the
// projects are found and before they are checked.
func GlobalTimeoutFor(projects int) time.Duration {
	if projects < 0 {
		return MinGlobalTimeout
	}
	d := time.Duration(projects) * PerProjectBudget
	switch {
	case d < MinGlobalTimeout:
		return MinGlobalTimeout
	case d > MaxGlobalTimeout:
		return MaxGlobalTimeout
	default:
		return d
	}
}

// LimitMinutes is the limit shown to the person: the limit in minutes, rounded
// up.
func LimitMinutes(d time.Duration) int {
	return int((d + time.Minute - 1) / time.Minute)
}

// GlobalTimeoutScope is the ScanError scope recorded when the global time limit
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
// done), the steps after it are not started, and Run appends a ScanError with
// scope GlobalTimeoutScope naming the interrupted step. A step that returns
// after the deadline still records what it collected.
//
// A step that panics does not bring the run down (BV-202 S1, CTO-24 §0): the
// panic is recovered on the step's own goroutine, its partial results are
// discarded (record is not called), and Run appends a ScanError scoped to that
// step so the run is fail-secure (at least yellow) and the steps after it still
// run. The panic value never reaches the ScanError — only the step name — so no
// read bytes leak into the report; the detail is logged locally.
//
// Run returns every ScanError it collected (panics in order, then at most one
// global_timeout), or nil when none occurred.
func Run(ctx context.Context, steps []Step) []finding.ScanError {
	var errs []finding.ScanError
	for i, s := range steps {
		if ctx.Err() != nil {
			errs = append(errs, *timeoutError(ctx, "", steps[i:]))
			return errs
		}
		stepStart := time.Now()
		done := make(chan func(), 1)
		panicked := make(chan struct{}, 1)
		go func(s Step) {
			defer func() {
				if r := recover(); r != nil {
					// Log the detail locally (value-free in the report).
					slog.Error("scan: scanner panicked; results discarded",
						"scanner", s.Name, "panic", fmt.Sprint(r))
					panicked <- struct{}{}
				}
			}()
			done <- s.Run(ctx)
		}(s)

		var record func()
		stepPanicked := false
		select {
		case record = <-done:
		case <-panicked:
			stepPanicked = true
		case <-ctx.Done():
			grace := time.NewTimer(abandonGrace)
			select {
			case record = <-done:
				grace.Stop()
			case <-panicked:
				grace.Stop()
				stepPanicked = true
			case <-grace.C:
				slog.Warn("scan: scanner did not stop at the global deadline; results discarded", "scanner", s.Name)
			}
		}
		if stepPanicked {
			errs = append(errs, finding.ScanError{
				Scope:   s.Name,
				Message: "scanner panicked; its results were discarded and the scan continued",
			})
		}
		if record != nil {
			record()
		}
		// Per-step duration (BV-101, decision 5): technical codes only, no
		// catalog text. Logged for every step, including one abandoned at the
		// deadline or panicked (record == nil).
		slog.Info("scan: step done", "step", s.Name, "duration_ms", time.Since(stepStart).Milliseconds())
		if ctx.Err() != nil {
			errs = append(errs, *timeoutError(ctx, s.Name, steps[i+1:]))
			return errs
		}
	}
	return errs
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
	msg := fmt.Sprintf("global scan timeout (%s): interrupted scanner: %s; scanners not run: %s",
		cause, interrupted, notRunList)
	slog.Warn("scan: global timeout", "interrupted", interrupted, "not_run", names)
	return &finding.ScanError{Scope: GlobalTimeoutScope, Message: msg}
}
