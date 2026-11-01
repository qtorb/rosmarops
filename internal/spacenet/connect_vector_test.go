package spacenet

// E1 (CTO-14): a shared connection test vector, so the program and the area
// (rosmarops-app) cannot drift apart on the connect protocol without a test
// failing on one side. The vector lives at <repo>/testdata/cross/connect-v1.json;
// BA copies it into rosmarops-app (BA-21), so its name must not change once the
// PR is open. This Go test regenerates it (ROSMAROPS_REGEN_CROSS=1) and, every
// run, recomputes every derived value from the stored inputs with the real
// program code and checks it against the stored expected values — the same
// shape as the kit vector in tests/cross.

import (
	"bytes"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/qtorb/rosmarops/internal/kit"
)

// crossVector is the on-disk shape of connect-v1.json. Binary fields are
// base64url without padding (b64); connect_id, kit_id and key_id are lowercase
// hex; host_id keeps its host_ prefix — exactly as each travels in the protocol.
type crossVector struct {
	Description  string `json:"description"`
	V            int    `json:"v"`
	EPPrivate    string `json:"e_p_private"`
	EPPublic     string `json:"e_p_public"`
	EBPrivate    string `json:"e_b_private"`
	EBPublic     string `json:"e_b_public"`
	PickupSecret string `json:"pickup_secret"`
	ConnectID    string `json:"connect_id"`
	HostID       string `json:"host_id"`
	Key          string `json:"key"`
	Salt         string `json:"salt"`
	KitID        string `json:"kit_id"`
	KeyID        string `json:"key_id"`
	Number       int    `json:"number"`
	NSealedNonce string `json:"n_sealed_nonce"`
	NSealed      string `json:"n_sealed"`
	ResultNonce  string `json:"result_nonce"`
	Credential   string `json:"device_credential"`
	AAD          string `json:"additional_data"`
	Ciphertext   string `json:"ciphertext"`
}

func crossVectorPath(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "testdata", "cross", "connect-v1.json")
}

func TestCrossConnectVector(t *testing.T) {
	path := crossVectorPath(t)
	if os.Getenv("ROSMAROPS_REGEN_CROSS") == "1" {
		writeCrossConnectVector(t, path)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read vector (regenerate with ROSMAROPS_REGEN_CROSS=1): %v", err)
	}
	var v crossVector
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("parse vector: %v", err)
	}

	epPriv := mustPriv(t, v.EPPrivate)
	ebPriv := mustPriv(t, v.EBPrivate)
	epPub := mustB64(t, v.EPPublic)
	ebPub := mustB64(t, v.EBPublic)
	pickup := mustB64(t, v.PickupSecret)

	// Public keys match their private halves.
	if !bytes.Equal(epPriv.PublicKey().Bytes(), epPub) {
		t.Error("e_p_public does not match e_p_private")
	}
	if !bytes.Equal(ebPriv.PublicKey().Bytes(), ebPub) {
		t.Error("e_b_public does not match e_b_private")
	}

	// connect_id is 16 bytes, lowercase hex (CTO-14 P8).
	if len(v.ConnectID) != 32 || !isLowerHex(v.ConnectID) {
		t.Errorf("connect_id %q is not 32 lowercase hex chars", v.ConnectID)
	}

	// N is Number(pickup).
	if got := Number(pickup); got != v.Number {
		t.Errorf("number = %d, want %d", got, v.Number)
	}

	// n_sealed opens to the one byte N under the real key and AAD, and
	// re-sealing with the stored nonce reproduces it byte for byte.
	nSealed := mustB64(t, v.NSealed)
	nNonce := mustB64(t, v.NSealedNonce)
	nKey := macSum(sha256Sum(pickup), nLabel)
	nGCM, err := newGCM(nKey)
	if err != nil {
		t.Fatal(err)
	}
	nAAD := []byte(nLabel + "|" + v.HostID)
	if ns := nGCM.NonceSize(); len(nSealed) < ns || !bytes.Equal(nSealed[:ns], nNonce) {
		t.Error("n_sealed does not start with n_sealed_nonce")
	} else {
		pt, err := nGCM.Open(nil, nSealed[:ns], nSealed[ns:], nAAD)
		if err != nil || len(pt) != 1 || int(pt[0]) != v.Number {
			t.Errorf("n_sealed opened to %v (%v), want [%d]", pt, err, v.Number)
		}
		if reSealed := append(append([]byte(nil), nNonce...), nGCM.Seal(nil, nNonce, []byte{byte(v.Number)}, nAAD)...); !bytes.Equal(reSealed, nSealed) {
			t.Error("re-sealing N with the stored nonce did not reproduce n_sealed")
		}
	}

	// Both sides derive the same HKDF key.
	ikmP, err := epPriv.ECDH(mustPub(t, ebPub))
	if err != nil {
		t.Fatal(err)
	}
	ikmB, err := ebPriv.ECDH(mustPub(t, epPub))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ikmP, ikmB) {
		t.Fatal("the two sides do not share the same X25519 secret")
	}
	cid, err := hex.DecodeString(v.ConnectID)
	if err != nil {
		t.Fatal(err)
	}
	key, err := hkdf.Key(sha256.New, ikmP, cid, connectLabel, 32)
	if err != nil {
		t.Fatal(err)
	}

	// The additional data is exactly the contract's string.
	wantAAD := connectLabel + "|" + v.ConnectID + "|" + v.EPPublic + "|" + v.EBPublic + "|" + v.HostID
	if v.AAD != wantAAD {
		t.Errorf("additional_data mismatch:\n got %q\nwant %q", v.AAD, wantAAD)
	}

	// Re-sealing the 97-byte plaintext with the stored nonce reproduces the
	// ciphertext.
	plain := connectPlaintext(mustB64(t, v.Key), mustB64(t, v.Salt), mustHex(t, v.KitID), mustHex(t, v.KeyID))
	if len(plain) != plaintextSz {
		t.Fatalf("plaintext is %d bytes, want %d", len(plain), plaintextSz)
	}
	gcm, err := newGCM(key)
	if err != nil {
		t.Fatal(err)
	}
	ct := gcm.Seal(nil, mustB64(t, v.ResultNonce), plain, []byte(v.AAD))
	if !bytes.Equal(ct, mustB64(t, v.Ciphertext)) {
		t.Error("re-sealing the plaintext did not reproduce the stored ciphertext")
	}

	// And the real program path opens it: openResult recovers the key, salt
	// and kit_id and passes the key_id check.
	rr := resultResponse{EB: v.EBPublic, Nonce: v.ResultNonce, Ciphertext: v.Ciphertext, Credential: v.Credential}
	m, err := openResult(epPriv, v.ConnectID, epPub, v.HostID, rr)
	if err != nil {
		t.Fatalf("openResult on the vector: %v", err)
	}
	if !bytes.Equal(m.Secret.Key, mustB64(t, v.Key)) || !bytes.Equal(m.Secret.Salt, mustB64(t, v.Salt)) {
		t.Error("openResult recovered a different key or salt")
	}
	if m.KitID != v.KitID {
		t.Errorf("openResult kit_id = %q, want %q", m.KitID, v.KitID)
	}
	if kit.KeyID(m.Secret.Key) != v.KeyID {
		t.Errorf("key_id = %q, want %q", kit.KeyID(m.Secret.Key), v.KeyID)
	}
}

// connectPlaintext builds the 97-byte sealed content: version, key, salt,
// kit_id, key_id.
func connectPlaintext(key, salt, kitID, keyID []byte) []byte {
	out := make([]byte, 0, plaintextSz)
	out = append(out, 1)
	out = append(out, key...)
	out = append(out, salt...)
	out = append(out, kitID...)
	out = append(out, keyID...)
	return out
}

func sha256Sum(b []byte) []byte { h := sha256.Sum256(b); return h[:] }

func macSum(key []byte, label string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(label))
	return m.Sum(nil)
}

func mustB64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := b64.DecodeString(s)
	if err != nil {
		t.Fatalf("decode base64url %q: %v", s, err)
	}
	return b
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("decode hex %q: %v", s, err)
	}
	return b
}

func mustPriv(t *testing.T, s string) *ecdh.PrivateKey {
	t.Helper()
	p, err := ecdh.X25519().NewPrivateKey(mustB64(t, s))
	if err != nil {
		t.Fatalf("x25519 private key: %v", err)
	}
	return p
}

func mustPub(t *testing.T, b []byte) *ecdh.PublicKey {
	t.Helper()
	p, err := ecdh.X25519().NewPublicKey(b)
	if err != nil {
		t.Fatalf("x25519 public key: %v", err)
	}
	return p
}

// writeCrossConnectVector regenerates connect-v1.json from fresh random inputs,
// computing every value with the same code the protocol uses.
func writeCrossConnectVector(t *testing.T, path string) {
	t.Helper()
	epPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ebPriv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	pickup := randBytes(t, 32)
	cid := randBytes(t, 16)
	connectID := hex.EncodeToString(cid)
	hostID := "host_" + hex.EncodeToString(randBytes(t, 8))
	key := randBytes(t, kit.KeySize)
	salt := randBytes(t, kit.SaltSize)
	kitID := randBytes(t, 16)
	keyIDRaw, err := hex.DecodeString(kit.KeyID(key))
	if err != nil {
		t.Fatal(err)
	}
	cred := randBytes(t, credentialSize)

	n := Number(pickup)
	nNonce := randBytes(t, 12)
	nKey := macSum(sha256Sum(pickup), nLabel)
	nGCM, err := newGCM(nKey)
	if err != nil {
		t.Fatal(err)
	}
	nAAD := []byte(nLabel + "|" + hostID)
	nSealed := append(append([]byte(nil), nNonce...), nGCM.Seal(nil, nNonce, []byte{byte(n)}, nAAD)...)

	ikm, err := epPriv.ECDH(ebPriv.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	k, err := hkdf.Key(sha256.New, ikm, cid, connectLabel, 32)
	if err != nil {
		t.Fatal(err)
	}
	epPub := epPriv.PublicKey().Bytes()
	ebPub := ebPriv.PublicKey().Bytes()
	aad := connectLabel + "|" + connectID + "|" + b64.EncodeToString(epPub) + "|" + b64.EncodeToString(ebPub) + "|" + hostID
	resultNonce := randBytes(t, 12)
	plain := connectPlaintext(key, salt, kitID, keyIDRaw)
	gcm, err := newGCM(k)
	if err != nil {
		t.Fatal(err)
	}
	ct := gcm.Seal(nil, resultNonce, plain, []byte(aad))

	v := crossVector{
		Description:  "RosmarOps connect protocol v1 shared test vector (CTO-14 E1). The program (Go) and the area (rosmarops-app) each reproduce it and must agree. Test values only; no worth outside the tests.",
		V:            1,
		EPPrivate:    b64.EncodeToString(epPriv.Bytes()),
		EPPublic:     b64.EncodeToString(epPub),
		EBPrivate:    b64.EncodeToString(ebPriv.Bytes()),
		EBPublic:     b64.EncodeToString(ebPub),
		PickupSecret: b64.EncodeToString(pickup),
		ConnectID:    connectID,
		HostID:       hostID,
		Key:          b64.EncodeToString(key),
		Salt:         b64.EncodeToString(salt),
		KitID:        hex.EncodeToString(kitID),
		KeyID:        kit.KeyID(key),
		Number:       n,
		NSealedNonce: b64.EncodeToString(nNonce),
		NSealed:      b64.EncodeToString(nSealed),
		ResultNonce:  b64.EncodeToString(resultNonce),
		Credential:   b64.EncodeToString(cred),
		AAD:          aad,
		Ciphertext:   b64.EncodeToString(ct),
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	out = append(out, '\n')
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("regenerated %s", path)
}

func randBytes(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}
