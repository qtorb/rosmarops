package firstrun

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"io"
	"strings"
)

// KitConfig is the text the phrase window shows (BV-131, CTO-12 H2), all from
// the message catalog. Date is empty unless several different kits were found.
type KitConfig struct {
	Title  string `json:"title"`
	Found  string `json:"found"`
	Ask    string `json:"ask"`
	Date   string `json:"date"`
	Use    string `json:"use"`
	NotNow string `json:"not_now"`
	Wrong  string `json:"wrong"`
}

// MaxKitAttempts is how many wrong phrases the window takes before it closes
// itself as «Not now» (CTO-12 H2). maxKitAttemptsStr is the same number baked
// into the PowerShell script; a test checks they agree.
const (
	MaxKitAttempts    = 5
	maxKitAttemptsStr = "5"
)

// kitConfigInput is the config as the window reads it: base64 of its JSON, so
// no code page touches the text.
func kitConfigInput(c KitConfig) string {
	b, _ := json.Marshal(c)
	return base64.StdEncoding.EncodeToString(b)
}

// KitWindowScript shows the phrase window with System.Windows.Forms and talks
// to the program over the standard streams (CTO-12 H2). It reads its texts as
// base64 JSON on the first stdin line. The secret phrase is only ever a .NET
// string from the password TextBox: it leaves as base64 written with
// [Console]::Out, never as a cmdlet argument and never interpolated, so
// PowerShell's module, script-block and transcription logging cannot capture
// it. On «Use it» the window writes the phrase (base64) and reads one verdict
// line: "1" closes the window (the phrase opened the key), "0" shows the
// wrong-phrase label and clears the field. After MaxKitAttempts wrong
// verdicts, or on «Not now» or closing, the window exits and its output ends.
// Under Constrained Language Mode Add-Type fails and PowerShell exits with an
// error, which the caller treats as «Not now» (fail closed).
const KitWindowScript = `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$cfg = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([Console]::In.ReadLine().Trim())) | ConvertFrom-Json
$form = New-Object System.Windows.Forms.Form
$form.Text = $cfg.title
$form.TopMost = $true
$form.FormBorderStyle = 'FixedDialog'
$form.StartPosition = 'CenterScreen'
$form.ClientSize = New-Object System.Drawing.Size(420, 210)
$form.MaximizeBox = $false
$form.MinimizeBox = $false
function Add-Label($text, $y) {
  $l = New-Object System.Windows.Forms.Label
  $l.Text = $text
  $l.AutoSize = $false
  $l.SetBounds(16, $y, 388, 32)
  $form.Controls.Add($l)
  return $l
}
$found = Add-Label $cfg.found 14
$y = 50
if ($cfg.date -ne '') { [void](Add-Label $cfg.date $y); $y += 24 }
[void](Add-Label $cfg.ask $y); $y += 32
$tb = New-Object System.Windows.Forms.TextBox
$tb.UseSystemPasswordChar = $true
$tb.SetBounds(16, $y, 388, 24)
$form.Controls.Add($tb); $y += 34
$wrong = Add-Label $cfg.wrong $y
$wrong.ForeColor = [System.Drawing.Color]::Firebrick
$wrong.Visible = $false
$use = New-Object System.Windows.Forms.Button
$use.Text = $cfg.use
$use.SetBounds(228, 168, 84, 28)
$notnow = New-Object System.Windows.Forms.Button
$notnow.Text = $cfg.not_now
$notnow.SetBounds(320, 168, 84, 28)
$form.Controls.Add($use); $form.Controls.Add($notnow)
$form.AcceptButton = $use
$form.CancelButton = $notnow
$script:tries = 0
$use.Add_Click({
  [Console]::Out.WriteLine([Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($tb.Text)))
  [Console]::Out.Flush()
  $v = [Console]::In.ReadLine()
  if ($v -eq '1') { $form.Close() }
  else {
    $script:tries++
    $wrong.Visible = $true
    $tb.Clear()
    $tb.Focus()
    if ($script:tries -ge ` + maxKitAttemptsStr + `) { $form.Close() }
  }
})
$notnow.Add_Click({ $form.Close() })
[void]$form.ShowDialog()`

// KitWindowArgs are powershell.exe's arguments for the phrase window.
func KitWindowArgs() []string {
	return []string{"-NoProfile", "-NonInteractive", "-STA", "-Command", KitWindowScript}
}

// KitEchoScript is the phrase-handling path with no window, for the mandatory
// logging test (CTO-12 H2): it reads a base64 phrase on stdin (standing in for
// the TextBox value), handles it only with .NET, and writes it back as base64.
// It uses the same two .NET conversions the window's «Use it» handler uses, so
// the test proves that mechanism never writes the phrase where PowerShell
// logging would capture it.
const KitEchoScript = `$ErrorActionPreference = 'Stop'
$p = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([Console]::In.ReadLine().Trim()))
[Console]::Out.Write([Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes($p)))`

// KitEchoArgs are powershell.exe's arguments for the echo script.
func KitEchoArgs() []string {
	return []string{"-NoProfile", "-NonInteractive", "-Command", KitEchoScript}
}

// driveKitWindow runs the verdict protocol with a window already started: it
// reads each base64 phrase the window writes, asks check whether it opens the
// key, and writes "1" (stop, opened) or "0" (wrong, keep asking). It returns
// true when a phrase opened the key. The window's output ending — «Not now»,
// closing, or giving up after MaxKitAttempts — returns false. It also stops
// after maxAttempts wrong phrases of its own, so a misbehaving window cannot
// loop forever.
func driveKitWindow(out io.Reader, in io.Writer, check func(phrase string) bool, maxAttempts int) bool {
	r := bufio.NewReader(out)
	for attempts := 0; attempts < maxAttempts; attempts++ {
		line, err := r.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			return false // the window closed (Not now, or gave up) with nothing pending
		}
		b, derr := base64.StdEncoding.DecodeString(line)
		if derr != nil {
			return false
		}
		if check(string(b)) {
			_, _ = io.WriteString(in, "1\n")
			return true
		}
		_, _ = io.WriteString(in, "0\n")
		if err != nil {
			return false // the window ended right after writing
		}
	}
	return false
}
