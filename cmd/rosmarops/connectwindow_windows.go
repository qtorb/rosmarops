//go:build windows

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/winpath"
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
// the program over the standard streams. The first stdin line is the base64 JSON
// config, read with [Console]::In.ReadLine() before the window shows (as v0.7.0
// did). The window shows the question; on «Connect» it writes "connect", hides
// the buttons and starts a dedicated runspace that reads the program's state
// lines ("number <N>", then "done", "expired" or "failed") from [Console]::In on
// its own thread into a concurrent queue, which a UI Timer drains — so the
// interface thread is never blocked on a read (BV-145, BV-146, BV-147). The final
// state shows an OK button that, clicked, writes "ok" and closes; closing stops
// the reader runspace. «Not now» or closing before connecting writes "no" and
// ends. Under Constrained Language Mode Add-Type fails and PowerShell exits, which
// the caller treats as «Not now» (fail closed). With
// ROSMAROPS_CONNECT_WINDOW_SELFTEST set the window drives itself, echoes its label
// values and writes TICK on every Timer tick, for the Windows CI self-test only;
// ROSMAROPS_CONNECT_WINDOW_READMODE selects a broken reader (BV-145's
// consolein_async or v0.7.0's backgroundworker) so that self-test can prove each
// one fails (test only, never set in production).
const connectWindowScript = `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$selftest = -not [string]::IsNullOrEmpty($env:ROSMAROPS_CONNECT_WINDOW_SELFTEST)
# ROSMAROPS_CONNECT_WINDOW_READMODE selects the reader for the state lines.
# Default (empty) is the fix: a dedicated runspace (below). 'consolein_async' is
# BV-145's reader and 'backgroundworker' is v0.7.0's, kept only so the Windows
# self-test can prove each fails. Never set in production.
$readmode = $env:ROSMAROPS_CONNECT_WINDOW_READMODE
# The config line is read on this thread, before the window shows, exactly as
# v0.7.0 did (this always worked). Everything after it is read by the reader
# runspace, so the interface thread never touches [Console]::In again (BV-147):
# mixing a second reader over the same input is not reliable (its buffer and the
# host's would split the stream).
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
# Read the program's state lines without blocking the interface thread (BV-145,
# BV-146, BV-147). A dedicated runspace reads [Console]::In line by line on its
# own thread into $q; the UI Timer drains $q and handles each line ("number <N>",
# "done", "expired", "failed"), stopping on the end marker. The message loop stays
# free to paint throughout, and the interface thread never reads [Console]::In
# after the config line.
$script:q = New-Object System.Collections.Concurrent.ConcurrentQueue[string]
$script:eof = [char]0 + 'eof'
$script:reader = $null
$script:rsps = $null
$script:rs = $null
$timer = New-Object System.Windows.Forms.Timer
$timer.Interval = 200
function Show-Result($text) {
  $waitl.Visible = $false
  $resultl.Text = $text
  $resultl.Visible = $true
  $ok.Visible = $true
  if ($selftest) { [Console]::Out.WriteLine('RESULT=' + $text); [Console]::Out.Flush(); $form.Close() }
}
function Handle-Line($line) {
  $parts = ([string]$line).Split(' ', 2)
  switch ($parts[0]) {
    'number' {
      $numCaption.Visible = $true
      $numValue.Text = $parts[1]
      $numValue.Visible = $true
      $waitl.Visible = $true
      if ($selftest) { [Console]::Out.WriteLine('NUMLABEL=' + $numValue.Text); [Console]::Out.Flush() }
    }
    'done'    { Show-Result $cfg.done }
    'expired' { Show-Result $cfg.expired }
    'failed'  { Show-Result $cfg.failed }
  }
}
function Start-Reading {
  $ql.Visible = $false; $tl.Visible = $false
  $yes.Visible = $false; $no.Visible = $false
  $timer.Start()
  if ($readmode -eq 'consolein_async') {
    # BV-145's reader: [Console]::In is a SyncTextReader whose ReadLineAsync is
    # Task.FromResult(ReadLine()), so this first poll blocks the interface thread.
    $script:reader = [Console]::In.ReadLineAsync()
    return
  }
  if ($readmode -eq 'backgroundworker') {
    # v0.7.0's reader: the DoWork scriptblock runs on a thread-pool thread with no
    # runspace, so it fails ("There is no Runspace available...") and the worker
    # ends in error; ProgressChanged never fires and the window stays blank.
    $worker = New-Object System.ComponentModel.BackgroundWorker
    $worker.WorkerReportsProgress = $true
    $worker.Add_DoWork({
      while ($true) {
        $line = [Console]::In.ReadLine()
        if ($line -eq $null) { break }
        $worker.ReportProgress(0, $line)
      }
    })
    $worker.Add_ProgressChanged({ Handle-Line $_.UserState })
    $worker.Add_RunWorkerCompleted({
      if ($selftest -and $_.Error -ne $null) { [Console]::Out.WriteLine('WORKERERROR=' + $_.Error.GetType().Name); [Console]::Out.Flush() }
    })
    $worker.RunWorkerAsync()
    return
  }
  # The fix: a dedicated runspace reads [Console]::In on its own thread into $q,
  # ending with the marker. The interface thread only drains $q (from the Timer).
  $script:rs = [runspacefactory]::CreateRunspace()
  $script:rs.Open()
  $script:rs.SessionStateProxy.SetVariable('q', $script:q)
  $script:rs.SessionStateProxy.SetVariable('eofmarker', $script:eof)
  $script:rsps = [powershell]::Create()
  $script:rsps.Runspace = $script:rs
  [void]$script:rsps.AddScript('while ($null -ne ($l = [Console]::In.ReadLine())) { $q.Enqueue($l) }; $q.Enqueue($eofmarker)')
  [void]$script:rsps.BeginInvoke()
}
$timer.Add_Tick({
  if ($selftest) { [Console]::Out.WriteLine('TICK'); [Console]::Out.Flush() }
  if ($readmode -eq 'backgroundworker') { return }
  if ($readmode -eq 'consolein_async') {
    if ($script:reader -ne $null -and $script:reader.IsCompleted) {
      $line = $script:reader.Result
      if ($line -eq $null) { $timer.Stop(); $script:reader = $null; return }
      Handle-Line $line
      if ($script:reader -ne $null) { $script:reader = [Console]::In.ReadLineAsync() }
    }
    return
  }
  $line = $null
  while ($script:q.TryDequeue([ref]$line)) {
    if ($line -eq $script:eof) { $timer.Stop(); return }
    Handle-Line $line
  }
})
$yes.Add_Click({
  $script:answered = $true
  [Console]::Out.WriteLine('connect')
  [Console]::Out.Flush()
  Start-Reading
})
$no.Add_Click({ $form.Close() })
$ok.Add_Click({ [Console]::Out.WriteLine('ok'); [Console]::Out.Flush(); $form.Close() })
if ($selftest) {
  # Drive the window without a click: prove it painted (Shown), then read, so a
  # headless CI run can check the number appears and the Timer keeps ticking
  # while no line has arrived yet (the interface thread is alive). Test only.
  $form.Add_Shown({
    [Console]::Out.WriteLine('SHOWN')
    [Console]::Out.Flush()
    $script:answered = $true
    Start-Reading
  })
}
[void]$form.ShowDialog()
# Stop the reader runspace on close; closing stdin (the caller does) ends its
# ReadLine, so Stop returns without hanging. Guarded so close never fails.
try {
  if ($script:rsps -ne $null) { $script:rsps.Stop() }
  if ($script:rs -ne $null) { $script:rs.Dispose() }
} catch { }
if (-not $script:answered) { [Console]::Out.WriteLine('no'); [Console]::Out.Flush() }`

// maybeConnectWindow shows the connect window on a Windows double click when
// this computer is not connected, and runs the protocol if the person agrees
// (BV-137 point 5). Any failure to show the window is logged and ignored: the
// run goes on, and `rosmarops connect` is always available.
func maybeConnectWindow(paths spacePaths, cat catalog.Catalog) {
	if isSpaceConnected(paths) {
		return // already connected (permission AND credential): no window
	}
	// PowerShell by absolute path, with PSModulePath narrowed to the system
	// modules, like the folder picker and the notifier (CTO-15 PR4, BV-93 B4):
	// a tampered PATH or profile cannot substitute the interpreter or its
	// modules.
	ps := winpath.PowerShellExe()
	if ps == "" {
		ps = "powershell"
	}
	cmd := exec.Command(ps, connectWindowArgs()...)
	cmd.Env = winpath.ChildEnv(os.Environ())
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
	// If nobody answers the window within 10 minutes, close it as «Not now»
	// and let the run go on (CTO-15 PR5). Killing the process closes its
	// stdout, so driveConnectWindow returns.
	done := make(chan bool, 1)
	go func() { done <- driveConnectWindow(stdout, stdin, run) }()
	select {
	case connected := <-done:
		if connected {
			slog.Info("connect window: this computer is now connected")
		}
	case <-time.After(10 * time.Minute):
		slog.Info("connect window: no answer in 10 minutes; closing as «Not now» and continuing")
		_ = cmd.Process.Kill()
		<-done
	}
	_ = stdin.Close()
	_ = cmd.Wait()
}
