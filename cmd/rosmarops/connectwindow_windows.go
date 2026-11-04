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
	Done    string `json:"done"`
	Expired string `json:"expired"`
	Failed  string `json:"failed"`
	OK      string `json:"ok"`
	// Icon is the path of the running rosmarops.exe, so the window shows its
	// icon (BV-178). Empty when it cannot be resolved; the window then keeps the
	// default icon.
	Icon string `json:"icon"`
}

func connectWindowConfigFrom(cat catalog.Catalog) connectWindowConfig {
	icon, _ := os.Executable()
	return connectWindowConfig{
		Title:   "RosmarOps",
		Q:       cat.Text("connect.q", nil),
		Text:    cat.Text("connect.text", nil),
		Yes:     cat.Text("connect.yes", nil),
		No:      cat.Text("connect.no", nil),
		Done:    cat.Text("connect.done", nil),
		Expired: cat.Text("connect.expired", nil),
		Failed:  cat.Text("connect.failed", nil),
		OK:      cat.Text("connect.ok", nil),
		Icon:    icon,
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
# BV-178: make the process DPI-aware before any window exists, so the window is
# crisp at 125/150 % instead of a stretched bitmap. PerMonitorV2 (-4) on Windows
# 10 1703+; if that is not available, fall back to system-DPI aware. Both are
# wrapped so a locked-down host (no Add-Type) just keeps the default (the
# WinForms Add-Type above would already have failed there).
try {
  Add-Type -Namespace RO -Name Dpi -MemberDefinition '[DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(System.IntPtr value);'
  [void][RO.Dpi]::SetProcessDpiAwarenessContext([System.IntPtr](-4))
} catch {
  try {
    Add-Type -Namespace RO -Name Dpi2 -MemberDefinition '[DllImport("user32.dll")] public static extern bool SetProcessDPIAware();'
    [void][RO.Dpi2]::SetProcessDPIAware()
  } catch { }
}
# BV-178: the RosmarOps look — a white dialog, dark ink, a flat primary button
# and a flat outlined secondary, no grey chrome. All values are the 100 %
# (96-DPI) design; AutoScaleMode Dpi scales them for the monitor.
$cWhite  = [System.Drawing.ColorTranslator]::FromHtml('#FFFFFF')
$cInk    = [System.Drawing.ColorTranslator]::FromHtml('#1B1A18')
$cMuted  = [System.Drawing.ColorTranslator]::FromHtml('#4F4C46')
$cBorder = [System.Drawing.ColorTranslator]::FromHtml('#C9C5BC')
$fTitle  = New-Object System.Drawing.Font('Segoe UI Semibold', 15)
$fBody   = New-Object System.Drawing.Font('Segoe UI', 10.5)
$fBtnP   = New-Object System.Drawing.Font('Segoe UI Semibold', 10.5)
$fBtnS   = New-Object System.Drawing.Font('Segoe UI', 10.5)
$form = New-Object System.Windows.Forms.Form
$form.Text = $cfg.title
$form.AutoScaleMode = [System.Windows.Forms.AutoScaleMode]::Dpi
$form.AutoScaleDimensions = New-Object System.Drawing.SizeF(96, 96)
$form.TopMost = $true
$form.FormBorderStyle = 'FixedSingle'
$form.StartPosition = 'CenterScreen'
$form.BackColor = $cWhite
$form.ClientSize = New-Object System.Drawing.Size(480, 248)
$form.MaximizeBox = $false
$form.MinimizeBox = $false
if ($cfg.icon -and (Test-Path -LiteralPath $cfg.icon)) {
  try { $form.Icon = [System.Drawing.Icon]::ExtractAssociatedIcon($cfg.icon) } catch { }
}
$script:answered = $false
# Title (connect.q): Segoe UI Semibold 15, ink, wraps within 424 px.
$ql = New-Object System.Windows.Forms.Label
$ql.AutoSize = $true
$ql.MaximumSize = New-Object System.Drawing.Size(424, 0)
$ql.Font = $fTitle; $ql.ForeColor = $cInk; $ql.BackColor = $cWhite
$ql.Text = $cfg.q
$ql.Location = New-Object System.Drawing.Point(28, 28)
$form.Controls.Add($ql)
# Body (connect.text): Segoe UI 10.5, muted, 12 px under the title, 424 wide.
$tl = New-Object System.Windows.Forms.Label
$tl.AutoSize = $true
$tl.MaximumSize = New-Object System.Drawing.Size(424, 0)
$tl.Font = $fBody; $tl.ForeColor = $cMuted; $tl.BackColor = $cWhite
$tl.Text = $cfg.text
$tl.Location = New-Object System.Drawing.Point(28, $ql.Bottom + 12)
$form.Controls.Add($tl)
# The result text replaces the title/body once connecting starts; same muted
# body style, at the top-left content origin.
$resultl = New-Object System.Windows.Forms.Label
$resultl.AutoSize = $true
$resultl.MaximumSize = New-Object System.Drawing.Size(424, 0)
$resultl.Font = $fBody; $resultl.ForeColor = $cMuted; $resultl.BackColor = $cWhite
$resultl.Location = New-Object System.Drawing.Point(28, 28)
$resultl.Visible = $false
$form.Controls.Add($resultl)
# BV-164: the connect offer no longer shows a number. $numValue stays an
# invisible label, set only so the Windows self-test can prove the reader thread
# is alive (it echoes NUMLABEL); the person never sees it.
$numValue = New-Object System.Windows.Forms.Label
$numValue.AutoSize = $true; $numValue.Visible = $false
$form.Controls.Add($numValue)
# The window grows in height if the text needs it; the buttons never wrap.
$clientH = 248
$minH = $tl.Bottom + 16 + 40 + 28
if ($minH -gt $clientH) { $clientH = $minH }
$form.ClientSize = New-Object System.Drawing.Size(480, $clientH)
$btnY = $clientH - 28 - 40
# A flat button sized to its text (one line) plus 18 px each side, 40 px tall.
function New-FlatButton($text, $font, $back, $fore, $borderSize, $primary) {
  $b = New-Object System.Windows.Forms.Button
  $b.Text = $text
  $b.Font = $font
  $b.FlatStyle = [System.Windows.Forms.FlatStyle]::Flat
  $b.FlatAppearance.BorderSize = $borderSize
  if ($borderSize -gt 0) { $b.FlatAppearance.BorderColor = $cBorder }
  $b.BackColor = $back
  $b.ForeColor = $fore
  $b.AutoSize = $false
  $m = [System.Windows.Forms.TextRenderer]::MeasureText($text, $font)
  $b.Height = 40
  $b.Width = $m.Width + 36
  if ($primary) { $b.Cursor = [System.Windows.Forms.Cursors]::Hand }
  $form.Controls.Add($b)
  return $b
}
# Primary «Save to my space» (connect.yes) on the right; secondary «Not now»
# (connect.no) 12 px to its left.
$yes = New-FlatButton $cfg.yes $fBtnP $cInk $cWhite 0 $true
$no  = New-FlatButton $cfg.no  $fBtnS $cWhite $cInk 1 $false
$ok  = New-FlatButton $cfg.ok  $fBtnP $cInk $cWhite 0 $true
$yes.Location = New-Object System.Drawing.Point((480 - 28 - $yes.Width), $btnY)
$no.Location  = New-Object System.Drawing.Point(($yes.Left - 12 - $no.Width), $btnY)
$ok.Location  = New-Object System.Drawing.Point((480 - 28 - $ok.Width), $btnY)
$ok.Visible = $false
$form.AcceptButton = $yes
$form.CancelButton = $no
# BV-178: screenshot mode (CI only). ROSMAROPS_CONNECT_WINDOW_SHOT is the PNG
# path; _SCALE renders the window at that factor (1.5 emulates 150 % on a
# 96-DPI runner — the same multiply AutoScaleMode Dpi would apply). Never set in
# production.
$shot = $env:ROSMAROPS_CONNECT_WINDOW_SHOT
$shotScale = $env:ROSMAROPS_CONNECT_WINDOW_SHOT_SCALE
if ($shot -and $shotScale -and ([double]$shotScale -ne 1)) {
  $form.Scale((New-Object System.Drawing.SizeF([double]$shotScale, [double]$shotScale)))
}
if ($shot) {
  $form.Add_Shown({
    Start-Sleep -Milliseconds 500
    $b = $form.Bounds
    $bmp = New-Object System.Drawing.Bitmap($b.Width, $b.Height)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.CopyFromScreen($b.Location, [System.Drawing.Point]::Empty, $b.Size)
    $bmp.Save($shot, [System.Drawing.Imaging.ImageFormat]::Png)
    $g.Dispose(); $bmp.Dispose()
    $script:answered = $true
    $form.Close()
  })
}
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
  $resultl.Text = $text
  $resultl.Visible = $true
  $ok.Visible = $true
  if ($selftest) { [Console]::Out.WriteLine('RESULT=' + $text); [Console]::Out.Flush(); $form.Close() }
}
function Handle-Line($line) {
  $parts = ([string]$line).Split(' ', 2)
  switch ($parts[0]) {
    'number' {
      # BV-164: not shown; $numValue is set only for the self-test's liveness echo.
      $numValue.Text = $parts[1]
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
    [Console]::Out.WriteLine('DPI=' + $form.DeviceDpi)
    [Console]::Out.WriteLine('CLIENT=' + $form.ClientSize.Width + ',' + $form.ClientSize.Height)
    [Console]::Out.WriteLine('TITLE=' + $ql.Left + ',' + $ql.Top + ',' + $ql.Width + ',' + $ql.Height)
    [Console]::Out.WriteLine('BODY=' + $tl.Left + ',' + $tl.Top + ',' + $tl.Width + ',' + $tl.Height)
    [Console]::Out.WriteLine('YES=' + $yes.Left + ',' + $yes.Top + ',' + $yes.Width + ',' + $yes.Height)
    [Console]::Out.WriteLine('NO=' + $no.Left + ',' + $no.Top + ',' + $no.Width + ',' + $no.Height)
    [Console]::Out.WriteLine('BORDER=' + $form.FormBorderStyle + ' MAX=' + $form.MaximizeBox + ' MIN=' + $form.MinimizeBox + ' TOP=' + $form.TopMost)
    [Console]::Out.WriteLine('FORMBACK=' + $form.BackColor.ToArgb())
    [Console]::Out.WriteLine('YESBACK=' + $yes.BackColor.ToArgb() + ' YESFORE=' + $yes.ForeColor.ToArgb() + ' YESBORDER=' + $yes.FlatAppearance.BorderSize + ' YESFLAT=' + $yes.FlatStyle)
    [Console]::Out.WriteLine('NOBACK=' + $no.BackColor.ToArgb() + ' NOFORE=' + $no.ForeColor.ToArgb() + ' NOBORDER=' + $no.FlatAppearance.BorderSize + ' NOBORDERCOLOR=' + $no.FlatAppearance.BorderColor.ToArgb() + ' NOFLAT=' + $no.FlatStyle)
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

// connectOffer shows the connect window on a Windows double click and runs the
// protocol if the person agrees (BV-137 point 5; BV-158 part 4). Any failure to
// show the window is logged and ignored (connectNotOffered): the run goes on,
// and `rosmarops connect` is always available. The caller has already checked
// that this computer is not connected and that the offer is due.
func connectOffer(paths spacePaths, cat catalog.Catalog) connectResult {
	if isSpaceConnected(paths) {
		return connectNotOffered // already connected (permission AND credential): no window
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
		return connectNotOffered
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return connectNotOffered
	}
	if err := cmd.Start(); err != nil {
		slog.Info("connect window not shown", "err", err)
		return connectNotOffered
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
	done := make(chan connectResult, 1)
	go func() { done <- driveConnectWindow(stdout, stdin, run) }()
	result := connectDeclined
	select {
	case result = <-done:
	case <-time.After(10 * time.Minute):
		slog.Info("connect window: no answer in 10 minutes; closing as «Not now» and continuing")
		_ = cmd.Process.Kill()
		<-done
		result = connectDeclined
	}
	_ = stdin.Close()
	_ = cmd.Wait()
	return result
}
