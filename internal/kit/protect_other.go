//go:build !windows

package kit

// On macOS and Linux the kit key file is protected by its mode (0600 in a
// 0700 folder) and, on disk, by FileVault or the system's disk encryption
// (CTO-10 point 2): the record is stored as is.

func protect(plain []byte) ([]byte, error) { return plain, nil }

func unprotect(data []byte) ([]byte, error) { return data, nil }
