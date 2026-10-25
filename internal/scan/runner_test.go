package scan

import (
	"context"
	"strings"
	"testing"
	"time"
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

func TestGlobalTimeoutValue(t *testing.T) {
	if GlobalTimeout != 300*time.Second {
		t.Fatalf("GlobalTimeout = %s, invariant §6 requires 300s", GlobalTimeout)
	}
}

func TestRun_AllStepsWithinDeadline(t *testing.T) {
	var ran []string
	err := Run(context.Background(), []Step{
		{Name: "a", Run: recorder("a", &ran)},
		{Name: "b", Run: recorder("b", &ran)},
	})
	if err != nil {
		t.Fatalf("unexpected error: %+v", err)
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
	err := Run(ctx, []Step{
		{Name: "fast", Run: recorder("fast", &ran)},
		{Name: "slow", Run: sleeper},
		{Name: "after", Run: recorder("after", &ran)},
		{Name: "last", Run: recorder("last", &ran)},
	})
	elapsed := time.Since(begin)

	if elapsed > testDeadline+testMargin {
		t.Errorf("run took %s, cap %s + margin %s", elapsed, testDeadline, testMargin)
	}
	if err == nil {
		t.Fatal("no global_timeout error")
	}
	if err.Scope != GlobalTimeoutScope {
		t.Errorf("scope = %q, want %q", err.Scope, GlobalTimeoutScope)
	}
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
	err := Run(ctx, []Step{
		{Name: "stuck", Run: stuck},
		{Name: "after", Run: recorder("after", &ran)},
	})
	elapsed := time.Since(begin)

	if elapsed > testDeadline+abandonGrace+testMargin {
		t.Errorf("run took %s", elapsed)
	}
	if err == nil || !strings.Contains(err.Message, "interrupted scanner: stuck") ||
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
	err := Run(ctx, []Step{{Name: "a", Run: recorder("a", &ran)}})
	if err == nil || !strings.Contains(err.Message, "interrupted scanner: none; scanners not run: a") {
		t.Fatalf("error: %+v", err)
	}
	if len(ran) != 0 {
		t.Errorf("recorded: %v", ran)
	}
}
