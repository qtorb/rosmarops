package spacenet

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/kit"
)

// The connect protocol, program side (BV-137, CTO-13 §1, contract
// qtorb/rosmarops-app docs/api-v1.md). The program generates an ephemeral
// X25519 pair and a pickup secret, asks the server to start a connection,
// opens the browser so the person confirms there, then polls for the result:
// the space key sealed for the program's ephemeral public key, and this
// computer's send credential. Nothing here runs until the permission is in the
// state; the caller enforces that.

const (
	// b64 is base64url without padding, the contract's encoding for binary.
	// connect_id stays hex and host_id keeps its host_ prefix.
	sasLabel     = "rosmarops-connect-sas"
	nLabel       = "rosmarops-connect-n-v1"
	connectLabel = "rosmarops-connect-v1"

	pollEvery   = 2 * time.Second
	pollFor     = 10 * time.Minute
	plaintextSz = 1 + kit.KeySize + kit.SaltSize + 16 + 16 // 97: version, key, salt, kit_id, key_id

	// credentialSize is the exact length of this computer's send credential
	// (CTO-14 P8): the area mints 32 bytes, and anything else is rejected.
	credentialSize = 32
)

// b64 is base64url without padding, in strict mode (CTO-14 P8): it rejects any
// encoding with non-canonical trailing bits, so a value only ever has one valid
// form on the wire.
var b64 = base64.RawURLEncoding.Strict()

// Material is what a completed connection leaves on this computer: the space
// key and its salt, the kit_id, and the send credential (all to be stored with
// DPAPI in the kit blob by the caller).
type Material struct {
	Secret     kit.Secret
	KitID      string
	Credential []byte
}

// ErrConnectCancelled is the server's 410: a wrong number or a rejection.
var ErrConnectCancelled = errors.New("spacenet: the connection was cancelled in the browser")

// ErrConnectExpired is a 404 or the 10-minute deadline: no confirmation in time.
var ErrConnectExpired = errors.New("spacenet: the connection was not confirmed in time")

// Number is the connection number the program shows and the browser asks the
// person to pick (CTO-13 H2): 10..99, derived from the pickup secret.
func Number(pickup []byte) int {
	mac := hmac.New(sha256.New, pickup)
	mac.Write([]byte(sasLabel))
	sum := mac.Sum(nil)
	return 10 + int(binary.BigEndian.Uint32(sum[:4])%90)
}

// sealNumber returns n_sealed: the one byte Number(pickup), AES-256-GCM
// encrypted under a key derived from the pickup secret and bound to host_id, so
// that whoever holds only the connect_id cannot read it.
func sealNumber(pickup []byte, hostID string) (string, error) {
	h := sha256.Sum256(pickup)
	mac := hmac.New(sha256.New, h[:])
	mac.Write([]byte(nLabel))
	key := mac.Sum(nil) // 32 bytes
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	aad := []byte(nLabel + "|" + hostID)
	ct := gcm.Seal(nil, nonce, []byte{byte(Number(pickup))}, aad)
	return b64.EncodeToString(append(nonce, ct...)), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(blk)
}

type startRequest struct {
	V          int    `json:"v"`
	EP         string `json:"e_p"`
	HostID     string `json:"host_id"`
	PickupHash string `json:"pickup_hash"`
	NSealed    string `json:"n_sealed"`
}

type startResponse struct {
	ConnectID string `json:"connect_id"`
	ExpiresAt string `json:"expires_at"`
}

type resultResponse struct {
	State      string `json:"state"`
	Error      string `json:"error"`
	EB         string `json:"e_b"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
	Credential string `json:"device_credential"`
}

// start performs POST /api/v1/connect/start and returns the connect_id and the
// server's expiry (zero when it gave none or an unparseable one).
func start(ctx context.Context, c *http.Client, base string, ep ecdhPub, hostID string, pickup []byte) (string, time.Time, error) {
	nSealed, err := sealNumber(pickup, hostID)
	if err != nil {
		return "", time.Time{}, err
	}
	ph := sha256.Sum256(pickup)
	body, err := json.Marshal(startRequest{
		V: 1, EP: b64.EncodeToString(ep), HostID: hostID,
		PickupHash: hex.EncodeToString(ph[:]), NSealed: nSealed,
	})
	if err != nil {
		return "", time.Time{}, err
	}
	resp, err := do(ctx, c, http.MethodPost, base+"/api/v1/connect/start", "", body)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", time.Time{}, fmt.Errorf("spacenet: connect/start: %s", resp.Status)
	}
	var sr startResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&sr); err != nil {
		return "", time.Time{}, fmt.Errorf("spacenet: connect/start response: %w", err)
	}
	// connect_id is 16 bytes as 32 lowercase hex characters (CTO-14 P8): it
	// goes verbatim into the HKDF salt and the additional data, so only its
	// lowercase form is accepted — an uppercase one would derive a different
	// key on each side.
	if len(sr.ConnectID) != 32 || !isLowerHex(sr.ConnectID) {
		return "", time.Time{}, fmt.Errorf("spacenet: connect/start gave a bad connect_id")
	}
	// expires_at bounds how long we keep asking (CTO-14 P8); an absent or
	// unparseable value leaves the local 10-minute cap as the only bound.
	var expiresAt time.Time
	if t, err := time.Parse(time.RFC3339, sr.ExpiresAt); err == nil {
		expiresAt = t
	}
	return sr.ConnectID, expiresAt, nil
}

// ecdhPub is an X25519 public key as bytes.
type ecdhPub = []byte

// Connect runs the whole program-side protocol under the person's recorded
// permission (CTO-14 P3): it builds the one hardened client itself from perm,
// so no caller passes a client in. onNumber is called with the number to show;
// onOpen with the browser URL. On success it returns the recovered Material and
// an ack function the caller runs ONLY after it has stored the material
// (CTO-14 P5): the area deletes the connection on ack, so storing first means a
// failed save (e.g. DPAPI) never loses the credential — the person can retry
// until the connection expires. It returns ErrConnectCancelled,
// ErrConnectExpired, or ErrNoPermission.
func Connect(ctx context.Context, perm Permission, hostID string, onNumber func(int), onOpen func(string) error) (Material, func(context.Context) error, error) {
	c, err := perm.httpClient()
	if err != nil {
		return Material{}, nil, err
	}
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Material{}, nil, err
	}
	ep := priv.PublicKey().Bytes()
	pickup := make([]byte, 32)
	if _, err := rand.Read(pickup); err != nil {
		return Material{}, nil, err
	}
	// The bearer below is a string copy of pickup; the ack closure uses that,
	// so the pickup bytes can be wiped as soon as Connect returns (CTO-14 P8).
	defer zero(pickup)
	connectID, expiresAt, err := start(ctx, c, perm.base, ep, hostID, pickup)
	if err != nil {
		return Material{}, nil, err
	}
	if onNumber != nil {
		onNumber(Number(pickup))
	}
	if onOpen != nil {
		u := perm.url("/connect#v1." + connectID + "." + b64.EncodeToString(ep))
		if err := onOpen(u); err != nil {
			return Material{}, nil, fmt.Errorf("spacenet: open browser: %w", err)
		}
	}
	bearer := b64.EncodeToString(pickup)
	deadline := pollDeadline(time.Now(), expiresAt)
	resultURL := perm.url("/api/v1/connect/" + connectID + "/result")
	for {
		m, done, err := poll(ctx, c, resultURL, bearer, priv, connectID, ep, hostID)
		if err != nil {
			return Material{}, nil, err
		}
		if done {
			base := perm.base
			ack := func(ctx context.Context) error { return ackConnect(ctx, c, base, connectID, bearer) }
			return m, ack, nil
		}
		if !time.Now().Before(deadline) {
			return Material{}, nil, ErrConnectExpired
		}
		select {
		case <-ctx.Done():
			return Material{}, nil, ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

// pollDeadline is how long Connect keeps asking: the local 10-minute cap, or
// the server's expires_at when that is sooner (CTO-14 P8).
func pollDeadline(now, expiresAt time.Time) time.Time {
	base := now.Add(pollFor)
	if !expiresAt.IsZero() && expiresAt.Before(base) {
		return expiresAt
	}
	return base
}

// poll does one GET …/result. done is true with the material when the browser
// has confirmed. A transient failure — a network error, a 429, or a 5xx — is
// not returned as an error: poll reports "keep waiting" (done false, err nil)
// so Connect keeps asking until the deadline (CTO-14 P5). Only a cancellation
// (410), an expiry (404), or a malformed/undecryptable 200 is terminal.
func poll(ctx context.Context, c *http.Client, url, bearer string, priv *ecdh.PrivateKey, connectID string, ep ecdhPub, hostID string) (m Material, done bool, err error) {
	resp, err := do(ctx, c, http.MethodGet, url, bearer, nil)
	if err != nil {
		return Material{}, false, nil // transient network error: keep waiting
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusAccepted: // 202 pending
		return Material{}, false, nil
	case resp.StatusCode == http.StatusGone: // 410 cancelled
		return Material{}, false, ErrConnectCancelled
	case resp.StatusCode == http.StatusNotFound: // 404 expired/unknown
		return Material{}, false, ErrConnectExpired
	case resp.StatusCode == http.StatusOK:
		var rr resultResponse
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&rr); err != nil {
			return Material{}, false, fmt.Errorf("spacenet: result response: %w", err)
		}
		m, err := openResult(priv, connectID, ep, hostID, rr)
		if err != nil {
			return Material{}, false, err
		}
		return m, true, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		// 429 and 5xx are transient (CTO-14 P5): keep waiting until expiry.
		return Material{}, false, nil
	default:
		return Material{}, false, fmt.Errorf("spacenet: connect/result: %s", resp.Status)
	}
}

// openResult derives the shared key, decrypts the sealed space key, and checks
// the key_id against the key (CTO-13 §1 step 7, contract §2).
func openResult(priv *ecdh.PrivateKey, connectID string, ep ecdhPub, hostID string, rr resultResponse) (Material, error) {
	ebBytes, err := b64.DecodeString(rr.EB)
	if err != nil {
		return Material{}, fmt.Errorf("spacenet: e_b: %w", err)
	}
	ebPub, err := ecdh.X25519().NewPublicKey(ebBytes)
	if err != nil {
		return Material{}, fmt.Errorf("spacenet: e_b key: %w", err)
	}
	ikm, err := priv.ECDH(ebPub)
	if err != nil {
		return Material{}, fmt.Errorf("spacenet: ecdh: %w", err)
	}
	defer zero(ikm) // the shared secret never outlives this function (CTO-14 P8)
	cid, err := hex.DecodeString(connectID)
	if err != nil || len(cid) != 16 {
		return Material{}, fmt.Errorf("spacenet: connect_id not 16 bytes")
	}
	key, err := hkdf.Key(sha256.New, ikm, cid, connectLabel, 32)
	if err != nil {
		return Material{}, fmt.Errorf("spacenet: hkdf: %w", err)
	}
	defer zero(key) // so does the derived key
	gcm, err := newGCM(key)
	if err != nil {
		return Material{}, err
	}
	nonce, err := b64.DecodeString(rr.Nonce)
	if err != nil || len(nonce) != gcm.NonceSize() {
		return Material{}, fmt.Errorf("spacenet: nonce")
	}
	ct, err := b64.DecodeString(rr.Ciphertext)
	if err != nil {
		return Material{}, fmt.Errorf("spacenet: ciphertext: %w", err)
	}
	aad := []byte(connectLabel + "|" + connectID + "|" + b64.EncodeToString(ep) + "|" + rr.EB + "|" + hostID)
	plain, err := gcm.Open(nil, nonce, ct, aad)
	if err != nil {
		return Material{}, fmt.Errorf("spacenet: open sealed key: %w", err)
	}
	defer zero(plain) // the 97-byte content is copied out below, then wiped (CTO-14 P8)
	if len(plain) != plaintextSz || plain[0] != 1 {
		return Material{}, fmt.Errorf("spacenet: sealed key is %d bytes, want %d", len(plain), plaintextSz)
	}
	off := 1
	keyBytes := plain[off : off+kit.KeySize]
	off += kit.KeySize
	salt := plain[off : off+kit.SaltSize]
	off += kit.SaltSize
	kitIDRaw := plain[off : off+16]
	off += 16
	keyIDRaw := plain[off : off+16]
	sec := kit.Secret{Key: append([]byte(nil), keyBytes...), Salt: append([]byte(nil), salt...)}
	if !hmac.Equal([]byte(kit.KeyID(sec.Key)), []byte(hex.EncodeToString(keyIDRaw))) {
		return Material{}, fmt.Errorf("spacenet: key_id does not match the key")
	}
	// The send credential is exactly 32 bytes (CTO-14 P8); anything else is a
	// malformed result.
	cred, err := b64.DecodeString(rr.Credential)
	if err != nil || len(cred) != credentialSize {
		return Material{}, fmt.Errorf("spacenet: device_credential")
	}
	return Material{Secret: sec, KitID: hex.EncodeToString(kitIDRaw), Credential: cred}, nil
}

func ackConnect(ctx context.Context, c *http.Client, base, connectID, bearer string) error {
	resp, err := do(ctx, c, http.MethodPost, base+"/api/v1/connect/"+connectID+"/ack", bearer, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("spacenet: connect/ack: %s", resp.Status)
	}
	return nil
}

// do issues one request with the fixed client, an optional Bearer token, and an
// optional JSON body. The caller closes the response body.
func do(ctx context.Context, c *http.Client, method, url, bearer string, body []byte) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return c.Do(req)
}

// isLowerHex reports whether s is valid hex with no uppercase letters.
func isLowerHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil && s == strings.ToLower(s)
}

// zero wipes a byte slice holding key material (CTO-14 P8).
func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
