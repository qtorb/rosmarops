package graph

import "testing"

// TestLocationParsing (BV-202 S4): the single ParseLocation handles "#"
// pointers and ":<line>" suffixes without ever splitting a Windows drive
// colon or a colon that is not a line number.
func TestLocationParsing(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\Users\a\.cursor\mcp.json#/servers/x/env/TOKEN`, `C:\Users\a\.cursor\mcp.json`},
		{`C:\Users\a\src\app\.env:12`, `C:\Users\a\src\app\.env`},
		{`C:\Users\a\src\app\.env`, `C:\Users\a\src\app\.env`}, // no line, drive colon kept
		{`/home/a/src/app/.env:12`, `/home/a/src/app/.env`},
		{`/home/a/.cursor/mcp.json#/servers/x/env/TOKEN`, `/home/a/.cursor/mcp.json`},
		{`\\host\share\proj\.env:3`, `\\host\share\proj\.env`}, // UNC
		{`C:\proj\café\.env:7`, `C:\proj\café\.env`},           // non-ASCII
		{`/home/a/.env`, `/home/a/.env`},                       // no separator
		{`/home/a/file:notaline`, `/home/a/file:notaline`},     // colon not a line number
	}
	for _, c := range cases {
		if got := ParseLocation(c.in); got != c.want {
			t.Errorf("ParseLocation(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
