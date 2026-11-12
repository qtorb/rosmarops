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

// DeadlineFor is the global deadline of a review whose project search began
// at searchStart and found projects (invariant 6, CTO-38 G2): the limit counts
// from the start of the search, so the search's own time is within it.
func DeadlineFor(searchStart time.Time, projects int) time.Time {
	return searchStart.Add(GlobalTimeoutFor(projects))
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

// RecoverStep runs fn and, if it panics, contains it the same way Run contains
// a panicking scanner (BV-202 C1, CTO-28): it logs the step name and the
// panic's TYPE only — never its value (invariant 3, BV-202 C2) — and returns a
// value-free ScanError scoped to the step. It returns nil when fn does not
// panic. The graph build runs outside Run (cmd/rosmarops builds it after the
// scanners finish), so that call site wraps buildGraph in this to stay
// fail-secure: a panic there leaves the graph unbuilt (E10) as a visible
// scope-"graph" error, instead of bringing the program down with no report.
func RecoverStep(name string, fn func()) (se *finding.ScanError) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("scan: step panicked; results discarded", "step", name, "panic_type", fmt.Sprintf("%T", r))
			se = &finding.ScanError{Scope: name, Message: "panicked; its results were discarded"}
		}
	}()
	fn()
	return nil
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
		panicked := make(chan finding.ScanError, 1)
		go func(s Step) {
			// The same recover as the out-of-Run graph build (RecoverStep): it
			// logs the step and the panic's type only, never its value.
			var rec func()
			if se := RecoverStep(s.Name, func() { rec = s.Run(ctx) }); se != nil {
				panicked <- *se
				return
			}
			done <- rec
		}(s)

		var record func()
		select {
		case record = <-done:
		case pe := <-panicked:
			errs = append(errs, pe)
		case <-ctx.Done():
			grace := time.NewTimer(abandonGrace)
			select {
			case record = <-done:
				grace.Stop()
			case pe := <-panicked:
				grace.Stop()
				errs = append(errs, pe)
			case <-grace.C:
				slog.Warn("scan: scanner did not stop at the global deadline; results discarded", "scanner", s.Name)
			}
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
