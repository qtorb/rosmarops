package secrets

import (
	"math/rand"
	"reflect"
	"regexp/syntax"
	"strings"
	"testing"
	"time"
	"unicode"
)

// BV-33: for every built-in pattern, its prefilter is a necessary condition:
// the regex is a concatenation one of whose required elements is a literal
// containing the prefilter (with the same case folding), so every text the
// regex matches contains it. For a fold literal, every letter's case
// folding stays within ASCII, so containsFoldASCII misses no match.
func TestBuiltinPrefiltersAreNecessary(t *testing.T) {
	for _, s := range builtinSpecs {
		pre, ok := builtinPrefilters[s.id]
		if !ok {
			t.Logf("%s: no prefilter (always run)", s.id)
			continue
		}
		re, err := syntax.Parse(s.expr, syntax.Perl)
		if err != nil {
			t.Fatal(err)
		}
		re = re.Simplify()
		parts := []*syntax.Regexp{re}
		if re.Op == syntax.OpConcat {
			parts = re.Sub
		}
		found := false
		for _, p := range parts {
			if p.Op != syntax.OpLiteral {
				continue
			}
			fold := p.Flags&syntax.FoldCase != 0
			lit := string(p.Rune)
			if fold != pre.fold {
				continue
			}
			if (!fold && strings.Contains(lit, pre.lit)) || (fold && strings.Contains(strings.ToLower(lit), strings.ToLower(pre.lit))) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: prefilter %q is not a required literal of %s", s.id, pre.lit, s.expr)
		}
		if pre.fold {
			for _, r := range pre.lit {
				for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
					if f > unicode.MaxASCII {
						t.Errorf("%s: %q folds to non-ASCII %U", s.id, r, f)
					}
				}
			}
		}
	}
	if len(builtinPrefilters) != len(builtinSpecs) {
		t.Errorf("%d prefilters for %d patterns", len(builtinPrefilters), len(builtinSpecs))
	}
}

// Match with prefilters equals Match without them, on texts built from the
// patterns' own literals and random noise, and on every pattern's example.
func TestPrefilterDoesNotChangeMatches(t *testing.T) {
	with := BuiltinPatterns()
	without := make([]Pattern, len(with))
	for i, p := range with {
		p.pre = prefilter{}
		without[i] = p
	}
	rng := rand.New(rand.NewSource(1))
	alphabet := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_-.:=/@ \"'\n"
	var pieces []string
	for _, p := range builtinPrefilters {
		pieces = append(pieces, p.lit, strings.ToUpper(p.lit), strings.ToLower(p.lit))
	}
	pieces = append(pieces, "Bearer ", "postgres://u:p@h", "PRIVATE KEY-----", "-sk-", "eyJ", ".eyJ")
	for i := 0; i < 20000; i++ {
		var b strings.Builder
		for j := 0; j < 6; j++ {
			if rng.Intn(2) == 0 {
				b.WriteString(pieces[rng.Intn(len(pieces))])
			}
			for k := rng.Intn(80); k > 0; k-- {
				b.WriteByte(alphabet[rng.Intn(len(alphabet))])
			}
		}
		text := []byte(b.String())
		if got, want := Match(text, with), Match(text, without); !reflect.DeepEqual(got, want) {
			t.Fatalf("text %q: with prefilter %v, without %v", text, got, want)
		}
	}
}

func TestContainsFoldASCII(t *testing.T) {
	for _, c := range []struct {
		s, lit string
		want   bool
	}{
		{"x railway_to y", "RAILWAY_TO", true},
		{"Railway_To", "RAILWAY_TO", true},
		{"RAILWAY_T", "RAILWAY_TO", false},
		{"rrailway_to", "RAILWAY_TO", true},
		{"", "a", false},
		{"AUTHORIZATION:", "Authorization", true},
	} {
		if got := containsFoldASCII([]byte(c.s), c.lit); got != c.want {
			t.Errorf("containsFoldASCII(%q, %q) = %v", c.s, c.lit, got)
		}
	}
}

// BV-33: the fold search is linear. 40 MB with only one case of the first
// letter, the way a large commit diff can look, ends in well under a second.
func TestContainsFoldASCII_Linear(t *testing.T) {
	content := []byte(strings.Repeat("data, done; ", 40*1024*1024/12))
	start := time.Now()
	if containsFoldASCII(content, "databa") {
		t.Fatal("false match")
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %s on %d bytes: not linear", d, len(content))
	}
	if !containsFoldASCII(append(content, "DataBase"...), "databa") {
		t.Error("missed the match at the end")
	}
}
