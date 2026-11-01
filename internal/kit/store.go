package kit

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// Stored is what the kit key file keeps on this computer: the kit_id, the
// secret, and the recovery code envelope, so that a new phrase can copy it
// unchanged without the code.
type Stored struct {
	KitID  string
	Secret Secret
	Code   Envelope
	// SpaceCredential is this computer's send credential for the connected
	// space (BV-137): kept in the same DPAPI blob as the key, never in the
	// clear and never in the state. Empty when the computer is not connected
	// or the kit came from a plain `rosmarops kit import`.
	SpaceCredential []byte
}

// record is Stored as JSON, before the platform protection. Version 2 adds
// space_credential (BV-137); a version-1 file (no credential) still loads.
type record struct {
	Version         int      `json:"version"`
	KitID           string   `json:"kit_id"`
	Key             string   `json:"key"`
	Salt            string   `json:"salt"`
	Code            Envelope `json:"recovery_code_envelope"`
	SpaceCredential string   `json:"space_credential,omitempty"`
}

// StorePath is the kit key file: ~/.rosmarops/kit.dpapi on Windows,
// ~/.rosmarops/kit.key on macOS and Linux (CLAUDE.md invariant 2).
func StorePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	name := "kit.key"
	if runtime.GOOS == "windows" {
		name = "kit.dpapi"
	}
	return filepath.Join(home, ".rosmarops", name), nil
}

// file is the kit file of s with its code envelope and an empty phrase
// envelope, for Rewrap: it is how new-phrase gets the code envelope without
// the code.
func (s Stored) file() File {
	return File{Format: Format, Version: Version, KitID: s.KitID, KeyID: KeyID(s.Secret.Key), Envelopes: []Envelope{{Type: TypePhrase}, s.Code}}
}

// NewPhrase is the kit file of s with a new phrase envelope and its code
// envelope copied unchanged: same kit_id, same key_id.
func (s Stored) NewPhrase(phrase string) (File, error) {
	return Rewrap(s.file(), s.Secret, phrase)
}

// FromFile is what to keep on this computer for f, opened as sec.
func FromFile(f File, sec Secret) (Stored, error) {
	code, err := f.envelope(TypeRecoveryCode)
	if err != nil {
		return Stored{}, err
	}
	return Stored{KitID: f.KitID, Secret: sec, Code: code}, nil
}

// Save writes s to path (StorePath) protected for this user: DPAPI on
// Windows, mode 0600 in a 0700 folder elsewhere. The write is atomic: a temp
// file in the same folder, fsync, rename.
func Save(path string, s Stored) error {
	b64 := base64.StdEncoding.EncodeToString
	rec := record{Version: 2, KitID: s.KitID, Key: b64(s.Secret.Key), Salt: b64(s.Secret.Salt), Code: s.Code}
	if len(s.SpaceCredential) > 0 {
		rec.SpaceCredential = b64(s.SpaceCredential)
	}
	plain, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encode kit key file: %w", err)
	}
	data, err := protect(plain)
	if err != nil {
		return fmt.Errorf("protect kit key file: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("restrict %s: %w", dir, err)
		}
	}
	tmp, err := os.CreateTemp(dir, ".kit-*.tmp") // mode 0600
	if err != nil {
		return fmt.Errorf("create temp kit key file in %s: %w", dir, err)
	}
	name := tmp.Name()
	defer os.Remove(name) // a no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp kit key file: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("fsync temp kit key file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp kit key file: %w", err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("rename temp kit key file to %s: %w", path, err)
	}
	return nil
}

// Load reads the kit key file at path. A missing file is an error wrapping
// fs.ErrNotExist.
func Load(path string) (Stored, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Stored{}, fmt.Errorf("read kit key file: %w", err)
	}
	plain, err := unprotect(data)
	if err != nil {
		return Stored{}, fmt.Errorf("unprotect kit key file: %w", err)
	}
	var r record
	if err := json.Unmarshal(plain, &r); err != nil || (r.Version != 1 && r.Version != 2) {
		return Stored{}, fmt.Errorf("kit key file %s is not a version 1 or 2 record", path)
	}
	dec := base64.StdEncoding.DecodeString
	key, err1 := dec(r.Key)
	salt, err2 := dec(r.Salt)
	if err1 != nil || err2 != nil || len(key) != KeySize || len(salt) != SaltSize || len(r.KitID) != 32 {
		return Stored{}, fmt.Errorf("kit key file %s is malformed", path)
	}
	st := Stored{KitID: r.KitID, Secret: Secret{Key: key, Salt: salt}, Code: r.Code}
	if r.SpaceCredential != "" {
		cred, err := dec(r.SpaceCredential)
		if err != nil {
			return Stored{}, fmt.Errorf("kit key file %s has a malformed space credential", path)
		}
		st.SpaceCredential = cred
	}
	return st, nil
}
