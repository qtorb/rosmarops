//go:build windows

package main

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/winpath"
)

// dismissCard is one finding as the window draws it (BV-184 §4). It carries no
// secret and no graph detail; only what the person reads.
type dismissCard struct {
	ID    string `json:"id"`
	Sev   string `json:"sev"`   // "red" or "yellow"
	Title string `json:"title"` // the finding's title
	What  string `json:"what"`  // the report's «what it means»
	When  string `json:"when"`  // findings.when.<rule>
	New   bool   `json:"new"`   // a new finding
	Since string `json:"since"` // the localized «open since» sentence; empty when New
	Agent string `json:"agent"` // the agent, or empty for a machine finding
	Group string `json:"group"` // "new", "rest" or "dismissed"
	// Dismissed is whether the checkbox starts ticked (the dismissed group).
	Dismissed bool `json:"dismissed"`
}

// dismissWindowConfig is everything the window draws, all text from the
// catalog, passed as base64 JSON on the first stdin line (BV-184 §4).
type dismissWindowConfig struct {
	Title        string        `json:"title"`
	Q            string        `json:"q"`
	Text         string        `json:"text"`
	GroupNew     string        `json:"group_new"`
	GroupRest    string        `json:"group_rest"`
	GroupDismiss string        `json:"group_dismissed"`
	Red          string        `json:"red"`
	Yellow       string        `json:"yellow"`
	SubNew       string        `json:"sub_new"`
	WhatLabel    string        `json:"what_label"`
	WhenLabel    string        `json:"when_label"`
	CountZero    string        `json:"count_zero"`
	CountOne     string        `json:"count_one"`
	CountOther   string        `json:"count_other"`
	No           string        `json:"no"`
	Save         string        `json:"save"`
	Cards        []dismissCard `json:"cards"`
	Icon         string        `json:"icon"`
	IconFallback string        `json:"iconfallback"`
}

// dismissWindowConfigFrom builds the window config from the finding cards and
// the catalog. The per-card «open since» subtitle is resolved here (with the
// localized long date already in the card), so the window only draws strings.
func dismissWindowConfigFrom(cat catalog.Catalog, cards []report.FindingCard) dismissWindowConfig {
	icon, _ := os.Executable()
	out := dismissWindowConfig{
		Title:        "RosmarOps",
		Q:            cat.Text("findings.q", nil),
		Text:         cat.Text("findings.text", nil),
		GroupNew:     cat.Text("findings.group.new", nil),
		GroupRest:    cat.Text("findings.group.rest", nil),
		GroupDismiss: cat.Text("findings.group.dismissed", nil),
		Red:          cat.Text("findings.red", nil),
		Yellow:       cat.Text("findings.yellow", nil),
		SubNew:       cat.Text("findings.sub.new", nil),
		WhatLabel:    cat.Text("findings.what", nil),
		WhenLabel:    cat.Text("findings.when", nil),
		CountZero:    cat.Text("findings.count.zero", nil),
		CountOne:     cat.Text("findings.count.one", nil),
		CountOther:   cat.Text("findings.count.other", nil),
		No:           cat.Text("findings.no", nil),
		Save:         cat.Text("findings.save", nil),
		Icon:         icon,
	}
	for _, c := range cards {
		sub := ""
		if !c.New && c.Since != "" {
			sub = cat.Text("findings.sub.since", map[string]string{"date": c.Since})
		}
		out.Cards = append(out.Cards, dismissCard{
			ID: c.ID, Sev: c.Severity, Title: c.Title, What: c.WhatItMeans, When: c.WhenApplies,
			New: c.New, Since: sub, Agent: c.Agent, Group: c.Group,
			Dismissed: c.Group == report.CardGroupDismissed,
		})
	}
	return out
}

func dismissWindowInput(c dismissWindowConfig) string {
	b, _ := json.Marshal(c)
	return base64.StdEncoding.EncodeToString(b)
}

func dismissWindowArgs() []string {
	return []string{"-NoProfile", "-NonInteractive", "-STA", "-Command", dismissWindowScript}
}

// driveDismissWindow reads the window's answer: "save" then a line of the
// space-separated ids that are ticked (possibly empty), or "cancel" (or a
// closed stream) for «Not now». It returns the ticked ids and whether saved.
func driveDismissWindow(out io.Reader) ([]string, bool) {
	r := bufio.NewReader(out)
	first, _ := r.ReadString('\n')
	if strings.TrimRight(first, "\r\n") != "save" {
		return nil, false
	}
	ids, _ := r.ReadString('\n')
	return strings.Fields(ids), true
}

// showDismiss shows the «No me afecta» window on Windows and returns the ids
// the person left ticked and whether they saved. Any failure to show the
// window is logged and treated as «Not now» (nil, false).
func showDismiss(cards []report.FindingCard, cat catalog.Catalog) ([]string, bool) {
	ps := winpath.PowerShellExe()
	if ps == "" {
		ps = "powershell"
	}
	cmd := exec.Command(ps, dismissWindowArgs()...)
	cmd.Env = winpath.ChildEnv(os.Environ())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, false
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, false
	}
	if err := cmd.Start(); err != nil {
		slog.Info("dismiss window not shown", "err", err)
		return nil, false
	}
	if _, err := stdin.Write([]byte(dismissWindowInput(dismissWindowConfigFrom(cat, cards)) + "\n")); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, false
	}
	type res struct {
		ids   []string
		saved bool
	}
	done := make(chan res, 1)
	go func() {
		ids, saved := driveDismissWindow(stdout)
		done <- res{ids, saved}
	}()
	var out res
	select {
	case out = <-done:
	case <-time.After(15 * time.Minute):
		slog.Info("dismiss window: no answer in 15 minutes; closing as «Not now»")
		_ = cmd.Process.Kill()
		<-done
	}
	_ = stdin.Close()
	_ = cmd.Wait()
	return out.ids, out.saved
}

// dismissWindowScript shows the «No me afecta» window (BV-184 §4) with
// System.Windows.Forms, reading its base64 JSON config from the first stdin
// line and writing the result to stdout: "save" and then the ticked ids, or
// "cancel". It follows the connect window's one scale mechanism (AutoScaleMode
// Dpi in production; a manual $f for the CI screenshots) and its DPI-awareness.
//
// The list is a FlowLayoutPanel (TopDown, no wrap, vertical AutoScroll). Each
// row is a Panel that draws itself — the checkbox, the severity pill, the «i»
// and the title/subtitle (and the two detail texts when open) — so nothing
// relies on transparent child labels, which rendered blank. Every row's width
// is set to the list's client width (which already excludes the vertical
// scrollbar), so the rows fill the panel and no horizontal scrollbar appears.
const dismissWindowScript = `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
$cfg = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String([Console]::In.ReadLine().Trim())) | ConvertFrom-Json
try {
  Add-Type -Namespace RO -Name Dpi -MemberDefinition '[DllImport("user32.dll")] public static extern bool SetProcessDpiAwarenessContext(System.IntPtr value);'
  [void][RO.Dpi]::SetProcessDpiAwarenessContext([System.IntPtr](-4))
} catch {
  try {
    Add-Type -Namespace RO -Name Dpi2 -MemberDefinition '[DllImport("user32.dll")] public static extern bool SetProcessDPIAware();'
    [void][RO.Dpi2]::SetProcessDPIAware()
  } catch { }
}
$shot = $env:ROSMAROPS_FINDINGS_WINDOW_SHOT
$shotScale = $env:ROSMAROPS_FINDINGS_WINDOW_SHOT_SCALE
$shotOpenI = $env:ROSMAROPS_FINDINGS_WINDOW_SHOT_OPEN_I
$selftest = -not [string]::IsNullOrEmpty($env:ROSMAROPS_FINDINGS_WINDOW_SELFTEST)
$f = 1.0
if ($shot -and $shotScale) { $f = [double]$shotScale }
function Px($n) { return [int][math]::Round([double]$n * $f) }
$cWhite   = [System.Drawing.ColorTranslator]::FromHtml('#FFFFFF')
$cInk     = [System.Drawing.ColorTranslator]::FromHtml('#1B1A18')
$cMuted   = [System.Drawing.ColorTranslator]::FromHtml('#4F4C46')
$cSub     = [System.Drawing.ColorTranslator]::FromHtml('#66635D')
$cDetail  = [System.Drawing.ColorTranslator]::FromHtml('#3A3833')
$cRowTop  = [System.Drawing.ColorTranslator]::FromHtml('#EFEDE7')
$cHover   = [System.Drawing.ColorTranslator]::FromHtml('#F6F5F2')
$cBox     = [System.Drawing.ColorTranslator]::FromHtml('#8A867E')
$cCobalt  = [System.Drawing.ColorTranslator]::FromHtml('#2346B8')
$cRedTx   = [System.Drawing.ColorTranslator]::FromHtml('#9A2B17')
$cRedBg   = [System.Drawing.ColorTranslator]::FromHtml('#FBE9E5')
$cYelTx   = [System.Drawing.ColorTranslator]::FromHtml('#6B4700')
$cYelBg   = [System.Drawing.ColorTranslator]::FromHtml('#FDF5DF')
$fTitle   = New-Object System.Drawing.Font('Segoe UI Semibold', [single](15 * $f))
$fBody    = New-Object System.Drawing.Font('Segoe UI', [single](10.5 * $f))
$fHdr     = New-Object System.Drawing.Font('Segoe UI Semibold', [single](8.5 * $f))
$fRow     = New-Object System.Drawing.Font('Segoe UI', [single](10.5 * $f))
$fSub     = New-Object System.Drawing.Font('Segoe UI', [single](9 * $f))
$fSubB    = New-Object System.Drawing.Font('Segoe UI Semibold', [single](9 * $f))
$fPill    = New-Object System.Drawing.Font('Segoe UI Semibold', [single](9 * $f))
$fDetail  = New-Object System.Drawing.Font('Segoe UI', [single](10 * $f))
$fDetailB = New-Object System.Drawing.Font('Segoe UI Semibold', [single](10 * $f))
$fCount   = New-Object System.Drawing.Font('Segoe UI', [single](9.75 * $f))
$fBtnP    = New-Object System.Drawing.Font('Segoe UI Semibold', [single](10.5 * $f))
$fBtnS    = New-Object System.Drawing.Font('Segoe UI', [single](10.5 * $f))
$fIcon    = New-Object System.Drawing.Font('Segoe UI', [single](9 * $f), [System.Drawing.FontStyle]::Italic)
$noPad    = [System.Windows.Forms.TextFormatFlags]::NoPadding
$titleFlags = [System.Windows.Forms.TextFormatFlags]::WordBreak -bor [System.Windows.Forms.TextFormatFlags]::EndEllipsis -bor [System.Windows.Forms.TextFormatFlags]::NoPadding

$form = New-Object System.Windows.Forms.Form
$form.Text = $cfg.title
if ($shot) { $form.AutoScaleMode = [System.Windows.Forms.AutoScaleMode]::None }
else { $form.AutoScaleMode = [System.Windows.Forms.AutoScaleMode]::Dpi; $form.AutoScaleDimensions = New-Object System.Drawing.SizeF(96, 96) }
$form.TopMost = $true
$form.FormBorderStyle = 'FixedSingle'
$form.StartPosition = 'CenterScreen'
$form.BackColor = $cWhite
$form.MaximizeBox = $false
$form.MinimizeBox = $false
$form.ClientSize = New-Object System.Drawing.Size((Px 640), (Px 560))
$iconSet = $false
if ($cfg.icon -and (Test-Path -LiteralPath $cfg.icon)) {
  try { $ic = [System.Drawing.Icon]::ExtractAssociatedIcon($cfg.icon); if ($ic -ne $null) { $form.Icon = $ic; $iconSet = $true } } catch { }
}
if (-not $iconSet -and $cfg.iconfallback -and (Test-Path -LiteralPath $cfg.iconfallback)) {
  try { $form.Icon = New-Object System.Drawing.Icon($cfg.iconfallback); $iconSet = $true } catch { }
}

# Title (findings.q) and body (findings.text), 584 wide, 28 margins.
$ql = New-Object System.Windows.Forms.Label
$ql.AutoSize = $true; $ql.MaximumSize = New-Object System.Drawing.Size((Px 584), 0)
$ql.Font = $fTitle; $ql.ForeColor = $cInk; $ql.BackColor = $cWhite; $ql.Text = $cfg.q
$ql.Location = New-Object System.Drawing.Point((Px 28), (Px 28))
$form.Controls.Add($ql)
$tl = New-Object System.Windows.Forms.Label
$tl.AutoSize = $true; $tl.MaximumSize = New-Object System.Drawing.Size((Px 584), 0)
$tl.Font = $fBody; $tl.ForeColor = $cMuted; $tl.BackColor = $cWhite; $tl.Text = $cfg.text
$tl.Location = New-Object System.Drawing.Point((Px 28), ($ql.Bottom + (Px 12)))
$form.Controls.Add($tl)

# Footer buttons (findings.no / findings.save), 40 tall, 28 from the edges.
function New-FlatButton($text, $font, $back, $fore, $borderSize, $primary) {
  $b = New-Object System.Windows.Forms.Button
  $b.Text = $text; $b.Font = $font
  $b.FlatStyle = [System.Windows.Forms.FlatStyle]::Flat
  $b.FlatAppearance.BorderSize = $borderSize
  if ($borderSize -gt 0) { $b.FlatAppearance.BorderColor = [System.Drawing.ColorTranslator]::FromHtml('#C9C5BC') }
  $b.BackColor = $back; $b.ForeColor = $fore; $b.AutoSize = $false
  $m = [System.Windows.Forms.TextRenderer]::MeasureText($text, $font)
  $b.Height = Px 40; $b.Width = $m.Width + (Px 36)
  if ($primary) { $b.Cursor = [System.Windows.Forms.Cursors]::Hand }
  $form.Controls.Add($b)
  return $b
}
$save = New-FlatButton $cfg.save $fBtnP $cCobalt $cWhite 0 $true
$save.FlatAppearance.MouseOverBackColor = $cInk
$save.FlatAppearance.MouseDownBackColor = $cInk
$no = New-FlatButton $cfg.no $fBtnS $cWhite $cInk 1 $false
$btnY = (Px 560) - (Px 28) - (Px 40)
$save.Location = New-Object System.Drawing.Point(((Px 640) - (Px 28) - $save.Width), $btnY)
$no.Location = New-Object System.Drawing.Point(($save.Left - (Px 12) - $no.Width), $btnY)
$form.AcceptButton = $save
$form.CancelButton = $no

# Count label (findings.count), left, centered with the buttons.
$countl = New-Object System.Windows.Forms.Label
$countl.AutoSize = $true; $countl.Font = $fCount; $countl.ForeColor = $cMuted; $countl.BackColor = $cWhite
$form.Controls.Add($countl)

# The list: a FlowLayoutPanel inside a 1px-bordered host, between the body and
# the buttons. The window never grows; the list scrolls vertically.
$listTop = $tl.Bottom + (Px 20)
$listBottom = $btnY - (Px 20)
$listHost = New-Object System.Windows.Forms.Panel
$listHost.Location = New-Object System.Drawing.Point((Px 28), $listTop)
$listHost.Size = New-Object System.Drawing.Size((Px 584), ($listBottom - $listTop))
$listHost.BackColor = $cWhite
$listHost.BorderStyle = [System.Windows.Forms.BorderStyle]::FixedSingle
$form.Controls.Add($listHost)
$list = New-Object System.Windows.Forms.FlowLayoutPanel
$list.Dock = [System.Windows.Forms.DockStyle]::Fill
$list.FlowDirection = [System.Windows.Forms.FlowDirection]::TopDown
$list.WrapContents = $false
$list.AutoScroll = $true
$list.BackColor = $cWhite
$list.Padding = New-Object System.Windows.Forms.Padding(0)
$listHost.Controls.Add($list)

$script:checked = @{}
$script:openRow = $null
$vsb = [System.Windows.Forms.SystemInformation]::VerticalScrollBarWidth
# A safe starting row width; Fit-Rows sets the exact client width once shown.
$script:rowW0 = (Px 584) - 2 - $vsb

function Update-Count {
  $n = 0
  foreach ($k in $script:checked.Keys) { if ($script:checked[$k]) { $n++ } }
  if ($n -eq 0) { $countl.Text = $cfg.count_zero }
  elseif ($n -eq 1) { $countl.Text = $cfg.count_one }
  else { $countl.Text = ($cfg.count_other -replace '\{n\}', $n) }
}

# Flow-Para draws (or, with $g = $null, only measures) a paragraph that starts
# with a Semibold label and continues in the normal font, wrapping within $w at
# a 1.45 line height. Returns the height used.
function Flow-Para($g, $x, $y, $w, $label, $body) {
  $lh = [int][math]::Ceiling($fDetail.GetHeight() * 1.45)
  $cx = $x; $cy = $y
  $segs = @()
  foreach ($word in ($label.Trim() -split '\s+')) { if ($word -ne '') { $segs += @{ t = $word; f = $fDetailB } } }
  foreach ($word in ($body.Trim() -split '\s+')) { if ($word -ne '') { $segs += @{ t = $word; f = $fDetail } } }
  foreach ($seg in $segs) {
    $wd = [System.Windows.Forms.TextRenderer]::MeasureText($seg.t + ' ', $seg.f, [System.Drawing.Size]::Empty, $noPad).Width
    if (($cx + $wd) -gt ($x + $w) -and $cx -gt $x) { $cx = $x; $cy += $lh }
    if ($g -ne $null) {
      [System.Windows.Forms.TextRenderer]::DrawText($g, $seg.t, $seg.f, (New-Object System.Drawing.Point($cx, $cy)), $cDetail, $noPad)
    }
    $cx += $wd
  }
  return ($cy + $lh - $y)
}

# Row metrics from a row's current width (so they adapt after Fit-Rows).
function Row-TitleTop { return (Px 12) }
function Row-TextX { return (Px 16) + (Px 18) + (Px 12) + (Px 76) + (Px 12) }
function Row-IconX($w) { return $w - (Px 16) - (Px 20) }
function Row-TextW($w) { return (Row-IconX $w) - (Px 12) - (Row-TextX) }
function Row-TitleH { return [int][math]::Ceiling($fRow.GetHeight() * 2) }
function Row-SubTop { return (Row-TitleTop) + (Row-TitleH) + (Px 2) }
function Row-SubH { return [int][math]::Ceiling($fSub.GetHeight()) }
function Row-BaseH { return [System.Math]::Max((Px 56), (Row-SubTop) + (Row-SubH) + (Px 12)) }
function Row-DetailTop { return (Row-SubTop) + (Row-SubH) + (Px 8) }

function Row-DetailHeight($card) {
  $w = (Row-TextW $card.__w)
  $h1 = Flow-Para $null (Row-TextX) (Row-DetailTop) $w $cfg.what_label $card.what
  $lh = [int][math]::Ceiling($fDetail.GetHeight() * 1.45)
  $h2 = Flow-Para $null (Row-TextX) ((Row-DetailTop) + $h1 + $lh) $w $cfg.when_label $card.when
  return ($h1 + $lh + $h2)
}

function Add-Header($text) {
  $h = New-Object System.Windows.Forms.Label
  $h.AutoSize = $false
  $h.Font = $fHdr; $h.ForeColor = $cMuted; $h.BackColor = $cWhite
  $h.Text = $text.ToUpper()
  $h.TextAlign = [System.Drawing.ContentAlignment]::BottomLeft
  $h.Margin = New-Object System.Windows.Forms.Padding(0)
  $h.Padding = New-Object System.Windows.Forms.Padding((Px 16), (Px 12), 0, (Px 6))
  $h.Width = $script:rowW0
  $h.Height = (Px 12) + [int][math]::Ceiling($fHdr.GetHeight()) + (Px 6)
  $list.Controls.Add($h)
}

function Add-Row($card) {
  $row = New-Object System.Windows.Forms.Panel
  $row.Margin = New-Object System.Windows.Forms.Padding(0)
  $row.Width = $script:rowW0
  $row.BackColor = $cWhite
  $script:checked[$card.id] = [bool]$card.dismissed
  $state = @{ card = $card; open = $false }
  $card | Add-Member -NotePropertyName __w -NotePropertyValue $script:rowW0 -Force
  $row.Tag = $state
  $row.Height = (Row-BaseH)
  $row.Add_Paint({
    param($s, $e)
    $g = $e.Graphics
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $g.TextRenderingHint = [System.Drawing.Text.TextRenderingHint]::ClearTypeGridFit
    $st = $s.Tag; $card = $st.card; $w = $s.ClientSize.Width
    # Top border.
    $g.DrawLine((New-Object System.Drawing.Pen($cRowTop, 1)), 0, 0, $w, 0)
    # Checkbox.
    $bx = Px 16; $by = Px 12; $bd = Px 18
    $b = New-Object System.Drawing.Rectangle($bx, $by, $bd, $bd)
    if ($script:checked[$card.id]) {
      $g.FillRectangle((New-Object System.Drawing.SolidBrush($cCobalt)), $b)
      $g.DrawRectangle((New-Object System.Drawing.Pen($cCobalt, 1)), $b)
      $pen = New-Object System.Drawing.Pen($cWhite, [single](2 * $f))
      $g.DrawLines($pen, @(
        (New-Object System.Drawing.Point(($bx + [int]($bd * 0.22)), ($by + [int]($bd * 0.52)))),
        (New-Object System.Drawing.Point(($bx + [int]($bd * 0.42)), ($by + [int]($bd * 0.72)))),
        (New-Object System.Drawing.Point(($bx + [int]($bd * 0.78)), ($by + [int]($bd * 0.30))))))
    } else {
      $g.FillRectangle((New-Object System.Drawing.SolidBrush($cWhite)), $b)
      $g.DrawRectangle((New-Object System.Drawing.Pen($cBox, 1)), $b)
    }
    # Severity pill, fixed 76-wide column.
    $txt = $cfg.red; $tx = $cRedTx; $bg = $cRedBg
    if ($card.sev -eq 'yellow') { $txt = $cfg.yellow; $tx = $cYelTx; $bg = $cYelBg }
    $pillX = $bx + $bd + (Px 12)
    $pm = [System.Windows.Forms.TextRenderer]::MeasureText($txt, $fPill, [System.Drawing.Size]::Empty, $noPad)
    $ph = $pm.Height + (Px 4)
    $pr = New-Object System.Drawing.Rectangle($pillX, (Px 12), (Px 76), $ph)
    $rad = Px 9
    $path = New-Object System.Drawing.Drawing2D.GraphicsPath
    $path.AddArc($pr.X, $pr.Y, ($rad * 2), ($rad * 2), 180, 90)
    $path.AddArc(($pr.Right - $rad * 2), $pr.Y, ($rad * 2), ($rad * 2), 270, 90)
    $path.AddArc(($pr.Right - $rad * 2), ($pr.Bottom - $rad * 2), ($rad * 2), ($rad * 2), 0, 90)
    $path.AddArc($pr.X, ($pr.Bottom - $rad * 2), ($rad * 2), ($rad * 2), 90, 90)
    $path.CloseFigure()
    $g.FillPath((New-Object System.Drawing.SolidBrush($bg)), $path)
    [System.Windows.Forms.TextRenderer]::DrawText($g, $txt, $fPill, (New-Object System.Drawing.Point(($pillX + (Px 8)), ((Px 12) + [int](($ph - $pm.Height) / 2)))), $tx, $noPad)
    # «i» circle.
    $iconX = Row-IconX $w; $iconD = Px 20
    $ir = New-Object System.Drawing.Rectangle($iconX, (Px 12), $iconD, $iconD)
    if ($st.open) {
      $g.FillEllipse((New-Object System.Drawing.SolidBrush($cInk)), $ir)
      [System.Windows.Forms.TextRenderer]::DrawText($g, 'i', $fIcon, $ir, $cWhite, ([System.Windows.Forms.TextFormatFlags]::HorizontalCenter -bor [System.Windows.Forms.TextFormatFlags]::VerticalCenter))
    } else {
      $g.DrawEllipse((New-Object System.Drawing.Pen($cBox, 1)), $ir)
      [System.Windows.Forms.TextRenderer]::DrawText($g, 'i', $fIcon, $ir, $cMuted, ([System.Windows.Forms.TextFormatFlags]::HorizontalCenter -bor [System.Windows.Forms.TextFormatFlags]::VerticalCenter))
    }
    # Title (up to two lines, ellipsis).
    $textX = Row-TextX; $textW = Row-TextW $w
    $trect = New-Object System.Drawing.Rectangle($textX, (Row-TitleTop), $textW, (Row-TitleH))
    [System.Windows.Forms.TextRenderer]::DrawText($g, $card.title, $fRow, $trect, $cInk, $titleFlags)
    # Subtitle: New (cobalt, semibold) or «open since …», then · agent (muted).
    $sy = Row-SubTop
    $cx = $textX
    if ($card.new) {
      [System.Windows.Forms.TextRenderer]::DrawText($g, $cfg.sub_new, $fSubB, (New-Object System.Drawing.Point($cx, $sy)), $cCobalt, $noPad)
      $cx += [System.Windows.Forms.TextRenderer]::MeasureText($cfg.sub_new, $fSubB, [System.Drawing.Size]::Empty, $noPad).Width
    } elseif ($card.since) {
      [System.Windows.Forms.TextRenderer]::DrawText($g, $card.since, $fSub, (New-Object System.Drawing.Point($cx, $sy)), $cSub, $noPad)
      $cx += [System.Windows.Forms.TextRenderer]::MeasureText($card.since, $fSub, [System.Drawing.Size]::Empty, $noPad).Width
    }
    if ($card.agent) {
      $sep = ' · ' + $card.agent
      [System.Windows.Forms.TextRenderer]::DrawText($g, $sep, $fSub, (New-Object System.Drawing.Point($cx, $sy)), $cSub, $noPad)
    }
    # Detail, when open.
    if ($st.open) {
      $dy = Row-DetailTop
      $h1 = Flow-Para $g $textX $dy $textW $cfg.what_label $card.what
      $lh = [int][math]::Ceiling($fDetail.GetHeight() * 1.45)
      [void](Flow-Para $g $textX ($dy + $h1 + $lh) $textW $cfg.when_label $card.when)
    }
  })
  $row.Add_MouseEnter({ $this.BackColor = $cHover; $this.Invalidate() })
  $row.Add_MouseLeave({ $this.BackColor = $cWhite; $this.Invalidate() })
  $row.Add_MouseUp({
    param($s, $e)
    $st = $s.Tag; $card = $st.card; $w = $s.ClientSize.Width
    $iconX = Row-IconX $w; $iconD = Px 20
    if ($e.X -ge $iconX -and $e.X -le ($iconX + $iconD) -and $e.Y -ge (Px 12) -and $e.Y -le ((Px 12) + $iconD)) {
      Toggle-Detail $s; return
    }
    $script:checked[$card.id] = -not $script:checked[$card.id]
    Update-Count; $s.Invalidate()
  })
  $list.Controls.Add($row)
}

function Toggle-Detail($row) {
  $st = $row.Tag
  if ($script:openRow -ne $null -and $script:openRow -ne $row) {
    $o = $script:openRow.Tag; $o.open = $false
    $script:openRow.Height = (Row-BaseH); $script:openRow.Invalidate()
  }
  $st.open = -not $st.open
  if ($st.open) {
    $st.card.__w = $row.ClientSize.Width
    $row.Height = (Row-DetailTop) + (Row-DetailHeight $st.card) + (Px 12)
    $script:openRow = $row
  } else {
    $row.Height = (Row-BaseH)
    $script:openRow = $null
  }
  $row.Invalidate()
}

# Fit every row (and header) to the list's client width, which already excludes
# the vertical scrollbar, so the rows fill the panel and no horizontal scrollbar
# appears.
function Fit-Rows {
  $w = $list.ClientSize.Width
  foreach ($c in $list.Controls) {
    $c.Width = $w
    if ($c.Tag -ne $null) { $c.Tag.card.__w = $w }
    $c.Invalidate()
  }
}

$groups = @(
  @{ name = 'new'; label = $cfg.group_new },
  @{ name = 'rest'; label = $cfg.group_rest },
  @{ name = 'dismissed'; label = $cfg.group_dismissed }
)
foreach ($grp in $groups) {
  $rows = @($cfg.cards | Where-Object { $_.group -eq $grp.name })
  if ($rows.Count -eq 0) { continue }
  Add-Header $grp.label
  foreach ($c in $rows) { Add-Row $c }
}
Update-Count
$countl.Location = New-Object System.Drawing.Point((Px 28), ($btnY + [int]((((Px 40)) - $countl.Height) / 2)))

$script:result = 'cancel'
$save.Add_Click({
  $ids = @()
  foreach ($k in $script:checked.Keys) { if ($script:checked[$k]) { $ids += $k } }
  [Console]::Out.WriteLine('save')
  [Console]::Out.WriteLine([string]::Join(' ', $ids))
  [Console]::Out.Flush()
  $script:result = 'save'
  $form.Close()
})
$no.Add_Click({ $form.Close() })

if ($shot) {
  $form.Add_Shown({
    Fit-Rows
    if ($shotOpenI) {
      foreach ($c in $list.Controls) { if ($c.Tag -ne $null) { Toggle-Detail $c; break } }
    }
    Start-Sleep -Milliseconds 600
    $b = $form.Bounds
    $bmp = New-Object System.Drawing.Bitmap($b.Width, $b.Height)
    $g = [System.Drawing.Graphics]::FromImage($bmp)
    $g.CopyFromScreen($b.Location, [System.Drawing.Point]::Empty, $b.Size)
    $bmp.Save($shot, [System.Drawing.Imaging.ImageFormat]::Png)
    $g.Dispose(); $bmp.Dispose()
    $form.Close()
  })
}
if ($selftest) {
  $form.Add_Shown({
    Fit-Rows
    $rows = @($list.Controls | Where-Object { $_.Tag -ne $null })
    $maxRight = 0
    foreach ($r in $rows) { if (($r.Left + $r.Width) -gt $maxRight) { $maxRight = $r.Left + $r.Width } }
    $nchecked = 0
    foreach ($k in $script:checked.Keys) { if ($script:checked[$k]) { $nchecked++ } }
    [Console]::Out.WriteLine('SHOWN')
    [Console]::Out.WriteLine('CLIENT=' + $form.ClientSize.Width + ',' + $form.ClientSize.Height)
    [Console]::Out.WriteLine('LISTCLIENT=' + $list.ClientSize.Width + ',' + $list.ClientSize.Height)
    [Console]::Out.WriteLine('ROWS=' + $rows.Count)
    [Console]::Out.WriteLine('ROWMAXRIGHT=' + $maxRight)
    [Console]::Out.WriteLine('HSCROLL=' + $list.HorizontalScroll.Visible)
    [Console]::Out.WriteLine('CHECKED=' + $nchecked)
    [Console]::Out.WriteLine('COUNT=' + $countl.Text)
    [Console]::Out.WriteLine('BORDER=' + $form.FormBorderStyle + ' MAX=' + $form.MaximizeBox + ' MIN=' + $form.MinimizeBox + ' TOP=' + $form.TopMost)
    [Console]::Out.WriteLine('FORMBACK=' + $form.BackColor.ToArgb())
    [Console]::Out.WriteLine('SAVEBACK=' + $save.BackColor.ToArgb())
    [Console]::Out.WriteLine('PT=' + $ql.Font.SizeInPoints + ',' + $tl.Font.SizeInPoints)
    if ($rows.Count -gt 0) {
      $r0 = $rows[0]
      $hbefore = $r0.Height
      Toggle-Detail $r0
      [Console]::Out.WriteLine('IOPEN=' + $hbefore + ',' + $r0.Height)
      Toggle-Detail $r0
    }
    [Console]::Out.Flush()
    $form.Close()
  })
}
[void]$form.ShowDialog()
if ($script:result -ne 'save' -and -not $shot -and -not $selftest) { [Console]::Out.WriteLine('cancel'); [Console]::Out.Flush() }`
