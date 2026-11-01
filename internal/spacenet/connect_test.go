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
	"sync"
	"testing"

	"github.com/qtorb/rosmarops/internal/kit"
)

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
		json.NewEncoder(w).Encode(startResponse{ConnectID: s.connectID, ExpiresAt: "2026-11-01T13:00:00Z"})
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

	origBase := baseURL
	baseURL = ts.URL
	t.Cleanup(func() { baseURL = origBase })

	const hostID = "host_0123456789abcdef"
	var shownNumber int
	var openedURL string
	m, err := Connect(context.Background(), ts.Client(), hostID,
		func(n int) { shownNumber = n },
		func(u string) error { openedURL = u; return nil })
	if err != nil {
		t.Fatalf("Connect: %v", err)
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
	if !srv.acked {
		t.Error("program did not ack")
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
		json.NewEncoder(w).Encode(startResponse{ConnectID: cid, ExpiresAt: "2026-11-01T13:00:00Z"})
	})
	mux.HandleFunc("GET /api/v1/connect/{id}/result", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"cancelled"}`, http.StatusGone)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	origBase := baseURL
	baseURL = ts.URL
	t.Cleanup(func() { baseURL = origBase })
	_, err := Connect(context.Background(), ts.Client(), "host_0123456789abcdef", func(int) {}, func(string) error { return nil })
	if err != ErrConnectCancelled {
		t.Fatalf("err = %v, want ErrConnectCancelled", err)
	}
}

func hasPrefix(s, p string) bool { return len(s) >= len(p) && s[:len(p)] == p }
