package scan

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/finding"
)

// testDeadline stands in for GlobalTimeout: the cap is injected through ctx.
const testDeadline = 200 * time.Millisecond

// testMargin is the "small margin" a run may take past its deadline.
const testMargin = time.Second

func recorder(name string, ran *[]string) func(context.Context) func() {
	return func(context.Context) func() {
		return func() { *ran = append(*ran, name) }
	}
}

// timeoutOf returns the single global_timeout error in errs, failing if there
// is not exactly one error and it is not the timeout.
func timeoutOf(t *testing.T, errs []finding.ScanError) finding.ScanError {
	t.Helper()
	if len(errs) != 1 {
		t.Fatalf("want exactly one error, got %d: %+v", len(errs), errs)
	}
	if errs[0].Scope != GlobalTimeoutScope {
		t.Fatalf("want %q scope, got %q", GlobalTimeoutScope, errs[0].Scope)
	}
	return errs[0]
}

// BV-155: the global limit is 15 s per project, clamped to [300, 1200] s,
// and the minutes shown are the limit rounded up (invariant §6).
func TestGlobalTimeoutFor(t *testing.T) {
	cases := []struct {
		projects int
		wantSec  int
		wantMin  int
	}{
		{0, 300, 5},
		{10, 300, 5},
		{20, 300, 5},
		{21, 315, 6},
		{48, 720, 12},
		{80, 1200, 20},
		{500, 1200, 20},
		{-1, 300, 5}, // a count that could not be established falls back to the minimum
	}
	for _, c := range cases {
		got := GlobalTimeoutFor(c.projects)
		if got != time.Duration(c.wantSec)*time.Second {
			t.Errorf("GlobalTimeoutFor(%d) = %s, want %ds", c.projects, got, c.wantSec)
		}
		if m := LimitMinutes(got); m != c.wantMin {
			t.Errorf("LimitMinutes for %d projects = %d, want %d", c.projects, m, c.wantMin)
		}
	}
}

func TestRun_AllStepsWithinDeadline(t *testing.T) {
	var ran []string
	errs := Run(context.Background(), []Step{
		{Name: "a", Run: recorder("a", &ran)},
		{Name: "b", Run: recorder("b", &ran)},
	})
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %+v", errs)
	}
	if strings.Join(ran, ",") != "a,b" {
		t.Errorf("recorded: %v", ran)
	}
}

// A scanner that sleeps but honours ctx is interrupted at the deadline,
// records what it has, and the scanners after it do not run.
func TestRun_SleepingScannerInterrupted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()

	var ran []string
	sleeper := func(ctx context.Context) func() {
		select {
		case <-ctx.Done():
		case <-time.After(time.Hour):
		}
		return func() { ran = append(ran, "slow") }
	}
	begin := time.Now()
	errs := Run(ctx, []Step{
		{Name: "fast", Run: recorder("fast", &ran)},
		{Name: "slow", Run: sleeper},
		{Name: "after", Run: recorder("after", &ran)},
		{Name: "last", Run: recorder("last", &ran)},
	})
	elapsed := time.Since(begin)

	if elapsed > testDeadline+testMargin {
		t.Errorf("run took %s, cap %s + margin %s", elapsed, testDeadline, testMargin)
	}
	err := timeoutOf(t, errs)
	for _, want := range []string{"interrupted scanner: slow", "scanners not run: after, last"} {
		if !strings.Contains(err.Message, want) {
			t.Errorf("message %q lacks %q", err.Message, want)
		}
	}
	if strings.Join(ran, ",") != "fast,slow" {
		t.Errorf("recorded: %v, want fast,slow", ran)
	}
}

// A scanner that ignores ctx is abandoned after the grace period and its
// results are discarded.
func TestRun_ScannerIgnoringDeadlineAbandoned(t *testing.T) {
	defer func(g time.Duration) { abandonGrace = g }(abandonGrace)
	abandonGrace = 100 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), testDeadline)
	defer cancel()

	var ran []string
	stuck := func(context.Context) func() {
		time.Sleep(time.Minute)
		return func() { ran = append(ran, "stuck") }
	}
	begin := time.Now()
	errs := Run(ctx, []Step{
		{Name: "stuck", Run: stuck},
		{Name: "after", Run: recorder("after", &ran)},
	})
	elapsed := time.Since(begin)

	if elapsed > testDeadline+abandonGrace+testMargin {
		t.Errorf("run took %s", elapsed)
	}
	err := timeoutOf(t, errs)
	if !strings.Contains(err.Message, "interrupted scanner: stuck") ||
		!strings.Contains(err.Message, "scanners not run: after") {
		t.Fatalf("error: %+v", err)
	}
	if len(ran) != 0 {
		t.Errorf("abandoned or skipped scanner recorded: %v", ran)
	}
}

func TestRun_DeadlineAlreadyPassed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()

	var ran []string
	errs := Run(ctx, []Step{{Name: "a", Run: recorder("a", &ran)}})
	err := timeoutOf(t, errs)
	if !strings.Contains(err.Message, "interrupted scanner: none; scanners not run: a") {
		t.Fatalf("error: %+v", err)
	}
	if len(ran) != 0 {
		t.Errorf("recorded: %v", ran)
	}
}

// TestPanicInReaderIsContained (BV-202 S1): a scanner that panics does not
// bring the run down — the panic is recovered, a ScanError scoped to that
// scanner is returned so the run is fail-secure, the step's partial results
// are discarded, and the scanners after it still run. The panic value never
// appears in the ScanError (no read bytes leak into the report).
func TestPanicInReaderIsContained(t *testing.T) {
	const secret = "sk-proj-PANIC-secret-0000"

	// Capture the run log: the panic value must not reach it either (BV-202 C2).
	var logBuf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(prev)

	var ran []string
	boom := func(context.Context) func() {
		panic("reader exploded with " + secret)
	}
	errs := Run(context.Background(), []Step{
		{Name: "before", Run: recorder("before", &ran)},
		{Name: "boom", Run: boom},
		{Name: "after", Run: recorder("after", &ran)},
	})
	if len(errs) != 1 {
		t.Fatalf("want exactly one error, got %d: %+v", len(errs), errs)
	}
	if errs[0].Scope != "boom" {
		t.Errorf("scope = %q, want %q", errs[0].Scope, "boom")
	}
	if strings.Contains(errs[0].Message, secret) || strings.Contains(errs[0].Message, "exploded") {
		t.Errorf("panic detail leaked into error: %q", errs[0].Message)
	}
	if log := logBuf.String(); strings.Contains(log, secret) || strings.Contains(log, "exploded") {
		t.Errorf("panic value leaked into the run log: %q", log)
	}
	if strings.Join(ran, ",") != "before,after" {
		t.Errorf("recorded: %v, want before,after (panicked step discarded, later steps still ran)", ran)
	}
}
