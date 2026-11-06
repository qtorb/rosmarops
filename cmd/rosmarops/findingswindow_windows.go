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
	// EncProbe is a fixed string with non-ASCII characters used only by the
	// self-test to check the UTF-8 pipeline end to end.
	EncProbe string `json:"enc_probe"`
}

// dismissEncProbe is the fixed probe string (a middle dot, an á and an ñ).
const dismissEncProbe = "·áñ"

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
		EncProbe:     dismissEncProbe,
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
// System.Windows.Forms. Rows use ordinary controls: a bordered-Panel checkbox,
// an owner-drawn pill and «i» (rounded/elliptical fills drawn in Paint), the
// title and subtitle Labels, and a read-only borderless RichTextBox for the
// open detail (left-aligned text with the two Semibold labels inline). Capture
// (screenshots and the pixel self-test) goes through WM_PRINT, which renders the
// RichTextBox and the owner-drawn shapes and does not need the window fully
// on-screen. Non-ASCII text arrives as base64 UTF-8 JSON; the only separator
// literal is built from a char code.
const dismissWindowScript = `$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
try { [Console]::InputEncoding = [System.Text.Encoding]::UTF8; [Console]::OutputEncoding = [System.Text.Encoding]::UTF8 } catch { }
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
try {
  Add-Type -Namespace RO -Name Win -MemberDefinition '[DllImport("user32.dll")] public static extern int SendMessage(System.IntPtr hWnd, int Msg, System.IntPtr wParam, System.IntPtr lParam);'
} catch { }
$shot = $env:ROSMAROPS_FINDINGS_WINDOW_SHOT
$shotScale = $env:ROSMAROPS_FINDINGS_WINDOW_SHOT_SCALE
$shotOpenI = $env:ROSMAROPS_FINDINGS_WINDOW_SHOT_OPEN_I
$selftest = -not [string]::IsNullOrEmpty($env:ROSMAROPS_FINDINGS_WINDOW_SELFTEST)
$f = 1.0
if ($shot -and $shotScale) { $f = [double]$shotScale }
function Px($n) { return [int][math]::Round([double]$n * $f) }
$mid = ' ' + [char]0x00B7 + ' '
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
$fCheck   = New-Object System.Drawing.Font('Segoe UI', [single](11 * $f), [System.Drawing.FontStyle]::Bold)
$fIcon    = New-Object System.Drawing.Font('Segoe UI', [single](9 * $f), [System.Drawing.FontStyle]::Italic)
$noPad    = [System.Windows.Forms.TextFormatFlags]::NoPadding
$wordBreak = [System.Windows.Forms.TextFormatFlags]::WordBreak
$center   = ([System.Windows.Forms.TextFormatFlags]::HorizontalCenter -bor [System.Windows.Forms.TextFormatFlags]::VerticalCenter -bor $noPad)
$aTL = [System.Windows.Forms.AnchorStyles]::Top -bor [System.Windows.Forms.AnchorStyles]::Left
$aTLR = $aTL -bor [System.Windows.Forms.AnchorStyles]::Right
$aTR = [System.Windows.Forms.AnchorStyles]::Top -bor [System.Windows.Forms.AnchorStyles]::Right
$aBR = [System.Windows.Forms.AnchorStyles]::Bottom -bor [System.Windows.Forms.AnchorStyles]::Right
$aBL = [System.Windows.Forms.AnchorStyles]::Bottom -bor [System.Windows.Forms.AnchorStyles]::Left
$aFill = $aTLR -bor [System.Windows.Forms.AnchorStyles]::Bottom

function Rounded-Path($w, $h, $r) {
  $d = $r * 2
  if ($d -gt $w) { $d = $w }
  if ($d -gt $h) { $d = $h }
  $p = New-Object System.Drawing.Drawing2D.GraphicsPath
  $p.AddArc(0, 0, $d, $d, 180, 90)
  $p.AddArc($w - $d, 0, $d, $d, 270, 90)
  $p.AddArc($w - $d, $h - $d, $d, $d, 0, 90)
  $p.AddArc(0, $h - $d, $d, $d, 90, 90)
  $p.CloseFigure()
  return $p
}
function Rounded-Region($w, $h, $r) { return (New-Object System.Drawing.Region((Rounded-Path $w $h $r))) }

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
  try { $ic0 = [System.Drawing.Icon]::ExtractAssociatedIcon($cfg.icon); if ($ic0 -ne $null) { $form.Icon = $ic0; $iconSet = $true } } catch { }
}
if (-not $iconSet -and $cfg.iconfallback -and (Test-Path -LiteralPath $cfg.iconfallback)) {
  try { $form.Icon = New-Object System.Drawing.Icon($cfg.iconfallback); $iconSet = $true } catch { }
}

$ql = New-Object System.Windows.Forms.Label
$ql.AutoSize = $true; $ql.MaximumSize = New-Object System.Drawing.Size((Px 584), 0)
$ql.Font = $fTitle; $ql.ForeColor = $cInk; $ql.BackColor = $cWhite; $ql.Text = $cfg.q
$ql.Location = New-Object System.Drawing.Point((Px 28), (Px 28)); $ql.Anchor = $aTL
$form.Controls.Add($ql)
$tl = New-Object System.Windows.Forms.Label
$tl.AutoSize = $true; $tl.MaximumSize = New-Object System.Drawing.Size((Px 584), 0)
$tl.Font = $fBody; $tl.ForeColor = $cMuted; $tl.BackColor = $cWhite; $tl.Text = $cfg.text
$tl.Location = New-Object System.Drawing.Point((Px 28), ($ql.Bottom + (Px 12))); $tl.Anchor = $aTL
$form.Controls.Add($tl)

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
$save.Anchor = $aBR; $no.Anchor = $aBR
$form.AcceptButton = $save
$form.CancelButton = $no

$countl = New-Object System.Windows.Forms.Label
$countl.AutoSize = $true; $countl.Font = $fCount; $countl.ForeColor = $cMuted; $countl.BackColor = $cWhite
$countl.Anchor = $aBL
$form.Controls.Add($countl)

$listTop = $tl.Bottom + (Px 20)
$listBottom = $btnY - (Px 20)
$listHost = New-Object System.Windows.Forms.Panel
$listHost.Location = New-Object System.Drawing.Point((Px 28), $listTop)
$listHost.Size = New-Object System.Drawing.Size((Px 584), ($listBottom - $listTop))
$listHost.BackColor = $cWhite
$listHost.BorderStyle = [System.Windows.Forms.BorderStyle]::FixedSingle
$listHost.Anchor = $aFill
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
$script:rows = @()
$vsb = [System.Windows.Forms.SystemInformation]::VerticalScrollBarWidth
$rowW0 = (Px 584) - 2 - $vsb
$titleLineH = [int][math]::Ceiling($fRow.GetHeight())
$titleMaxH = $titleLineH * 2
$subH = [int][math]::Ceiling($fSub.GetHeight())
$textX = (Px 16) + (Px 18) + (Px 12) + (Px 76) + (Px 12)
$rightInset = (Px 16) + (Px 20) + (Px 12)
$pillH = Px 22

function Update-Count {
  $n = 0
  foreach ($k in $script:checked.Keys) { if ($script:checked[$k]) { $n++ } }
  if ($n -eq 0) { $countl.Text = $cfg.count_zero }
  elseif ($n -eq 1) { $countl.Text = $cfg.count_one }
  else { $countl.Text = ($cfg.count_other -replace '\{n\}', $n) }
}

function Set-Check($state) {
  if ([bool]$script:checked[$state.card.id]) {
    $state.cb.BackColor = $cCobalt
    $state.cb.BorderStyle = [System.Windows.Forms.BorderStyle]::None
    $state.tick.Visible = $true
  } else {
    $state.cb.BackColor = $cWhite
    $state.cb.BorderStyle = [System.Windows.Forms.BorderStyle]::FixedSingle
    $state.tick.Visible = $false
  }
}

function Set-RowBack($state, $col) {
  $state.row.BackColor = $col
  foreach ($c in @($state.title, $state.subStatus, $state.subAgent)) { if ($c -ne $null) { $c.BackColor = $col } }
}

function Add-Header($text) {
  $h = New-Object System.Windows.Forms.Label
  $h.AutoSize = $false
  $h.Font = $fHdr; $h.ForeColor = $cMuted; $h.BackColor = $cWhite
  $h.Text = $text.ToUpper()
  $h.TextAlign = [System.Drawing.ContentAlignment]::BottomLeft
  $h.Margin = New-Object System.Windows.Forms.Padding(0)
  $h.Padding = New-Object System.Windows.Forms.Padding((Px 16), (Px 12), 0, (Px 6))
  $h.Width = $rowW0
  $h.Height = (Px 12) + [int][math]::Ceiling($fHdr.GetHeight()) + (Px 6)
  $list.Controls.Add($h)
}

function Add-Row($card) {
  $colW = $rowW0 - $textX - $rightInset
  $th = [System.Windows.Forms.TextRenderer]::MeasureText($card.title, $fRow, (New-Object System.Drawing.Size($colW, 0)), $wordBreak).Height
  if ($th -gt $titleMaxH) { $th = $titleMaxH }
  if ($th -lt $titleLineH) { $th = $titleLineH }
  $rowSubTop = (Px 12) + $th + (Px 2)
  $baseH = [System.Math]::Max((Px 56), $rowSubTop + $subH + (Px 12))

  $row = New-Object System.Windows.Forms.Panel
  $row.Margin = New-Object System.Windows.Forms.Padding(0)
  $row.Width = $rowW0
  $row.Height = $baseH
  $row.BackColor = $cWhite
  $script:checked[$card.id] = [bool]$card.dismissed

  $bdr = New-Object System.Windows.Forms.Panel
  $bdr.Height = 1; $bdr.Dock = [System.Windows.Forms.DockStyle]::Top; $bdr.BackColor = $cRowTop
  $row.Controls.Add($bdr)

  $cb = New-Object System.Windows.Forms.Panel
  $cb.Size = New-Object System.Drawing.Size((Px 18), (Px 18))
  $cb.Location = New-Object System.Drawing.Point((Px 16), (Px 12))
  $cb.Anchor = $aTL
  $row.Controls.Add($cb)
  $tick = New-Object System.Windows.Forms.Label
  $tick.Text = [char]0x2713; $tick.Font = $fCheck; $tick.ForeColor = $cWhite; $tick.BackColor = $cCobalt
  $tick.Dock = [System.Windows.Forms.DockStyle]::Fill
  $tick.TextAlign = [System.Drawing.ContentAlignment]::MiddleCenter
  $tick.Visible = $false
  $cb.Controls.Add($tick)

  # Severity pill: owner-drawn rounded fill + centered text (radius 9, 22 tall).
  $pillTxt = $cfg.red; $pillTx = $cRedTx; $pillBg = $cRedBg
  if ($card.sev -eq 'yellow') { $pillTxt = $cfg.yellow; $pillTx = $cYelTx; $pillBg = $cYelBg }
  $pill = New-Object System.Windows.Forms.Panel
  $pill.Size = New-Object System.Drawing.Size((Px 76), $pillH)
  $pill.Location = New-Object System.Drawing.Point(((Px 16) + (Px 18) + (Px 12)), (Px 12))
  $pill.Anchor = $aTL
  $pill.BackColor = $cWhite
  $pill.Tag = @{ txt = $pillTxt; tx = $pillTx; bg = $pillBg }
  $pill.Add_Paint({
    param($s, $e)
    $t = $s.Tag
    $g = $e.Graphics; $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $w = $s.ClientSize.Width; $h = $s.ClientSize.Height
    $path = Rounded-Path $w $h (Px 9)
    $g.FillPath((New-Object System.Drawing.SolidBrush($t.bg)), $path)
    [System.Windows.Forms.TextRenderer]::DrawText($g, $t.txt, $fPill, (New-Object System.Drawing.Rectangle(0, 0, $w, $h)), $t.tx, $center)
  })
  $row.Controls.Add($pill)

  $title = New-Object System.Windows.Forms.Label
  $title.AutoSize = $false
  $title.Location = New-Object System.Drawing.Point($textX, (Px 12))
  $title.Size = New-Object System.Drawing.Size($colW, $th)
  $title.Anchor = $aTLR
  $title.Font = $fRow; $title.ForeColor = $cInk; $title.BackColor = $cWhite
  $title.AutoEllipsis = $true; $title.Text = $card.title
  $row.Controls.Add($title)

  # Subtitle. For a dated finding the whole line is one muted label joined by
  # « · »; for a new finding «New» is cobalt-bold and the agent muted.
  $subStatus = New-Object System.Windows.Forms.Label
  $subStatus.AutoSize = $true; $subStatus.BackColor = $cWhite
  $subStatus.Location = New-Object System.Drawing.Point($textX, $rowSubTop); $subStatus.Anchor = $aTL
  $subAgent = $null
  if ($card.new) {
    $subStatus.Font = $fSubB; $subStatus.ForeColor = $cCobalt; $subStatus.Text = $cfg.sub_new
    if ($card.agent) {
      $subAgent = New-Object System.Windows.Forms.Label
      $subAgent.AutoSize = $true; $subAgent.BackColor = $cWhite; $subAgent.Font = $fSub; $subAgent.ForeColor = $cSub
      $subAgent.Text = $mid + $card.agent; $subAgent.Anchor = $aTL
      $swid = [System.Windows.Forms.TextRenderer]::MeasureText($cfg.sub_new, $fSubB, [System.Drawing.Size]::Empty, $noPad).Width
      $subAgent.Location = New-Object System.Drawing.Point(($textX + $swid), $rowSubTop)
      $row.Controls.Add($subAgent)
    }
  } else {
    $parts = @()
    if ($card.since) { $parts += $card.since }
    if ($card.agent) { $parts += $card.agent }
    $subStatus.Font = $fSub; $subStatus.ForeColor = $cSub; $subStatus.Text = [string]::Join($mid, $parts)
  }
  $row.Controls.Add($subStatus)

  # «i»: owner-drawn circle. Closed: white fill, 1 px #8A867E border, muted «i».
  # Open: #1B1A18 fill, white «i». An elliptical Region clips it to a circle.
  $icon = New-Object System.Windows.Forms.Panel
  $icon.Size = New-Object System.Drawing.Size((Px 20), (Px 20))
  $icon.Location = New-Object System.Drawing.Point(($rowW0 - (Px 16) - (Px 20)), (Px 12))
  $icon.Anchor = $aTR
  $icon.BackColor = $cWhite
  $icon.Region = Rounded-Region (Px 20) (Px 20) (Px 10)
  $icon.Cursor = [System.Windows.Forms.Cursors]::Hand
  $icon.Add_Paint({
    param($s, $e)
    $st = $s.Tag
    $g = $e.Graphics; $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $d = $s.ClientSize.Width
    $rect = New-Object System.Drawing.Rectangle(0, 0, ($d - 1), ($d - 1))
    if ($st -ne $null -and $st.open) {
      $g.FillEllipse((New-Object System.Drawing.SolidBrush($cInk)), $rect)
      [System.Windows.Forms.TextRenderer]::DrawText($g, 'i', $fIcon, (New-Object System.Drawing.Rectangle(0, 0, $d, $d)), $cWhite, $center)
    } else {
      $g.FillEllipse((New-Object System.Drawing.SolidBrush($cWhite)), $rect)
      $g.DrawEllipse((New-Object System.Drawing.Pen($cBox, 1)), $rect)
      [System.Windows.Forms.TextRenderer]::DrawText($g, 'i', $fIcon, (New-Object System.Drawing.Rectangle(0, 0, $d, $d)), $cMuted, $center)
    }
  })
  $row.Controls.Add($icon)

  # Detail: a read-only, borderless, scroll-free RichTextBox; left-aligned text
  # with the two Semibold labels inline. Built lazily when first opened.
  $rtb = New-Object System.Windows.Forms.RichTextBox
  $rtb.BorderStyle = [System.Windows.Forms.BorderStyle]::None
  $rtb.ReadOnly = $true
  $rtb.ScrollBars = [System.Windows.Forms.RichTextBoxScrollBars]::None
  $rtb.BackColor = $cWhite
  $rtb.TabStop = $false
  $rtb.Cursor = [System.Windows.Forms.Cursors]::Default
  $rtb.Font = $fDetail
  $rtb.WordWrap = $true
  $rtb.Multiline = $true
  $rtb.Visible = $false
  $rtb.Width = $colW
  $rtb.Anchor = $aTLR
  $rtb.Add_ContentsResized({ param($s, $e) $s.Height = $e.NewRectangle.Height + (Px 2) })
  $rtb.Add_Enter({ try { $list.Focus() } catch { } })
  $row.Controls.Add($rtb)

  $state = @{ card = $card; open = $false; built = $false; row = $row; cb = $cb; tick = $tick; title = $title;
    subStatus = $subStatus; subAgent = $subAgent; icon = $icon; pill = $pill; rtb = $rtb; subtop = $rowSubTop; baseh = $baseH }
  $row.Tag = $state
  $icon.Tag = $state
  Set-Check $state

  $toggleCheck = {
    $p = $this.Tag; if ($p -eq $null) { $p = $this.Parent.Tag }
    if ($p -eq $null -or $p.card -eq $null) { return }
    $script:checked[$p.card.id] = -not $script:checked[$p.card.id]
    Set-Check $p; Update-Count
  }
  $row.Add_Click($toggleCheck)
  $cb.Add_Click($toggleCheck); $tick.Add_Click($toggleCheck)
  $pill.Add_Click($toggleCheck); $title.Add_Click($toggleCheck)
  $subStatus.Add_Click($toggleCheck)
  if ($subAgent -ne $null) { $subAgent.Add_Click($toggleCheck) }
  $icon.Add_Click({ Toggle-Detail $this.Parent })

  $row.Add_MouseEnter({ Set-RowBack $this.Tag $cHover })
  $row.Add_MouseLeave({ Set-RowBack $this.Tag $cWhite })

  $list.Controls.Add($row)
  $script:rows += $row
}

function Build-Detail($st) {
  if ($st.built) { return }
  $rtb = $st.rtb
  [void]$rtb.Handle
  $rtb.SelectionFont = $fDetailB; $rtb.SelectionColor = $cInk; $rtb.AppendText($cfg.what_label + ' ')
  $rtb.SelectionFont = $fDetail; $rtb.SelectionColor = $cDetail; $rtb.AppendText($st.card.what + [Environment]::NewLine + [Environment]::NewLine)
  $rtb.SelectionFont = $fDetailB; $rtb.SelectionColor = $cInk; $rtb.AppendText($cfg.when_label + ' ')
  $rtb.SelectionFont = $fDetail; $rtb.SelectionColor = $cDetail; $rtb.AppendText($st.card.when)
  $rtb.SelectionStart = 0; $rtb.SelectionLength = 0
  $st.built = $true
}

function Toggle-Detail($row) {
  $st = $row.Tag
  foreach ($r in $script:rows) {
    $o = $r.Tag
    if ($o -ne $st -and $o.open) { Close-Detail $r }
  }
  if ($st.open) { Close-Detail $row } else { Open-Detail $row }
}

function Open-Detail($row) {
  $st = $row.Tag
  $w = $st.title.Width
  $x = $st.title.Left
  $y = $st.subtop + $subH + (Px 8)
  $st.rtb.Width = $w
  Build-Detail $st
  $st.rtb.Location = New-Object System.Drawing.Point($x, $y)
  $st.rtb.Visible = $true
  $st.open = $true
  $st.icon.Invalidate()
  $row.Height = $y + $st.rtb.Height + (Px 12)
}

function Close-Detail($row) {
  $st = $row.Tag
  $st.rtb.Visible = $false
  $st.open = $false
  $st.icon.Invalidate()
  $row.Height = $st.baseh
}

function Fit-Rows {
  $w = $list.ClientSize.Width
  foreach ($c in $list.Controls) { $c.Width = $w }
}

$lastGroup = [char]0
foreach ($c in $cfg.cards) {
  if ($c.group -ne $lastGroup) {
    $label = ''
    if ($c.group -eq 'new') { $label = $cfg.group_new }
    elseif ($c.group -eq 'rest') { $label = $cfg.group_rest }
    elseif ($c.group -eq 'dismissed') { $label = $cfg.group_dismissed }
    Add-Header $label
    $lastGroup = $c.group
  }
  Add-Row $c
}
Update-Count
$countl.Location = New-Object System.Drawing.Point((Px 28), ($btnY + [int]((((Px 40)) - $countl.Height) / 2)))

if (-not $shot -and -not $selftest) {
  try {
    $wa = [System.Windows.Forms.Screen]::PrimaryScreen.WorkingArea.Height
    if ($form.Height -gt ($wa - 40)) { $form.Height = $wa - 40 }
  } catch { }
}

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

# Capture through WM_PRINT (PRF_CLIENT|PRF_ERASEBKGND|PRF_CHILDREN), which renders
# the RichTextBox and the owner-drawn shapes and does not need the window fully
# on-screen.
function Print-Bitmap($ctrl, $w, $h) {
  $bmp = New-Object System.Drawing.Bitmap($w, $h)
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  $g.Clear($cWhite)
  $hdc = $g.GetHdc()
  try { [void][RO.Win]::SendMessage($ctrl.Handle, 0x0317, $hdc, [System.IntPtr](28)) } catch { }
  $g.ReleaseHdc($hdc)
  $g.Dispose()
  return $bmp
}
function Capture-Form($path) {
  $form.Refresh(); [System.Windows.Forms.Application]::DoEvents()
  $bmp = Print-Bitmap $form $form.ClientSize.Width $form.ClientSize.Height
  $bmp.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
  $bmp.Dispose()
}
function Near($px, $r, $g, $b, $tol) {
  return (([math]::Abs([int]$px.R - $r) -le $tol) -and ([math]::Abs([int]$px.G - $g) -le $tol) -and ([math]::Abs([int]$px.B - $b) -le $tol))
}
function Row-Has($row, $r, $g, $b, $tol, $xmin, $ymin, $ymax) {
  try {
    $w = $row.Width; $h = $row.Height
    if ($ymax -le 0 -or $ymax -gt $h) { $ymax = $h }
    if ($w -le 0 -or $h -le 0) { return $false }
    $bmp = Print-Bitmap $row $w $h
    $found = $false
    for ($y = $ymin; $y -lt $ymax -and -not $found; $y += 3) {
      for ($x = $xmin; $x -lt $w -and -not $found; $x += 3) {
        if ((Near $bmp.GetPixel($x, $y) $r $g $b $tol)) { $found = $true }
      }
    }
    $bmp.Dispose()
    return $found
  } catch {
    return $false
  }
}
function Pixel-Near($row, $x, $y, $r, $g, $b, $tol) {
  try {
    $bmp = Print-Bitmap $row $row.Width $row.Height
    $ok = Near $bmp.GetPixel($x, $y) $r $g $b $tol
    $bmp.Dispose()
    return $ok
  } catch {
    return $false
  }
}

if ($shot) {
  $form.Add_Shown({
    try {
      Fit-Rows
      if ($shotOpenI -and $script:rows.Count -gt 0) { Open-Detail $script:rows[0] }
      Start-Sleep -Milliseconds 300
      Capture-Form $shot
    } catch {
      [Console]::Out.WriteLine('SHOTERR=' + ($_.Exception.Message -replace '\s+', ' ')); [Console]::Out.Flush()
    } finally {
      $form.Close()
    }
  })
}
if ($selftest) {
  $form.Add_Shown({
    try {
      Fit-Rows
      $form.Refresh(); [System.Windows.Forms.Application]::DoEvents()
      $rowsOnly = @($script:rows)
      $maxRight = 0
      foreach ($r in $rowsOnly) { if (($r.Left + $r.Width) -gt $maxRight) { $maxRight = $r.Left + $r.Width } }
      $encExpect = -join @([char]0x00B7, [char]0x00E1, [char]0x00F1)
      $dateDe = $false
      foreach ($c in $cfg.cards) { if ($c.since -and ($c.since -like '* de *')) { $dateDe = $true } }
      [Console]::Out.WriteLine('SHOWN')
      [Console]::Out.WriteLine('CLIENT=' + $form.ClientSize.Width + ',' + $form.ClientSize.Height)
      [Console]::Out.WriteLine('LISTCLIENT=' + $list.ClientSize.Width + ',' + $list.ClientSize.Height)
      [Console]::Out.WriteLine('CARDS=' + @($cfg.cards).Count)
      [Console]::Out.WriteLine('ROWS=' + $rowsOnly.Count)
      [Console]::Out.WriteLine('ROWMAXRIGHT=' + $maxRight)
      [Console]::Out.WriteLine('HSCROLL=' + $list.HorizontalScroll.Visible)
      [Console]::Out.WriteLine('ENCOK=' + ($cfg.enc_probe -eq $encExpect))
      [Console]::Out.WriteLine('DATEDE=' + $dateDe)
      [Console]::Out.WriteLine('BORDER=' + $form.FormBorderStyle + ' MAX=' + $form.MaximizeBox + ' MIN=' + $form.MinimizeBox + ' TOP=' + $form.TopMost)
      [Console]::Out.Flush()
      $pillRed = $false; $pillYel = $false; $checkCobalt = $false; $titleDark = $false; $pillCorner = $false
      foreach ($r in $rowsOnly) {
        if (-not $pillRed) { $pillRed = Row-Has $r 251 233 229 6 0 0 0 }
        if (-not $pillYel) { $pillYel = Row-Has $r 253 245 223 6 0 0 0 }
        $rid = $null
        if ($r.Tag -ne $null -and $r.Tag.card -ne $null) { $rid = $r.Tag.card.id }
        if (-not $checkCobalt -and $rid -and $script:checked[$rid]) { $checkCobalt = Row-Has $r 35 70 184 14 0 0 0 }
      }
      if ($rowsOnly.Count -gt 0) {
        $r0 = $rowsOnly[0]
        $titleDark = Row-Has $r0 27 26 24 24 $textX 0 0
        $pl = $r0.Tag.pill
        $pillCorner = Pixel-Near $r0 ($pl.Left + 1) ($pl.Top + 1) 255 255 255 8
      }
      [Console]::Out.WriteLine('PILLRED=' + $pillRed)
      [Console]::Out.WriteLine('PILLYEL=' + $pillYel)
      [Console]::Out.WriteLine('TITLEDARK=' + $titleDark)
      [Console]::Out.WriteLine('CHECKCOBALT=' + $checkCobalt)
      [Console]::Out.WriteLine('PILLCORNER=' + $pillCorner)
      $hbefore = 0; $hafter = 0; $detailText = $false; $iconFill = $false
      if ($rowsOnly.Count -gt 0) {
        $r0 = $rowsOnly[0]
        $hbefore = $r0.Height
        Open-Detail $r0
        $r0.Refresh(); [System.Windows.Forms.Application]::DoEvents()
        $hafter = $r0.Height
        $detailText = Row-Has $r0 27 26 24 30 $textX ($r0.Tag.subtop + $subH + (Px 4)) 0
        $ic = $r0.Tag.icon
        $iconFill = Row-Has $r0 27 26 24 16 $ic.Left (Px 12) ((Px 12) + (Px 20))
        Close-Detail $r0
      }
      [Console]::Out.WriteLine('IOPEN=' + $hbefore + ',' + $hafter)
      [Console]::Out.WriteLine('DETAILTEXT=' + $detailText)
      [Console]::Out.WriteLine('ICONFILL=' + $iconFill)
      [Console]::Out.Flush()
    } catch {
      [Console]::Out.WriteLine('SELFTESTERR=' + ($_.Exception.Message -replace '\s+', ' ')); [Console]::Out.Flush()
    } finally {
      $form.Close()
    }
  })
}
[void]$form.ShowDialog()
if ($script:result -ne 'save' -and -not $shot -and -not $selftest) { [Console]::Out.WriteLine('cancel'); [Console]::Out.Flush() }`
