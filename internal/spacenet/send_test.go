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
