// Package kit is the user kit (BV-118; vigil CTO-10, «Lo que Albert
// firmaría» points 1–5, 7, 8 and 10; decision 2026-10-29, área de clientes,
// §3.1 and its amendment of 2026-10-30): a 256-bit key generated on this
// computer and the user salt (state.json's local_salt, D-MD1), wrapped in the
// rosmarops-kit file once under the secret phrase and once under the
// recovery code, and kept on each computer in the kit key file.
//
// Nothing here uses the network. The key, the salt, the phrase and the code
// are never logged, and never reach the report, the state, the graph or the
// export for your AI.
package kit

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/qtorb/rosmarops/internal/schemas"
	"github.com/qtorb/rosmarops/internal/schemaval"
)

// The rosmarops-kit v1 format (CTO-10 §3).
const (
	Format  = "rosmarops-kit"
	Version = 1
	KDF     = "pbkdf2-sha256"

	TypePhrase       = "phrase"
	TypeRecoveryCode = "recovery_code"

	// PhraseIterations and CodeIterations are the PBKDF2-SHA256 iterations
	// of each envelope (CTO-10 point 4). They are written in the file and
	// bound by the AAD, so a later version can raise them.
	PhraseIterations = 600000
	CodeIterations   = 100000

	// KeySize is the kit key (AES-256); SaltSize, the user salt.
	KeySize  = 32
	SaltSize = 32

	// MinPhraseLen is the shortest own phrase, in characters (CTO-10 point 7).
	MinPhraseLen = 16

	// CodeLen is the recovery code's length: 24 Crockford base32 characters,
	// 120 bits.
	CodeLen = 24

	envelopeSaltSize = 16
	nonceSize        = 12
	plainVersion     = 1
	keyIDLabel       = "rosmarops-key-id"
)

// MessageKeys are the catalog codes the kit commands print (BV-118, point
// 5); the tests check them against the catalog.
var MessageKeys = []string{
	"kit.import_prompt", "kit.import_wrong", "kit.imported", "kit.new_phrase_done", "kit.new_phrase_intro", "kit.no_kit",
	"kit.phrase_again", "kit.phrase_generated", "kit.phrase_mismatch", "kit.phrase_prompt", "kit.phrase_too_short",
	"kit.recovery_check", "kit.recovery_check_failed", "kit.recovery_code", "kit.saved",
}

// ErrWrongSecret is the phrase or code that does not open a kit file.
var ErrWrongSecret = errors.New("kit: the phrase or recovery code does not open this kit file")

// Secret is what a kit file locks: the kit key and the user salt.
type Secret struct {
	Key  []byte
	Salt []byte
}

// File is a rosmarops-kit v1 file (schemas/kit.v1.schema.json).
type File struct {
	Format    string     `json:"format"`
	Version   int        `json:"version"`
	KitID     string     `json:"kit_id"`
	KeyID     string     `json:"key_id"`
	Envelopes []Envelope `json:"envelopes"`
}

// Envelope is the secret wrapped under one password: the phrase or the code.
type Envelope struct {
	Type       string `json:"type"`
	KDF        string `json:"kdf"`
	Iterations int    `json:"iterations"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
}

// NewSecret is a fresh kit key from crypto/rand with the given user salt.
func NewSecret(userSalt []byte) (Secret, error) {
	if len(userSalt) != SaltSize {
		return Secret{}, fmt.Errorf("kit: user salt is %d bytes, want %d", len(userSalt), SaltSize)
	}
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return Secret{}, fmt.Errorf("kit: generate key: %w", err)
	}
	return Secret{Key: key, Salt: append([]byte(nil), userSalt...)}, nil
}

// NewKitID is 16 random bytes, hex.
func NewKitID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("kit: generate kit_id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// KeyID is HMAC-SHA256(key, "rosmarops-key-id"), its first 16 bytes, hex.
func KeyID(key []byte) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(keyIDLabel))
	return hex.EncodeToString(m.Sum(nil)[:16])
}

// ExportName is the kit file's name in the reports directory.
func ExportName(kitID string) string { return "rosmarops-kit-" + kitID[:8] + ".json" }

// crockford is Crockford's base32 alphabet.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NewRecoveryCode is 120 random bits as 24 Crockford base32 characters.
func NewRecoveryCode() (string, error) {
	b := make([]byte, CodeLen*5/8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("kit: generate recovery code: %w", err)
	}
	var sb strings.Builder
	var acc uint32
	bits := 0
	for _, c := range b {
		acc = acc<<8 | uint32(c)
		bits += 8
		for bits >= 5 {
			bits -= 5
			sb.WriteByte(crockford[acc>>bits&31])
		}
	}
	return sb.String(), nil
}

// GroupCode shows a recovery code in groups of 4.
func GroupCode(code string) string {
	var groups []string
	for i := 0; i < len(code); i += 4 {
		groups = append(groups, code[i:min(i+4, len(code))])
	}
	return strings.Join(groups, "-")
}

// NormalizeCode reads a typed recovery code: case, spaces and hyphens do not
// matter, and I, L and O read as 1, 1 and 0 (Crockford). ok is false when it
// is not 24 Crockford characters.
func NormalizeCode(s string) (code string, ok bool) {
	code, ok = NormalizeChars(s)
	return code, ok && len(code) == CodeLen
}

// NormalizeChars reads typed characters of a recovery code as NormalizeCode
// does, at any length: the check of its last 4 characters.
func NormalizeChars(s string) (string, bool) {
	var sb strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch {
		case r == ' ' || r == '-' || r == '\t':
			continue
		case r == 'I' || r == 'L':
			r = '1'
		case r == 'O':
			r = '0'
		}
		if r > 0x7f || !strings.ContainsRune(crockford, r) {
			return "", false
		}
		sb.WriteRune(r)
	}
	return sb.String(), true
}

// PhraseLongEnough reports an own phrase of MinPhraseLen characters or more.
func PhraseLongEnough(p string) bool { return utf8.RuneCountInString(p) >= MinPhraseLen }

// Seal is a new kit file locking sec under phrase and code.
func Seal(sec Secret, kitID, phrase, code string) (File, error) {
	f := File{Format: Format, Version: Version, KitID: kitID, KeyID: KeyID(sec.Key)}
	p, err := f.wrap(sec, TypePhrase, PhraseIterations, phrase)
	if err != nil {
		return File{}, err
	}
	c, err := f.wrap(sec, TypeRecoveryCode, CodeIterations, code)
	if err != nil {
		return File{}, err
	}
	f.Envelopes = []Envelope{p, c}
	return f, nil
}

// Rewrap is f with a new phrase envelope for sec, the same kit_id and key_id,
// and the code envelope copied unchanged ("I forgot my secret phrase": the
// key does not change, CTO-10 point 8).
func Rewrap(f File, sec Secret, phrase string) (File, error) {
	if KeyID(sec.Key) != f.KeyID {
		return File{}, errors.New("kit: the key does not match the kit file's key_id")
	}
	code, err := f.envelope(TypeRecoveryCode)
	if err != nil {
		return File{}, err
	}
	p, err := f.wrap(sec, TypePhrase, PhraseIterations, phrase)
	if err != nil {
		return File{}, err
	}
	out := f
	out.Envelopes = []Envelope{p, code}
	return out, nil
}

// Open unlocks f with a phrase or a recovery code: a secret that reads as a
// recovery code is tried on the code envelope first, then everything on the
// phrase envelope. It fails with ErrWrongSecret when neither opens.
func Open(f File, secret string) (Secret, error) {
	if code, ok := NormalizeCode(secret); ok {
		if sec, err := f.unwrap(TypeRecoveryCode, code); err == nil {
			return sec, nil
		} else if !errors.Is(err, ErrWrongSecret) {
			return Secret{}, err
		}
	}
	return f.unwrap(TypePhrase, secret)
}

// aad is format|version|kit_id|key_id|type|kdf|iterations.
func (f File) aad(typ, kdf string, iterations int) []byte {
	return []byte(strings.Join([]string{f.Format, strconv.Itoa(f.Version), f.KitID, f.KeyID, typ, kdf, strconv.Itoa(iterations)}, "|"))
}

func (f File) envelope(typ string) (Envelope, error) {
	for _, e := range f.Envelopes {
		if e.Type == typ {
			return e, nil
		}
	}
	return Envelope{}, fmt.Errorf("kit: no %s envelope", typ)
}

// gcm is AES-256-GCM under the key PBKDF2-SHA256 derives from password.
func gcm(password string, salt []byte, iterations int) (cipher.AEAD, error) {
	k, err := pbkdf2.Key(sha256.New, password, salt, iterations, KeySize)
	if err != nil {
		return nil, fmt.Errorf("kit: derive key: %w", err)
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return nil, fmt.Errorf("kit: cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

// wrap locks sec under password with a new salt and nonce.
func (f File) wrap(sec Secret, typ string, iterations int, password string) (Envelope, error) {
	if len(sec.Key) != KeySize || len(sec.Salt) != SaltSize {
		return Envelope{}, errors.New("kit: secret has the wrong size")
	}
	salt := make([]byte, envelopeSaltSize)
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(salt); err != nil {
		return Envelope{}, fmt.Errorf("kit: envelope salt: %w", err)
	}
	if _, err := rand.Read(nonce); err != nil {
		return Envelope{}, fmt.Errorf("kit: envelope nonce: %w", err)
	}
	aead, err := gcm(password, salt, iterations)
	if err != nil {
		return Envelope{}, err
	}
	plain := make([]byte, 0, 1+KeySize+SaltSize)
	plain = append(append(append(plain, plainVersion), sec.Key...), sec.Salt...)
	ct := aead.Seal(nil, nonce, plain, f.aad(typ, KDF, iterations))
	b64 := base64.StdEncoding.EncodeToString
	return Envelope{Type: typ, KDF: KDF, Iterations: iterations, Salt: b64(salt), Nonce: b64(nonce), Ciphertext: b64(ct)}, nil
}

// unwrap opens f's envelope typ with password.
func (f File) unwrap(typ, password string) (Secret, error) {
	e, err := f.envelope(typ)
	if err != nil {
		return Secret{}, err
	}
	dec := base64.StdEncoding.DecodeString
	salt, err1 := dec(e.Salt)
	nonce, err2 := dec(e.Nonce)
	ct, err3 := dec(e.Ciphertext)
	if err := errors.Join(err1, err2, err3); err != nil || len(nonce) != nonceSize {
		return Secret{}, fmt.Errorf("kit: malformed %s envelope", typ)
	}
	aead, err := gcm(password, salt, e.Iterations)
	if err != nil {
		return Secret{}, err
	}
	plain, err := aead.Open(nil, nonce, ct, f.aad(e.Type, e.KDF, e.Iterations))
	if err != nil {
		return Secret{}, ErrWrongSecret
	}
	if len(plain) != 1+KeySize+SaltSize || plain[0] != plainVersion {
		return Secret{}, fmt.Errorf("kit: unsupported %s envelope content", typ)
	}
	sec := Secret{Key: plain[1 : 1+KeySize], Salt: plain[1+KeySize:]}
	if KeyID(sec.Key) != f.KeyID {
		return Secret{}, errors.New("kit: the key does not match the kit file's key_id")
	}
	return sec, nil
}

var compiled *schemaval.Schema

// Parse reads a kit file: valid against schemas/kit.v1.schema.json, with the
// phrase envelope first and the code envelope second, each with at least its
// iterations.
func Parse(raw []byte) (File, error) {
	if compiled == nil {
		s, err := schemaval.Compile(schemas.KitSchema)
		if err != nil {
			return File{}, fmt.Errorf("compile kit schema: %w", err)
		}
		compiled = s
	}
	res, err := compiled.Validate(raw)
	if err != nil {
		return File{}, fmt.Errorf("validate kit file: %w", err)
	}
	if !res.Valid() {
		var msgs []string
		for _, e := range res.Errors() {
			msgs = append(msgs, e.String())
		}
		return File{}, fmt.Errorf("kit file fails schema validation: %s", strings.Join(msgs, "; "))
	}
	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return File{}, fmt.Errorf("decode kit file: %w", err)
	}
	if f.Envelopes[0].Type != TypePhrase || f.Envelopes[0].Iterations < PhraseIterations ||
		f.Envelopes[1].Type != TypeRecoveryCode || f.Envelopes[1].Iterations < CodeIterations {
		return File{}, errors.New("kit file: envelopes must be phrase then recovery_code, with their iterations")
	}
	return f, nil
}

// Marshal is f as written to disk, checked against the schema.
func Marshal(f File) ([]byte, error) {
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode kit file: %w", err)
	}
	raw = append(raw, '\n')
	if _, err := Parse(raw); err != nil {
		return nil, err
	}
	return raw, nil
}
