//go:build darwin

package firstrun

import (
	"bytes"
	"os"
	"os/exec"
)

// Pick opens the folder picker with title and returns the folder chosen, ""
// when cancelled: /usr/bin/osascript with a constant script, the title as
// its argument and a minimal environment, with no time limit: the person
// takes the time they need (U4; decision 2026-10-29, macOS vista previa,
// point 2).
func Pick(title string) (string, error) { return pickMac(execMac, title, os.Environ()) }

// execMac runs the program with exec: no shell, env as its environment.
func execMac(name string, args, env []string) (string, string, error) {
	cmd := exec.Command(name, args...)
	cmd.Env = env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	return out.String(), errb.String(), err
}
