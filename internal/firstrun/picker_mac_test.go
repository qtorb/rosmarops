package firstrun

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// BV-102, point 2: osascript by its absolute path, the constant script, the
// title as its argument (never in the script), a minimal environment; the
// folder without its trailing slash; -128 is cancelled.
func TestPickMac(t *testing.T) {
	title := `Choose "your" folder" & do shell script "x`
	env := []string{"HOME=/Users/me", "USER=me", "LOGNAME=me", "TMPDIR=/var/folders/x/", "LANG=en_US.UTF-8", "PATH=/opt/evil:/usr/bin", "DYLD_INSERT_LIBRARIES=/tmp/x.dylib", "OSA_LIBRARY_PATH=/tmp"}
	var gotName string
	var gotArgs, gotEnv []string
	run := func(out, errb string, err error) macRunner {
		return func(name string, args, env []string) (string, string, error) {
			gotName, gotArgs, gotEnv = name, args, env
			return out, errb, err
		}
	}

	dir, err := pickMac(run("/Users/me/Projects/\n", "", nil), title, env)
	if err != nil || dir != "/Users/me/Projects" {
		t.Fatalf("dir = %q, err = %v", dir, err)
	}
	if gotName != "/usr/bin/osascript" {
		t.Errorf("ran %q", gotName)
	}
	want := []string{"-e", "on run argv", "-e", "return POSIX path of (choose folder with prompt (item 1 of argv))", "-e", "end run", title}
	if !reflect.DeepEqual(gotArgs, want) {
		t.Errorf("args = %q, want %q", gotArgs, want)
	}
	for _, a := range gotArgs[:len(gotArgs)-1] {
		if strings.Contains(a, "your") {
			t.Errorf("the title is in the script: %q", a)
		}
	}
	wantEnv := []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin", "HOME=/Users/me", "USER=me", "LOGNAME=me", "TMPDIR=/var/folders/x/", "LANG=en_US.UTF-8"}
	if !reflect.DeepEqual(gotEnv, wantEnv) {
		t.Errorf("env = %q, want %q", gotEnv, wantEnv)
	}

	if dir, err := pickMac(run("/\n", "", nil), title, env); err != nil || dir != "/" {
		t.Errorf("root: dir = %q, err = %v", dir, err)
	}
	if dir, err := pickMac(run("", "0:73: execution error: User canceled. (-128)\n", errors.New("exit status 1")), title, env); err != nil || dir != "" {
		t.Errorf("cancelled: dir = %q, err = %v", dir, err)
	}
	if _, err := pickMac(run("", "execution error: Not authorized (-1743)", errors.New("exit status 1")), title, env); err == nil {
		t.Error("a failure is not an error")
	}
	gotName = ""
	if _, err := pickMac(run("", "", nil), "-e", env); err == nil || gotName != "" {
		t.Errorf("a title starting with -: err = %v, ran %q", err, gotName)
	}
}
