package kit

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const testPhrase = "correct horse battery staple x"

func testSalt() []byte { return bytes.Repeat([]byte{0x5a}, SaltSize) }

// sealed is a new kit file with its secret and recovery code.
func sealed(t *testing.T) (File, Secret, string) {
	t.Helper()
	sec, err := NewSecret(testSalt())
	if err != nil {
		t.Fatal(err)
	}
	id, err := NewKitID()
	if err != nil {
		t.Fatal(err)
	}
	code, err := NewRecoveryCode()
	if err != nil {
		t.Fatal(err)
	}
	f, err := Seal(sec, id, testPhrase, code)
	if err != nil {
		t.Fatal(err)
	}
	return f, sec, code
}

// remarshal is f written and read back, as an import reads it.
func remarshal(t *testing.T, f File) File {
	t.Helper()
	raw, err := Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	back, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return back
}

func TestRoundTripPhraseAndCode(t *testing.T) {
	f, sec, code := sealed(t)
	f = remarshal(t, f)
	for name, secret := range map[string]string{
		"phrase": testPhrase, "code": code, "grouped lowercase code": strings.ToLower(GroupCode(code)),
	} {
		got, err := Open(f, secret)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(got.Key, sec.Key) || !bytes.Equal(got.Salt, sec.Salt) {
			t.Errorf("%s: opened another secret", name)
		}
	}
	if _, err := Open(f, "not the phrase at all, no"); !errors.Is(err, ErrWrongSecret) {
		t.Errorf("wrong phrase: %v", err)
	}
	wrongCode := []byte(code)
	wrongCode[0] = map[bool]byte{true: '1', false: '0'}[wrongCode[0] == '0']
	if _, err := Open(f, string(wrongCode)); !errors.Is(err, ErrWrongSecret) {
		t.Errorf("wrong code: %v", err)
	}
}

// The file's shape is CTO-10 §3: format, version, ids, and two envelopes
// with their KDF parameters, new salts and nonces; nothing in the clear.
func TestFileShape(t *testing.T) {
	f, sec, code := sealed(t)
	raw, err := Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if f.Format != "rosmarops-kit" || f.Version != 1 || f.KeyID != KeyID(sec.Key) || !regexp.MustCompile(`^[0-9a-f]{32}$`).MatchString(f.KitID) {
		t.Errorf("header: %+v", f)
	}
	p, c := f.Envelopes[0], f.Envelopes[1]
	if p.Type != "phrase" || p.Iterations != 600000 || c.Type != "recovery_code" || c.Iterations != 100000 || p.KDF != "pbkdf2-sha256" || c.KDF != "pbkdf2-sha256" {
		t.Errorf("envelopes: %+v", f.Envelopes)
	}
	if p.Salt == c.Salt || p.Nonce == c.Nonce {
		t.Error("the envelopes share a salt or a nonce")
	}
	for _, s := range []string{testPhrase, code, string(sec.Key), string(sec.Salt), b64(sec.Key), b64(sec.Salt)} {
		if bytes.Contains(raw, []byte(s)) {
			t.Errorf("the kit file holds a secret in the clear: %.8q…", s)
		}
	}
}

func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

// Each field the AAD binds makes the envelope fail when it changes.
func TestTamperedAADFails(t *testing.T) {
	f, _, code := sealed(t)
	other, _, _ := sealed(t)
	for name, tamper := range map[string]func(*File){
		"kit_id":     func(f *File) { f.KitID = other.KitID },
		"key_id":     func(f *File) { f.KeyID = other.KeyID },
		"version":    func(f *File) { f.Version = 2 },
		"format":     func(f *File) { f.Format = "rosmarops-kot" },
		"iterations": func(f *File) { f.Envelopes[0].Iterations++; f.Envelopes[1].Iterations++ },
		"type swap": func(f *File) {
			f.Envelopes[0].Type, f.Envelopes[1].Type = TypeRecoveryCode, TypePhrase
		},
	} {
		g := f
		g.Envelopes = append([]Envelope(nil), f.Envelopes...)
		tamper(&g)
		for _, secret := range []string{testPhrase, code} {
			if _, err := Open(g, secret); err == nil {
				t.Errorf("%s: tampered file opened", name)
			}
		}
	}
	// And the untouched file still opens.
	if _, err := Open(f, testPhrase); err != nil {
		t.Fatal(err)
	}
}

// new-phrase: the key, kit_id, key_id and code envelope stay; the old phrase
// no longer opens the new file; the new phrase and the code do.
func TestNewPhraseKeepsKey(t *testing.T) {
	f, sec, code := sealed(t)
	stored, err := FromFile(f, sec)
	if err != nil {
		t.Fatal(err)
	}
	const newPhrase = "a brand new phrase of mine"
	g, err := stored.NewPhrase(newPhrase)
	if err != nil {
		t.Fatal(err)
	}
	g = remarshal(t, g)
	if g.KitID != f.KitID || g.KeyID != f.KeyID || !reflect.DeepEqual(g.Envelopes[1], f.Envelopes[1]) {
		t.Errorf("new-phrase changed the ids or the code envelope")
	}
	if g.Envelopes[0] == f.Envelopes[0] {
		t.Error("new-phrase kept the phrase envelope")
	}
	if _, err := Open(g, testPhrase); !errors.Is(err, ErrWrongSecret) {
		t.Errorf("old phrase on the new file: %v", err)
	}
	for _, secret := range []string{newPhrase, code} {
		got, err := Open(g, secret)
		if err != nil || !bytes.Equal(got.Key, sec.Key) {
			t.Errorf("%q does not open the key: %v", secret, err)
		}
	}
	// The old file still opens with the old phrase.
	if _, err := Open(f, testPhrase); err != nil {
		t.Error(err)
	}
}

func TestRecoveryCode(t *testing.T) {
	seen := map[string]bool{}
	for range 50 {
		code, err := NewRecoveryCode()
		if err != nil {
			t.Fatal(err)
		}
		if !regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{24}$`).MatchString(code) {
			t.Fatalf("not 24 Crockford characters: %q", code)
		}
		seen[code] = true
		if g := GroupCode(code); len(g) != 29 || strings.Count(g, "-") != 5 {
			t.Errorf("groups of 4: %q", g)
		}
	}
	if len(seen) != 50 {
		t.Error("repeated codes")
	}
	for in, want := range map[string]string{
		"abcd-efgh-jkmn-pqrs-tvwx-yz01": "ABCDEFGHJKMNPQRSTVWXYZ01",
		"oOiI lL00 1111 2222 3333 4444": "001111001111222233334444",
	} {
		got, ok := NormalizeCode(in)
		if !ok || got != want {
			t.Errorf("NormalizeCode(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "ABCD", "ABCDEFGHJKMNPQRSTVWXYZ0U", "ABCDEFGHJKMNPQRSTVWXYZ012"} {
		if _, ok := NormalizeCode(bad); ok {
			t.Errorf("NormalizeCode(%q) accepted", bad)
		}
	}
}

func TestPhraseRules(t *testing.T) {
	if PhraseLongEnough("fifteen chars!!") || !PhraseLongEnough("sixteen chars!!!") || !PhraseLongEnough("ñandú ñandú ñand") {
		t.Error("16-character rule")
	}
	if _, err := generatePhrase(nil); !errors.Is(err, ErrNoWordlist) {
		t.Errorf("no list: %v", err)
	}
	words := make([]string, effListSize)
	for i := range words {
		words[i] = "w" + strconv.Itoa(i)
	}
	p, err := generatePhrase(words)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(strings.Fields(p)); n != PhraseWords {
		t.Errorf("%d words: %q", n, p)
	}
}

func TestParseRejects(t *testing.T) {
	f, _, _ := sealed(t)
	good, err := Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(map[string]any){
		"other format":     func(m map[string]any) { m["format"] = "x" },
		"extra field":      func(m map[string]any) { m["key"] = "x" },
		"one envelope":     func(m map[string]any) { m["envelopes"] = m["envelopes"].([]any)[:1] },
		"swapped order":    func(m map[string]any) { e := m["envelopes"].([]any); e[0], e[1] = e[1], e[0] },
		"weak phrase kdf":  func(m map[string]any) { m["envelopes"].([]any)[0].(map[string]any)["iterations"] = 100000 },
		"bad nonce":        func(m map[string]any) { m["envelopes"].([]any)[0].(map[string]any)["nonce"] = "AAAA" },
		"uppercase kit_id": func(m map[string]any) { m["kit_id"] = strings.ToUpper(m["kit_id"].(string)) },
	} {
		var m map[string]any
		if err := json.Unmarshal(good, &m); err != nil {
			t.Fatal(err)
		}
		edit(m)
		raw, _ := json.Marshal(m)
		if _, err := Parse(raw); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestStoreRoundTrip(t *testing.T) {
	f, sec, _ := sealed(t)
	stored, err := FromFile(f, sec)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), ".rosmarops")
	path := filepath.Join(dir, "kit.key")
	if err := Save(path, stored); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, stored) {
		t.Error("the stored kit changed on the way")
	}
	if runtime.GOOS != "windows" {
		for p, want := range map[string]os.FileMode{path: 0o600, dir: 0o700} {
			info, err := os.Stat(p)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != want {
				t.Errorf("%s: mode %o, want %o", p, info.Mode().Perm(), want)
			}
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files left: %v", entries)
	}
	if _, err := Load(filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing: %v", err)
	}
}
