package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/problems"
)

// withLocalProblemsServer points the send seam at a local test server (standing
// in for BA-84's endpoint until it ships) and returns the bodies it received.
func withLocalProblemsServer(t *testing.T, status int) *[][]byte {
	t.Helper()
	var got [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/problems" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if a := r.Header.Get("Authorization"); a != "" {
			t.Errorf("problems send must carry no credential, got Authorization %q", a)
		}
		if len(r.Cookies()) != 0 {
			t.Errorf("problems send must carry no cookie, got %v", r.Cookies())
		}
		b := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(b)
		got = append(got, b)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)

	orig := sendProblemsFn
	t.Cleanup(func() { sendProblemsFn = orig })
	sendProblemsFn = func(ctx context.Context, body []byte) (int, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL+"/api/v1/problems", strings.NewReader(string(body)))
		if err != nil {
			return 0, err
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := srv.Client().Do(req)
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		return resp.StatusCode, nil
	}
	return &got
}

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
	bodies := withLocalProblemsServer(t, http.StatusNoContent)

	res := submitProblems(context.Background(), dir, "en", "0.10.3", nil, nil, time.Now())
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

func TestSubmitProblemsKeepsOnReject(t *testing.T) {
	dir := t.TempDir()
	seedNotebook(t, dir)
	withLocalProblemsServer(t, http.StatusBadRequest)

	res := submitProblems(context.Background(), dir, "en", "0.10.3", nil, nil, time.Now())
	if res.Delivered || res.Sent != 0 || !res.Attempted {
		t.Fatalf("want attempted, not delivered, got %+v", res)
	}
	left, _ := problems.New(dir).Unsent(problems.MaxPerSend)
	if len(left) != 2 {
		t.Fatalf("want 2 unsent kept, got %d", len(left))
	}
	log := readSendsLog(t, dir)
	if !strings.Contains(log, "bad_request rosmarops-problems.v1") {
		t.Fatalf("sends log missing bad_request line:\n%s", log)
	}
}

func TestSubmitProblemsNothingToSend(t *testing.T) {
	dir := t.TempDir()
	res := submitProblems(context.Background(), dir, "en", "0.10.3", nil, nil, time.Now())
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
