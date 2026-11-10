package spacenet

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/results"
	"github.com/qtorb/rosmarops/internal/state"
)

// CTO-13 §3 test 6 (CTO-15 PR3), unit: the real Send transmits only the sealed
// package. Markers seeded in the scanned config and secrets (here, in the
// package's plaintext content) never appear in the body it sends nor in its
// headers — the content is encrypted and the header carries only ids.
func TestSendTransmitsNoPlaintextMarker(t *testing.T) {
	const marker = "MARKER-7a9f-do-not-leak"

	salt := make([]byte, kit.SaltSize)
	rand.Read(salt)
	sec, err := kit.NewSecret(salt)
	if err != nil {
		t.Fatal(err)
	}
	kidRaw := make([]byte, 16)
	rand.Read(kidRaw)
	hostID := "host_" + hex.EncodeToString(make([]byte, 8))
	content := []byte(`{"from_config":"` + marker + `","from_secret":"` + marker + `"}`)
	pkg, err := results.Seal(sec.Key, results.Header{
		Format: "rosmarops-results", Version: 1,
		KitID: hex.EncodeToString(kidRaw), KeyID: kit.KeyID(sec.Key), HostID: hostID,
		CreatedAt: "2026-11-01T00:00:00Z", RosmaropsVersion: "1.0.0",
	}, content)
	if err != nil {
		t.Fatal(err)
	}
	body, err := results.Marshal(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte(marker)) {
		t.Fatal("the sealed package itself carries the marker in clear")
	}

	var gotBody []byte
	var gotHeaders http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"x"}`))
	}))
	defer ts.Close()

	cred := make([]byte, credentialSize)
	rand.Read(cred)
	status, err := Send(context.Background(), grantedPermission(ts.Client(), ts.URL), hostID, cred, body)
	if err != nil || status != http.StatusCreated {
		t.Fatalf("Send: status %d, err %v", status, err)
	}
	if !bytes.Equal(gotBody, body) {
		t.Error("Send did not transmit the package body verbatim")
	}
	if bytes.Contains(gotBody, []byte(marker)) {
		t.Error("a marker reached the transmitted body")
	}
	for k, vs := range gotHeaders {
		if strings.Contains(k, marker) {
			t.Errorf("a marker reached a header name: %q", k)
		}
		for _, v := range vs {
			if strings.Contains(v, marker) {
				t.Errorf("a marker reached header %q: %q", k, v)
			}
		}
	}
}

// TestSendProblems_PathAndNoCredential is CTO-32 C2(a): the problem send is a
// POST to /api/v1/problems with no Authorization and no Cookie. It points the
// problems client at a local server (the production host-pin is unchanged and
// proved elsewhere) and fails if any of the four facts changes.
func TestSendProblems_PathAndNoCredential(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	var gotCookies int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotCookies = len(r.Cookies())
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	orig := problemsClient
	problemsClient = func() (*http.Client, string) { return ts.Client(), ts.URL }
	defer func() { problemsClient = orig }()

	status, err := SendProblems(context.Background(), ConsentFromSendButton(), []byte(`{"format":"problems.v1"}`))
	if err != nil || status != http.StatusNoContent {
		t.Fatalf("send: status=%d err=%v", status, err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/problems" {
		t.Errorf("path = %q, want /api/v1/problems", gotPath)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want none", gotAuth)
	}
	if gotCookies != 0 {
		t.Errorf("cookies = %d, want 0", gotCookies)
	}
}

// TestSendProblems_RefusesWithoutConsent is CTO-32 C1: the zero consent is
// unusable, so SendProblems opens no connection without it.
func TestSendProblems_RefusesWithoutConsent(t *testing.T) {
	called := false
	orig := problemsClient
	problemsClient = func() (*http.Client, string) { called = true; return Client(), prodBaseURL }
	defer func() { problemsClient = orig }()

	status, err := SendProblems(context.Background(), ProblemsConsent{}, []byte(`{}`))
	if err != ErrNoProblemsConsent || status != 0 {
		t.Fatalf("want ErrNoProblemsConsent and status 0, got status=%d err=%v", status, err)
	}
	if called {
		t.Error("the client must not be built without consent")
	}
}

// TestConsentFromState is CTO-32 C1: consent comes only from a state whose
// checkbox is set.
func TestConsentFromState(t *testing.T) {
	dir := t.TempDir()
	stPath := dir + "/state.json"
	st, err := state.DefaultState("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	if err := state.SaveState(stPath, st); err != nil {
		t.Fatal(err)
	}
	if _, err := ConsentFromState(stPath); err != ErrNoProblemsConsent {
		t.Fatalf("checkbox off: want ErrNoProblemsConsent, got %v", err)
	}
	st.SendProblemsWithoutAsking = true
	if err := state.SaveState(stPath, st); err != nil {
		t.Fatal(err)
	}
	if _, err := ConsentFromState(stPath); err != nil {
		t.Fatalf("checkbox on: want consent, got %v", err)
	}
}
