package skills

import "testing"

// BV-99b, item 2: trusted roots compare cleaned and, on Windows paths,
// without case; entries expand ~ and variables as Hermes does.
func TestPathKeyAndExpand(t *testing.T) {
	for _, c := range [][2]string{
		{`C:\Users\Albert\Src\App\`, `c:\users\albert\src\app`},
		{`C:/Users/Albert/src/app`, `c:\users\albert\src\app`},
		{`\\server\share\Repo`, `\\SERVER\share\repo`},
	} {
		if pathKey(c[0]) != pathKey(c[1]) {
			t.Errorf("pathKey(%q) = %q, pathKey(%q) = %q, want equal", c[0], pathKey(c[0]), c[1], pathKey(c[1]))
		}
	}
	if pathKey("/src/App") == pathKey("/src/app") {
		t.Error("a POSIX path compared without case")
	}
	env := func(k string) (string, bool) {
		v, ok := map[string]string{"USERPROFILE": `C:\Users\albert`, "SRC": "/src"}[k]
		return v, ok
	}
	for in, want := range map[string]string{
		`%USERPROFILE%\code\app`: `C:\Users\albert\code\app`,
		"${SRC}/app":             "/src/app",
		"$SRC/app":               "/src/app",
		"$UNSET/app":             "$UNSET/app",
		"~/app":                  "/home/albert/app",
		"~":                      "/home/albert",
	} {
		if got := expandHermesPath(in, "/home/albert", env); pathKey(got) != pathKey(want) {
			t.Errorf("expandHermesPath(%q) = %q, want %q", in, got, want)
		}
	}
}
