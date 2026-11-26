package secrets

import (
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// fakeToken joins the three parts of a made-up Discord-shaped token at run
// time, so no token-looking string is in the source (as the other secret
// tests do).
func fakeToken(id, ts, mac string) string { return id + "." + ts + "." + mac }

// bv311Pieces are fragments that build texts near or inside the shapes:
// Discord-shaped tokens and near misses, Bearer headers, database URLs.
var bv311Pieces = []string{
	fakeToken("M"+strings.Repeat("a1B", 8)[:23], "Xy_9-z", strings.Repeat("q7W", 10)[:27]),
	fakeToken("N"+strings.Repeat("c2D", 9)[:25], "AbCdEf", strings.Repeat("r8X", 13)[:38]),
	fakeToken("O"+strings.Repeat("e3F", 8)[:24], "123456", strings.Repeat("s9Y", 11)[:30]),
	"M23456789012345678901234.abcdef.", ".abcdef.", "x.ABCDEF.y", "this.config.get", "a.b.c",
	"Authorization: Bearer ", "authorization=bearer ", "AUTHORIZATION: BEARER ", "Bearer", "bearer",
	"DATABASE_URL=postgres://u:p@h", "database_url: mysql://root:pw@", "_URL", "://", "Database_Url=redis://a:b@",
}

func randomText(rng *rand.Rand, pieces []string) []byte {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-.:=/@ \"'\n"
	var b strings.Builder
	for j := 0; j < 6; j++ {
		if rng.Intn(2) == 0 {
			b.WriteString(pieces[rng.Intn(len(pieces))])
		}
		for k := rng.Intn(60); k > 0; k-- {
			b.WriteByte(alphabet[rng.Intn(len(alphabet))])
		}
	}
	return []byte(b.String())
}

// BV-311: every shape is a necessary condition: on texts built from tokens,
// near misses and noise, whenever the pattern's regex matches, its shape
// holds; and the three shapes do see their tokens.
func TestShapesAreNecessary(t *testing.T) {
	rng := rand.New(rand.NewSource(311))
	byID := map[string]Pattern{}
	for _, p := range BuiltinPatterns() {
		byID[p.ID] = p
	}
	matched := map[string]int{}
	for i := 0; i < 50000; i++ {
		text := randomText(rng, bv311Pieces)
		for id, shape := range builtinShapes {
			if byID[id].re.Match(text) {
				matched[id]++
				if !shape(text) {
					t.Fatalf("%s matches %q but its shape says no", id, text)
				}
			}
		}
	}
	for id := range builtinShapes {
		if matched[id] == 0 {
			t.Errorf("%s: no text matched; the test proves nothing for it", id)
		}
	}
}

// discordShape on hand-made cases: a token, and the near misses it must
// reject without running the regex.
func TestDiscordShape(t *testing.T) {
	id, ts, mac := "M"+strings.Repeat("a1B", 8)[:23], "Xy_9-z", strings.Repeat("q7W", 10)[:27]
	for _, c := range []struct {
		s    string
		want bool
	}{
		{"token: " + fakeToken(id, ts, mac), true}, // 24, 6, 27
		{fakeToken(id, ts, mac[:26]), false},       // 26 after
		{fakeToken(id[1:], ts, mac), false},        // 23 before
		{fakeToken(id, ts[:5], mac), false},        // 5 between
		{fakeToken(id, ts, mac[:24]), false},       // 24 after
		{"this.config.get(x)", false},
		{"", false},
		{"................................................", false},
	} {
		if got := discordShape([]byte(c.s)); got != c.want {
			t.Errorf("discordShape(%q) = %v, want %v", c.s, got, c.want)
		}
	}
}

// containsFoldASCII equals a plain lowercase search, whatever its anchor.
func TestContainsFoldASCIIEqualsNaive(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	lits := []string{"Authorization", "databa", "RAILWAY_TO", "Bearer", "_url", "zz", "a", "AaA"}
	const alphabet = "aAbBzZrRuUlL_ .tTiInNoO"
	for i := 0; i < 20000; i++ {
		lit := lits[rng.Intn(len(lits))]
		var b strings.Builder
		for k := rng.Intn(40); k > 0; k-- {
			if rng.Intn(8) == 0 {
				b.WriteString(lit)
			}
			b.WriteByte(alphabet[rng.Intn(len(alphabet))])
		}
		s := b.String()
		want := strings.Contains(strings.ToLower(s), strings.ToLower(lit))
		if got := containsFoldASCII([]byte(s), lit); got != want {
			t.Fatalf("containsFoldASCII(%q, %q) = %v, want %v", s, lit, got, want)
		}
	}
}

// BV-311: the matches are the same, one by one, with and without the
// prefilters and shapes, in every file of the repository up to 2 MB: the
// fixtures and testdata, and the test sources with their example tokens.
func TestSameMatchesInEveryFixture(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	with := BuiltinPatterns()
	without := WithoutPrefilters(with)
	files, total := 0, 0
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == ".git" || d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || info.Size() > 2<<20 {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		files++
		got, want := Match(b, with), Match(b, without)
		total += len(want)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %d matches with the prefilters, %d without", p, len(got), len(want))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("no file found")
	}
	if total == 0 {
		t.Fatal("no match in any file: the test proves nothing")
	}
	t.Logf("%d files, %d matches", files, total)
}
