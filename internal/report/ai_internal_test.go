package report

import "testing"

// The user's folder in a value's rules becomes ~/ (the home form Claude
// Code reads the same), on Windows and Unix paths.
func TestHomeInRules(t *testing.T) {
	for _, c := range []struct{ value, home, want string }{
		{`{"permissions":{"ask":["Read(//c/Users/Albert/webapp/.env.local)"]}}`, `C:\Users\Albert`, `{"permissions":{"ask":["Read(~/webapp/.env.local)"]}}`},
		{`{"permissions":{"ask":["Edit(//home/albert/repo/.env)"]}}`, "/home/albert/", `{"permissions":{"ask":["Edit(~/repo/.env)"]}}`},
		{`{"permissions":{"ask":["Read(//d/src/.env)"]}}`, `C:\Users\Albert`, `{"permissions":{"ask":["Read(//d/src/.env)"]}}`},
		{`{"permissions":{"ask":["Read(//c/Users/AlbertX/.env)"]}}`, `C:\Users\Albert`, `{"permissions":{"ask":["Read(//c/Users/AlbertX/.env)"]}}`},
	} {
		if got := homeInRules(c.value, c.home); got != c.want {
			t.Errorf("%s: %s, want %s", c.value, got, c.want)
		}
	}
}
