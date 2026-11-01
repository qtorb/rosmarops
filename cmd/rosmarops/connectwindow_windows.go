//go:build windows

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// connectWindowConfig is the window's text, all from the catalog. Passed as
// base64 JSON on the first stdin line so no code page touches it.
type connectWindowConfig struct {
	Title   string `json:"title"`
	Q       string `json:"q"`
	Text    string `json:"text"`
	Yes     string `json:"yes"`
	No      string `json:"no"`
	Number  string `json:"number"`
	Wait    string `json:"wait"`
	Done    string `json:"done"`
	Expired string `json:"expired"`
	Failed  string `json:"failed"`
	OK      string `json:"ok"`
}

func connectWindowConfigFrom(cat catalog.Catalog) connectWindowConfig {
	return connectWindowConfig{
		Title:   "RosmarOps",
		Q:       cat.Text("connect.q", nil),
		Text:    cat.Text("connect.text", nil),
		Yes:     cat.Text("connect.yes", nil),
		No:      cat.Text("connect.no", nil),
		Number:  cat.Text("connect.number", nil),
		Wait:    cat.Text("connect.wait", nil),
		Done:    cat.Text("connect.done", nil),
		Expired: cat.Text("connect.expired", nil),
		Failed:  cat.Text("connect.failed", nil),
		OK:      cat.Text("connect.ok", nil),
	}
}

func connectWindowInput(c connectWindowConfig) string {
	b, _ := json.Marshal(c)
	return base64.StdEncoding.EncodeToString(b)
}

// connectWindowArgs are powershell.exe's arguments for the window.
func connectWindowArgs() []string {
	return []string{"-NoProfile", "-NonInteractive", "-STA", "-Command", connectWindowScript}
}

// connectWindowScript shows the window with System.Windows.Forms and talks to
// the program over the standard streams. The first stdin line is the base64
// JSON config. The window shows the question; on «Connect» it writes
// "connect", hides the buttons and shows the number/wait view, then a
// BackgroundWorker reads the program's state lines ("number <N>", then "done",
// "expired" or "failed") and updates the view on the UI thread; the final state
// shows an OK button that, clicked, writes "ok" and closes. «Not now» or
// closing before connecting writes "no" and ends. Under Constrained Language
// Mode Add-Type fails and PowerShell exits, which the caller treats as «Not
// now» (fail closed).
const connectWindowScript = `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$cfg = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([Console]::In.ReadLine().Trim())) | ConvertFrom-Json
$form = New-Object System.Windows.Forms.Form
$form.Text = $cfg.title
$form.TopMost = $true
$form.FormBorderStyle = 'FixedDialog'
$form.StartPosition = 'CenterScreen'
$form.ClientSize = New-Object System.Drawing.Size(440, 240)
$form.MaximizeBox = $false
$form.MinimizeBox = $false
$script:answered = $false
function Add-Label($text, $y, $h) {
  $l = New-Object System.Windows.Forms.Label
  $l.Text = $text
  $l.SetBounds(16, $y, 408, $h)
  $form.Controls.Add($l)
  return $l
}
$ql = Add-Label $cfg.q 14 24
$ql.Font = New-Object System.Drawing.Font($ql.Font, [System.Drawing.FontStyle]::Bold)
$tl = Add-Label $cfg.text 44 90
$numCaption = Add-Label $cfg.number 14 24
$numCaption.Visible = $false
$numValue = Add-Label '' 44 48
$numValue.Font = New-Object System.Drawing.Font($numValue.Font.FontFamily, 32, [System.Drawing.FontStyle]::Bold)
$numValue.Visible = $false
$waitl = Add-Label $cfg.wait 96 48
$waitl.Visible = $false
$resultl = Add-Label '' 14 120
$resultl.Visible = $false
$yes = New-Object System.Windows.Forms.Button
$yes.Text = $cfg.yes
$yes.SetBounds(248, 198, 84, 28)
$no = New-Object System.Windows.Forms.Button
$no.Text = $cfg.no
$no.SetBounds(340, 198, 84, 28)
$ok = New-Object System.Windows.Forms.Button
$ok.Text = $cfg.ok
$ok.SetBounds(340, 198, 84, 28)
$ok.Visible = $false
$form.Controls.Add($yes); $form.Controls.Add($no); $form.Controls.Add($ok)
$form.AcceptButton = $yes
$form.CancelButton = $no
$worker = New-Object System.ComponentModel.BackgroundWorker
$worker.WorkerReportsProgress = $true
$worker.Add_DoWork({
  while ($true) {
    $line = [Console]::In.ReadLine()
    if ($line -eq $null) { break }
    $worker.ReportProgress(0, $line)
  }
})
$worker.Add_ProgressChanged({
  $parts = ([string]$_.UserState).Split(' ', 2)
  switch ($parts[0]) {
    'number' {
      $numCaption.Visible = $true
      $numValue.Text = $parts[1]
      $numValue.Visible = $true
      $waitl.Visible = $true
    }
    'done'    { $waitl.Visible = $false; $resultl.Text = $cfg.done;    $resultl.Visible = $true; $ok.Visible = $true }
    'expired' { $waitl.Visible = $false; $resultl.Text = $cfg.expired; $resultl.Visible = $true; $ok.Visible = $true }
    'failed'  { $waitl.Visible = $false; $resultl.Text = $cfg.failed;  $resultl.Visible = $true; $ok.Visible = $true }
  }
})
$yes.Add_Click({
  $script:answered = $true
  [Console]::Out.WriteLine('connect')
  [Console]::Out.Flush()
  $ql.Visible = $false; $tl.Visible = $false
  $yes.Visible = $false; $no.Visible = $false
  $worker.RunWorkerAsync()
})
$no.Add_Click({ $form.Close() })
$ok.Add_Click({ [Console]::Out.WriteLine('ok'); [Console]::Out.Flush(); $form.Close() })
[void]$form.ShowDialog()
if (-not $script:answered) { [Console]::Out.WriteLine('no'); [Console]::Out.Flush() }`

// maybeConnectWindow shows the connect window on a Windows double click when
// this computer is not connected, and runs the protocol if the person agrees
// (BV-137 point 5). Any failure to show the window is logged and ignored: the
// run goes on, and `rosmarops connect` is always available.
func maybeConnectWindow(paths spacePaths, cat catalog.Catalog) {
	if isSpaceConnected(paths) {
		return // already connected (permission AND credential): no window
	}
	cmd := exec.Command("powershell", connectWindowArgs()...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		slog.Info("connect window not shown", "err", err)
		return
	}
	fmt.Fprintln(stdin, connectWindowInput(connectWindowConfigFrom(cat)))
	run := func(onNumber func(int), onOpen func(string) error) error {
		ctx, cancel := context.WithTimeout(context.Background(), 11*time.Minute)
		defer cancel()
		return spaceConnect(ctx, paths, onNumber, onOpen)
	}
	if driveConnectWindow(stdout, stdin, run) {
		slog.Info("connect window: this computer is now connected")
	}
	_ = stdin.Close()
	_ = cmd.Wait()
}
