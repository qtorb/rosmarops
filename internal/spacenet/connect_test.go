package spacenet

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/kit"
	"github.com/qtorb/rosmarops/internal/state"
)

// testExpiry is a future expires_at for the test servers, always ahead of the
// program's 10-minute poll cap so pollDeadline never trips on wall-clock time.
func testExpiry() string { return time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339) }

// grantedPermission is the non-exported test hook (CTO-14 P3, CTO-15 PR3):
// in-package tests build a usable Permission pointed at a local server (client
// and base), without a real state and without any package-level destination.
func grantedPermission(c *http.Client, base string) Permission {
	return Permission{client: c, base: base}
}

func TestNumberRange(t *testing.T) {
	for i := 0; i < 2000; i++ {
		p := make([]byte, 32)
		rand.Read(p)
		if n := Number(p); n < 10 || n > 99 {
			t.Fatalf("Number out of 10..99: %d", n)
		}
	}
}

// sealNumber must open to the one byte Number, under the contract's key and AAD.
func TestSealNumberRoundTrip(t *testing.T) {
	pickup := make([]byte, 32)
	rand.Read(pickup)
	const hostID = "host_0123456789abcdef"
	sealed, err := sealNumber(pickup, hostID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := b64.DecodeString(sealed)
	if err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(pickup)
	mac := hmac.New(sha256.New, h[:])
	mac.Write([]byte(nLabel))
	gcm, err := newGCM(mac.Sum(nil))
	if err != nil {
		t.Fatal(err)
	}
	ns := gcm.NonceSize()
	pt, err := gcm.Open(nil, raw[:ns], raw[ns:], []byte(nLabel+"|"+hostID))
	if err != nil {
		t.Fatalf("open n_sealed: %v", err)
	}
	if len(pt) != 1 || int(pt[0]) != Number(pickup) {
		t.Fatalf("n_sealed opened to %v, want %d", pt, Number(pickup))
	}
}

// connectServer is a test double for the area: the /connect/start, /result and
// /ack endpoints of the contract, with the browser's confirm simulated inline
// (it seals a known key for the program's e_p).
type connectServer struct {
	t    *testing.T
	key  kit.Secret
	kid  []byte // kit_id, 16 bytes
	cred []byte // device credential, 32 bytes
	mu   sync.Mutex
	// per connect_id state
	ep        []byte // program's e_p (raw)
	hostID    string
	connectID string
	polls     int
	acked     bool
}

func (s *connectServer) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/connect/start", func(w http.ResponseWriter, r *http.Request) {
		var req startRequest
		json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
		if req.V != 1 || req.EP == "" || req.HostID == "" || req.PickupHash == "" || req.NSealed == "" {
			http.Error(w, "bad_request", http.StatusBadRequest)
			return
		}
		ep, err := b64.DecodeString(req.EP)
		if err != nil {
			http.Error(w, "bad_request", http.StatusBadRequest)
			return
		}
		cid := make([]byte, 16)
		rand.Read(cid)
		s.mu.Lock()
		s.ep, s.hostID, s.connectID, s.polls = ep, req.HostID, hex.EncodeToString(cid), 0
		s.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(startResponse{ConnectID: s.connectID, ExpiresAt: testExpiry()})
	})
	mux.HandleFunc("GET /api/v1/connect/{id}/result", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "no bearer", http.StatusUnauthorized)
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if r.PathValue("id") != s.connectID {
			http.Error(w, "unknown", http.StatusNotFound)
			return
		}
		s.polls++
		if s.polls == 1 { // exercise the program's wait loop once
			w.WriteHeader(http.StatusAccepted)
			json.NewEncoder(w).Encode(resultResponse{State: "pending"})
			return
		}
		rr := s.browserConfirm()
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(rr)
	})
	mux.HandleFunc("POST /api/v1/connect/{id}/ack", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.acked = r.PathValue("id") == s.connectID
		s.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

// browserConfirm seals the key for the program's e_p exactly as the area's
// browser does, per the contract.
func (s *connectServer) browserConfirm() resultResponse {
	ebPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		s.t.Fatal(err)
	}
	epPub, err := ecdh.X25519().NewPublicKey(s.ep)
	if err != nil {
		s.t.Fatal(err)
	}
	ikm, err := ebPriv.ECDH(epPub)
	if err != nil {
		s.t.Fatal(err)
	}
	cid, _ := hex.DecodeString(s.connectID)
	k, err := hkdf.Key(sha256.New, ikm, cid, connectLabel, 32)
	if err != nil {
		s.t.Fatal(err)
	}
	blk, _ := aes.NewCipher(k)
	gcm, _ := cipher.NewGCM(blk)
	keyIDraw, _ := hex.DecodeString(kit.KeyID(s.key.Key))
	plain := []byte{1}
	plain = append(plain, s.key.Key...)
	plain = append(plain, s.key.Salt...)
	plain = append(plain, s.kid...)
	plain = append(plain, keyIDraw...)
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	eb := ebPriv.PublicKey().Bytes()
	aad := []byte(connectLabel + "|" + s.connectID + "|" + b64.EncodeToString(s.ep) + "|" + b64.EncodeToString(eb) + "|" + s.hostID)
	ct := gcm.Seal(nil, nonce, plain, aad)
	return resultResponse{
		EB: b64.EncodeToString(eb), Nonce: b64.EncodeToString(nonce),
		Ciphertext: b64.EncodeToString(ct), Credential: b64.EncodeToString(s.cred),
	}
}

func TestConnectProtocol(t *testing.T) {
	// A known space key to seal and recover.
	salt := make([]byte, kit.SaltSize)
	rand.Read(salt)
	sec, err := kit.NewSecret(salt)
	if err != nil {
		t.Fatal(err)
	}
	kid := make([]byte, 16)
	rand.Read(kid)
	cred := make([]byte, 32)
	rand.Read(cred)
	srv := &connectServer{t: t, key: sec, kid: kid, cred: cred}
	ts := httptest.NewServer(srv.handler())
	defer ts.Close()

	const hostID = "host_0123456789abcdef"
	var shownNumber int
	var openedURL string
	m, ack, err := Connect(context.Background(), grantedPermission(ts.Client(), ts.URL), hostID,
		func(n int) { shownNumber = n },
		func(u string) error { openedURL = u; return nil })
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	// P5: the material is returned before the ack. The caller stores it, then
	// acks; the server must not have been acked until we call ack here.
	if srv.acked {
		t.Error("server was acked before the caller stored the material")
	}
	if shownNumber < 10 || shownNumber > 99 {
		t.Errorf("shown number %d", shownNumber)
	}
	if openedURL == "" || !hasPrefix(openedURL, ts.URL+"/connect#v1."+srv.connectID+".") {
		t.Errorf("browser url %q", openedURL)
	}
	if !hmac.Equal(m.Secret.Key, sec.Key) || !hmac.Equal(m.Secret.Salt, sec.Salt) {
		t.Error("recovered key or salt differs")
	}
	if m.KitID != hex.EncodeToString(kid) {
		t.Errorf("kit_id %q, want %q", m.KitID, hex.EncodeToString(kid))
	}
	if !hmac.Equal(m.Credential, cred) {
		t.Error("credential differs")
	}
	if err := ack(context.Background()); err != nil {
		t.Fatalf("ack: %v", err)
	}
	if !srv.acked {
		t.Error("program did not ack after the caller stored the material")
	}
	if srv.polls < 2 {
		t.Errorf("expected the program to poll at least twice (one pending), got %d", srv.polls)
	}
}

// A 410 from the result endpoint is a cancellation.
func TestConnectCancelled(t *testing.T) {
	mux := http.NewServeMux()
	var cid string
	mux.HandleFunc("POST /api/v1/connect/start", func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 16)
		rand.Read(b)
		cid = hex.EncodeToString(b)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(startResponse{ConnectID: cid, ExpiresAt: testExpiry()})
	})
	mux.HandleFunc("GET /api/v1/connect/{id}/result", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"cancelled"}`, http.StatusGone)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	_, _, err := Connect(context.Background(), grantedPermission(ts.Client(), ts.URL), "host_0123456789abcdef", func(int) {}, func(string) error { return nil })
	if err != ErrConnectCancelled {
		t.Fatalf("err = %v, want ErrConnectCancelled", err)
	}
}

// Without the person's permission, Connect refuses before any network: the
// zero Permission (and FromState of a state with SpacePermission false) is
// unusable (CTO-14 P3).
func TestConnectRequiresPermission(t *testing.T) {
	_, _, err := Connect(context.Background(), Permission{}, "host_0123456789abcdef", func(int) {}, func(string) error { return nil })
	if err != ErrNoPermission {
		t.Fatalf("zero Permission: err = %v, want ErrNoPermission", err)
	}
	// FromState loads the state from a path (CTO-15 PR3): a state with the
	// permission false is unusable; one with it true yields a Permission.
	write := func(perm bool) string {
		st, err := state.DefaultState("1.0.0")
		if err != nil {
			t.Fatal(err)
		}
		st.SpacePermission = perm
		p := filepath.Join(t.TempDir(), "state.json")
		if err := state.SaveState(p, st); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, err := FromState(write(false)); err != ErrNoPermission {
		t.Fatalf("FromState(false): err = %v, want ErrNoPermission", err)
	}
	if _, err := FromState(write(true)); err != nil {
		t.Fatalf("FromState(true): err = %v, want nil", err)
	}
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }
