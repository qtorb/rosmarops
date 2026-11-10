package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/problems"
	"github.com/qtorb/rosmarops/internal/spacenet"
)

// withLocalProblemsServer stubs the send seam with the given status and records
// the bodies it would send. It must not import net/http: the trust test
// TestNoNetwork_OnlySpacenetImportsNetwork forbids any package but spacenet from
// bringing in the network, test imports included. The real wire path (host-pin,
// POST /api/v1/problems, no credential or cookie) is proved in internal/spacenet
// and tests/trust, not here.
func withLocalProblemsServer(t *testing.T, status int) *[][]byte {
	t.Helper()
	var got [][]byte
	orig := sendProblemsFn
	t.Cleanup(func() { sendProblemsFn = orig })
	sendProblemsFn = func(_ context.Context, _ spacenet.ProblemsConsent, body []byte) (int, error) {
		got = append(got, append([]byte(nil), body...))
		return status, nil
	}
	return &got
}

// testConsent is a consent value for direct submitProblems calls in tests; the
// seam above ignores it, and the real consent discipline is tested in
// internal/spacenet and tests/trust.
func testConsent() spacenet.ProblemsConsent { return spacenet.ConsentFromSendButton() }

func seedNotebook(t *testing.T, dir string) {
	t.Helper()
	nb := problems.New(dir)
	if _, err := nb.Record(
		problems.Entry{Code: problems.SpaceRejected, Step: problems.StepSpaceSend, HTTPStatus: 409, Day: "2026-11-09", Count: 3},
		problems.Entry{Code: problems.ConfigUnreadable, Step: problems.StepAgents, Agent: "cline", Day: "2026-11-09", Count: 1},
	); err != nil {
		t.Fatal(err)
	}
}

func TestSubmitProblemsDeliversAndMarksSent(t *testing.T) {
	dir := t.TempDir()
	seedNotebook(t, dir)
	bodies := withLocalProblemsServer(t, 204)

	res := submitProblems(context.Background(), testConsent(), dir, "en", "0.10.3", nil, nil, time.Now())
	if !res.Delivered || res.Sent != 2 {
		t.Fatalf("want delivered with 2 sent, got %+v", res)
	}
	if len(*bodies) != 1 {
		t.Fatalf("want 1 request, got %d", len(*bodies))
	}
	// The body validates against problems.v1 and leaks no path/name.
	if err := problems.Validate((*bodies)[0]); err != nil {
		t.Fatalf("sent body must validate: %v", err)
	}
	body := string((*bodies)[0])
	// No path separators or file extensions (the codes/steps are closed tokens;
	// the dedicated §7 trust test plants real paths, names and keys).
	for _, bad := range []string{"/", "\\", ".json", ".toml"} {
		if strings.Contains(body, bad) {
			t.Fatalf("sent body leaks %q: %s", bad, body)
		}
	}
	// Nothing left unsent.
	left, _ := problems.New(dir).Unsent(problems.MaxPerSend)
	if len(left) != 0 {
		t.Fatalf("want 0 unsent after send, got %d", len(left))
	}
	// Sends log has a sent line for the problems report.
	log := readSendsLog(t, dir)
	if !strings.Contains(log, "sent rosmarops-problems.v1") {
		t.Fatalf("sends log missing problems sent line:\n%s", log)
	}
}

// TestSubmitProblems400MarksRejected is CTO-32 N2: a terminal 400 marks the
// sent lines rejected, so the next review does not ask about them again.
func TestSubmitProblems400MarksRejected(t *testing.T) {
	dir := t.TempDir()
	seedNotebook(t, dir)
	withLocalProblemsServer(t, 400)

	res := submitProblems(context.Background(), testConsent(), dir, "en", "0.10.3", nil, nil, time.Now())
	if res.Delivered || res.Sent != 0 || !res.Attempted {
		t.Fatalf("want attempted, not delivered, got %+v", res)
	}
	// Nothing is offered next time: the lines are rejected, not unsent.
	left, _ := problems.New(dir).Unsent(problems.MaxPerSend)
	if len(left) != 0 {
		t.Fatalf("after a 400, the lines must not be offered again, got %d unsent", len(left))
	}
	// A following review tries no send: there is nothing to send.
	next := submitProblems(context.Background(), testConsent(), dir, "en", "0.10.3", nil, nil, time.Now())
	if next.Attempted {
		t.Fatalf("the next review must not attempt a send, got %+v", next)
	}
	log := readSendsLog(t, dir)
	if !strings.Contains(log, "bad_request rosmarops-problems.v1") {
		t.Fatalf("sends log missing bad_request line:\n%s", log)
	}
}

// TestSubmitProblems413MarksRejected is CTO-32 N2 for the other terminal
// status: a 413 from the server also stops those lines being offered again.
func TestSubmitProblems413MarksRejected(t *testing.T) {
	dir := t.TempDir()
	seedNotebook(t, dir)
	withLocalProblemsServer(t, 413)

	submitProblems(context.Background(), testConsent(), dir, "en", "0.10.3", nil, nil, time.Now())
	left, _ := problems.New(dir).Unsent(problems.MaxPerSend)
	if len(left) != 0 {
		t.Fatalf("after a 413, the lines must not be offered again, got %d unsent", len(left))
	}
}

// TestSubmitProblemsKeepsOnDeferred: a 429 (or 5xx, or a network error) is not
// terminal — the lines stay unsent and are offered again next time.
func TestSubmitProblemsKeepsOnDeferred(t *testing.T) {
	dir := t.TempDir()
	seedNotebook(t, dir)
	withLocalProblemsServer(t, 429)

	res := submitProblems(context.Background(), testConsent(), dir, "en", "0.10.3", nil, nil, time.Now())
	if res.Delivered || res.Sent != 0 || !res.Attempted {
		t.Fatalf("want attempted, not delivered, got %+v", res)
	}
	left, _ := problems.New(dir).Unsent(problems.MaxPerSend)
	if len(left) != 2 {
		t.Fatalf("want 2 unsent kept after 429, got %d", len(left))
	}
}

func TestSubmitProblemsNothingToSend(t *testing.T) {
	dir := t.TempDir()
	res := submitProblems(context.Background(), testConsent(), dir, "en", "0.10.3", nil, nil, time.Now())
	if res.Attempted || res.Delivered || res.Sent != 0 {
		t.Fatalf("empty notebook: want no attempt, got %+v", res)
	}
}

func readSendsLog(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, SendsLogName))
	if err != nil {
		t.Fatalf("read sends log: %v", err)
	}
	return string(raw)
}
