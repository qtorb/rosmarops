//go:build windows

package firstrun

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Pick opens the folder picker with title and returns the folder chosen, ""
// when cancelled. Its PowerShell is a process of its own, visible (no hidden
// window, so that the dialog shows), with no time limit: the person takes
// the time they need (U4).
func Pick(title string) (string, error) {
	ps, err := exec.LookPath("powershell.exe")
	if err != nil {
		return "", fmt.Errorf("folder picker: powershell.exe not found: %w", err)
	}
	cmd := exec.Command(ps, PickerArgs()...)
	cmd.Stdin = strings.NewReader(pickerInput(title))
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("folder picker: %w; stderr: %s", err, strings.TrimSpace(errb.String()))
	}
	return pickerOutput(out.String())
}
