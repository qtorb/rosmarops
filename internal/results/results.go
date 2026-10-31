// Package results builds the closed package rosmarops-results v1 (BV-119,
// AP-17 §2, CTO-10 points 5 and 9): the space-view content sealed so only the
// holder of the kit key can read it. Two levels, both AES-256-GCM: a random
// 32-byte data key per package encrypts the content, and that data key is
// wrapped under the kit key. The cleartext header is the additional
// authenticated data of both, so changing it, the kit_id or the key_id breaks
// decryption. Nothing here reaches the network, and the package carries no
// secret in clear (CLAUDE.md invariants 1 and 3).
package results

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Format and Version identify the package (header.format, header.version).
const (
	Format  = "rosmarops-results"
	Version = 1
)

// dataKeySize is the per-package random data key (AES-256).
const dataKeySize = 32

// Header is the cleartext, canonically-ordered head of the package; it is the
// GCM additional data of both the content and the wrapped key.
type Header struct {
	Format           string `json:"format"`
	Version          int    `json:"version"`
	KitID            string `json:"kit_id"`
	KeyID            string `json:"key_id"`
	HostID           string `json:"host_id"`
	CreatedAt        string `json:"created_at"`
	RosmaropsVersion string `json:"rosmarops_version"`
}

// Package is the file written to disk: the header in clear and the three
// base64 blobs. wrapped_key carries its own nonce (prepended); nonce is the
// content's.
type Package struct {
	Header     Header `json:"header"`
	WrappedKey string `json:"wrapped_key"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// aad is the additional authenticated data: the header as canonical JSON with
// the keys in alphabetical order (CTO-11 B2). It encodes a map, not the
// struct, because encoding/json sorts a map's keys and escapes < > & and
// U+2028/U+2029 exactly as the browser's canonicalJSON does (rosmarops-app
// public/space.mjs); a package the program seals then opens in the browser.
// The cleartext header in the file keeps the struct's order; only these bytes
// must match on both sides.
func (h Header) aad() ([]byte, error) {
	b, err := json.Marshal(map[string]any{
		"format":            h.Format,
		"version":           h.Version,
		"kit_id":            h.KitID,
		"key_id":            h.KeyID,
		"host_id":           h.HostID,
		"created_at":        h.CreatedAt,
		"rosmarops_version": h.RosmaropsVersion,
	})
	if err != nil {
		return nil, fmt.Errorf("results: encode header: %w", err)
	}
	return b, nil
}

// gcmFor returns AES-256-GCM for a 32-byte key.
func gcmFor(key []byte) (cipher.AEAD, error) {
	if len(key) != dataKeySize {
		return nil, fmt.Errorf("results: key is %d bytes, want %d", len(key), dataKeySize)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("results: cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

// randBytes returns n cryptographically-random bytes.
func randBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, fmt.Errorf("results: random: %w", err)
	}
	return b, nil
}

// Seal builds the package: a fresh data key encrypts content, and the kit key
// wraps the data key; the header is the additional data of both.
func Seal(kitKey []byte, h Header, content []byte) (Package, error) {
	h.Format, h.Version = Format, Version
	aad, err := h.aad()
	if err != nil {
		return Package{}, err
	}
	kitGCM, err := gcmFor(kitKey)
	if err != nil {
		return Package{}, err
	}
	dataKey, err := randBytes(dataKeySize)
	if err != nil {
		return Package{}, err
	}
	dataGCM, err := gcmFor(dataKey)
	if err != nil {
		return Package{}, err
	}

	contentNonce, err := randBytes(dataGCM.NonceSize())
	if err != nil {
		return Package{}, err
	}
	ciphertext := dataGCM.Seal(nil, contentNonce, content, aad)

	wrapNonce, err := randBytes(kitGCM.NonceSize())
	if err != nil {
		return Package{}, err
	}
	// wrapped_key is wrapNonce || GCM(dataKey): its own nonce travels with it.
	wrapped := kitGCM.Seal(append([]byte(nil), wrapNonce...), wrapNonce, dataKey, aad)

	enc := base64.StdEncoding.EncodeToString
	return Package{
		Header:     h,
		WrappedKey: enc(wrapped),
		Nonce:      enc(contentNonce),
		Ciphertext: enc(ciphertext),
	}, nil
}

// Open decrypts a package with the kit key and returns the content. It fails
// if the header, the wrapped key or the ciphertext was changed (the header is
// the additional data of both GCM openings).
func Open(kitKey []byte, p Package) ([]byte, error) {
	aad, err := p.Header.aad()
	if err != nil {
		return nil, err
	}
	kitGCM, err := gcmFor(kitKey)
	if err != nil {
		return nil, err
	}
	dec := base64.StdEncoding.DecodeString
	wrapped, err1 := dec(p.WrappedKey)
	contentNonce, err2 := dec(p.Nonce)
	ciphertext, err3 := dec(p.Ciphertext)
	if err := firstErr(err1, err2, err3); err != nil {
		return nil, fmt.Errorf("results: decode package: %w", err)
	}
	ns := kitGCM.NonceSize()
	if len(wrapped) < ns {
		return nil, errors.New("results: wrapped key too short")
	}
	dataKey, err := kitGCM.Open(nil, wrapped[:ns], wrapped[ns:], aad)
	if err != nil {
		return nil, fmt.Errorf("results: unwrap data key: %w", err)
	}
	dataGCM, err := gcmFor(dataKey)
	if err != nil {
		return nil, err
	}
	if len(contentNonce) != dataGCM.NonceSize() {
		return nil, errors.New("results: bad content nonce")
	}
	content, err := dataGCM.Open(nil, contentNonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("results: decrypt content: %w", err)
	}
	return content, nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// Marshal encodes a package as indented JSON (the file written to disk).
func Marshal(p Package) ([]byte, error) {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("results: marshal package: %w", err)
	}
	return b, nil
}

// Parse decodes a package file.
func Parse(raw []byte) (Package, error) {
	var p Package
	if err := json.Unmarshal(raw, &p); err != nil {
		return Package{}, fmt.Errorf("results: parse package: %w", err)
	}
	return p, nil
}
