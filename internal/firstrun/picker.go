package firstrun

import (
	"encoding/base64"
	"fmt"
	"strings"
)

// PickerScript opens Windows's folder picker (System.Windows.Forms
// .FolderBrowserDialog) in front of every window (U4). It is constant: the
// title comes on the standard input and the chosen folder leaves on the
// standard output, both base64 of UTF-8, so that no code page touches them.
// It prints nothing when cancelled; under Constrained Language Mode Add-Type
// fails, PowerShell exits with an error and the caller treats it as
// cancelled.
const PickerScript = `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
$t = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([Console]::In.ReadToEnd().Trim()))
$o = New-Object System.Windows.Forms.Form
$o.TopMost = $true
$d = New-Object System.Windows.Forms.FolderBrowserDialog
$d.Description = $t
$d.ShowNewFolderButton = $false
if ($d.ShowDialog($o) -eq [System.Windows.Forms.DialogResult]::OK) {
  [Console]::Out.Write([Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($d.SelectedPath)))
}`

// PickerArgs are powershell.exe's arguments: always the same.
func PickerArgs() []string {
	return []string{"-NoProfile", "-NonInteractive", "-STA", "-Command", PickerScript}
}

// pickerInput is the title as the script reads it.
func pickerInput(title string) string { return base64.StdEncoding.EncodeToString([]byte(title)) }

// pickerOutput is the chosen folder from the script's output; "" when
// cancelled.
func pickerOutput(out string) (string, error) {
	out = strings.TrimSpace(out)
	if out == "" {
		return "", nil
	}
	b, err := base64.StdEncoding.DecodeString(out)
	if err != nil {
		return "", fmt.Errorf("folder picker output: %w", err)
	}
	return string(b), nil
}
