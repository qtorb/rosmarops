package firstrun

import (
	"errors"
	"fmt"
	"strings"
)

// macOsascript is macOS's osascript, by its absolute path (decision
// 2026-10-29, macOS vista previa, point 2). A variable only so that the
// integration tests can link a fake (-ldflags -X); nothing changes it at
// run time.
var macOsascript = "/usr/bin/osascript"

// MacPickerScript opens macOS's folder picker (choose folder). It is
// constant: the title comes as its only argument (argv), never in the
// script's text, and the chosen folder leaves on the standard output as a
// POSIX path.
var MacPickerScript = []string{
	"on run argv",
	"return POSIX path of (choose folder with prompt (item 1 of argv))",
	"end run",
}

// MacPickerArgs are osascript's arguments: the script's lines, each after
// -e, then the title as the script's argument.
func MacPickerArgs(title string) []string {
	args := make([]string, 0, 2*len(MacPickerScript)+1)
	for _, l := range MacPickerScript {
		args = append(args, "-e", l)
	}
	return append(args, title)
}

// macPickerEnv is the picker's minimal environment: the system PATH and,
// from env, only what a GUI dialog needs (the user, the home folder, the
// temporary folder and the language).
func macPickerEnv(env []string) []string {
	out := []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch k {
		case "HOME", "USER", "LOGNAME", "TMPDIR", "LANG":
			out = append(out, kv)
		}
	}
	return out
}

// macRunner runs name with args and env, no shell, and returns its standard
// output and error; tests replace it.
type macRunner func(name string, args, env []string) (stdout, stderr string, err error)

// macCancelled is the AppleScript error number of a cancelled dialog
// (userCanceledErr), as osascript prints it.
const macCancelled = "(-128)"

// pickMac opens the picker through run with title and returns the folder
// chosen, without the trailing slash; "" when cancelled.
func pickMac(run macRunner, title string, env []string) (string, error) {
	if strings.HasPrefix(title, "-") {
		return "", errors.New("folder picker: the title starts with -")
	}
	out, errb, err := run(macOsascript, MacPickerArgs(title), macPickerEnv(env))
	if err != nil {
		if strings.Contains(errb, macCancelled) {
			return "", nil
		}
		return "", fmt.Errorf("folder picker: %w; stderr: %s", err, strings.TrimSpace(errb))
	}
	dir := strings.TrimSuffix(out, "\n")
	if dir != "/" {
		dir = strings.TrimSuffix(dir, "/")
	}
	return dir, nil
}
