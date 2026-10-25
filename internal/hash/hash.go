// Package hash provides SHA-256 and HMAC-SHA256 helpers used throughout RosmarOps.
// All hashes are returned as lowercase hex strings (64 characters).
// No network I/O occurs in this package — all inputs are local files or
// in-memory bytes, consistent with the no-network invariant.
package hash

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// File computes the SHA-256 of the file at path and returns a lowercase
// 64-character hex string. The file is read in streaming fashion so large
// files do not need to be fully loaded into memory.
func File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Bytes computes the SHA-256 of b and returns a lowercase 64-character hex string.
func Bytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Keyed computes HMAC-SHA256 of data under key and returns a lowercase
// 64-character hex string. Used for salted fingerprints, so that a hash of a
// secret cannot be reversed by hashing guesses without the machine-local key.
func Keyed(key, data []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return hex.EncodeToString(m.Sum(nil))
}

// Self computes the SHA-256 of the running RosmarOps binary (os.Executable).
// Used by the startup integrity check and `rosmarops baseline`.
func Self() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	h, err := File(exe)
	if err != nil {
		return "", fmt.Errorf("hash self: %w", err)
	}
	return h, nil
}
