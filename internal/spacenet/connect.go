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
)

var b64 = base64.RawURLEncoding

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

// start performs POST /api/v1/connect/start and returns the connect_id.
func start(ctx context.Context, c *http.Client, ep ecdhPub, hostID string, pickup []byte) (string, error) {
	nSealed, err := sealNumber(pickup, hostID)
	if err != nil {
		return "", err
	}
	ph := sha256.Sum256(pickup)
	body, err := json.Marshal(startRequest{
		V: 1, EP: b64.EncodeToString(ep), HostID: hostID,
		PickupHash: hex.EncodeToString(ph[:]), NSealed: nSealed,
	})
	if err != nil {
		return "", err
	}
	resp, err := do(ctx, c, http.MethodPost, URL("/api/v1/connect/start"), "", body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("spacenet: connect/start: %s", resp.Status)
	}
	var sr startResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&sr); err != nil {
		return "", fmt.Errorf("spacenet: connect/start response: %w", err)
	}
	if len(sr.ConnectID) != 32 || !isHex(sr.ConnectID) {
		return "", fmt.Errorf("spacenet: connect/start gave a bad connect_id")
	}
	return sr.ConnectID, nil
}

// ecdhPub is an X25519 public key as bytes.
type ecdhPub = []byte

// Connect runs the whole program-side protocol. onNumber is called with the
// number to show; onOpen is called with the browser URL. It returns the key
// and credential once the browser has confirmed, or ErrConnectCancelled /
// ErrConnectExpired.
func Connect(ctx context.Context, c *http.Client, hostID string, onNumber func(int), onOpen func(string) error) (Material, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return Material{}, err
	}
	ep := priv.PublicKey().Bytes()
	pickup := make([]byte, 32)
	if _, err := rand.Read(pickup); err != nil {
		return Material{}, err
	}
	connectID, err := start(ctx, c, ep, hostID, pickup)
	if err != nil {
		return Material{}, err
	}
	if onNumber != nil {
		onNumber(Number(pickup))
	}
	if onOpen != nil {
		u := URL("/connect#v1." + connectID + "." + b64.EncodeToString(ep))
		if err := onOpen(u); err != nil {
			return Material{}, fmt.Errorf("spacenet: open browser: %w", err)
		}
	}
	bearer := b64.EncodeToString(pickup)
	deadline := time.Now().Add(pollFor)
	resultURL := URL("/api/v1/connect/" + connectID + "/result")
	for {
		m, done, err := poll(ctx, c, resultURL, bearer, priv, connectID, ep, hostID)
		if err != nil {
			return Material{}, err
		}
		if done {
			if err := ackConnect(ctx, c, connectID, bearer); err != nil {
				return Material{}, err
			}
			return m, nil
		}
		if time.Now().After(deadline) {
			return Material{}, ErrConnectExpired
		}
		select {
		case <-ctx.Done():
			return Material{}, ctx.Err()
		case <-time.After(pollEvery):
		}
	}
}

// poll does one GET …/result. done is true with the material when the browser
// has confirmed; false means keep waiting.
func poll(ctx context.Context, c *http.Client, url, bearer string, priv *ecdh.PrivateKey, connectID string, ep ecdhPub, hostID string) (m Material, done bool, err error) {
	resp, err := do(ctx, c, http.MethodGet, url, bearer, nil)
	if err != nil {
		return Material{}, false, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusAccepted: // 202 pending
		return Material{}, false, nil
	case http.StatusGone: // 410 cancelled
		return Material{}, false, ErrConnectCancelled
	case http.StatusNotFound: // 404 expired/unknown
		return Material{}, false, ErrConnectExpired
	case http.StatusOK:
		var rr resultResponse
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&rr); err != nil {
			return Material{}, false, fmt.Errorf("spacenet: result response: %w", err)
		}
		m, err := openResult(priv, connectID, ep, hostID, rr)
		if err != nil {
			return Material{}, false, err
		}
		return m, true, nil
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
	cid, err := hex.DecodeString(connectID)
	if err != nil || len(cid) != 16 {
		return Material{}, fmt.Errorf("spacenet: connect_id not 16 bytes")
	}
	key, err := hkdf.Key(sha256.New, ikm, cid, connectLabel, 32)
	if err != nil {
		return Material{}, fmt.Errorf("spacenet: hkdf: %w", err)
	}
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
	cred, err := b64.DecodeString(rr.Credential)
	if err != nil || len(cred) == 0 {
		return Material{}, fmt.Errorf("spacenet: device_credential")
	}
	return Material{Secret: sec, KitID: hex.EncodeToString(kitIDRaw), Credential: cred}, nil
}

func ackConnect(ctx context.Context, c *http.Client, connectID, bearer string) error {
	resp, err := do(ctx, c, http.MethodPost, URL("/api/v1/connect/"+connectID+"/ack"), bearer, nil)
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

func isHex(s string) bool {
	_, err := hex.DecodeString(s)
	return err == nil
}
