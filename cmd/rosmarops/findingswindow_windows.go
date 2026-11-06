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
// System.Windows.Forms. The list rows are built from ordinary WinForms controls
// — a Panel per row with a drawn checkbox (a bordered Panel that turns cobalt
// with a white ✓), a rounded severity-pill Label, the title and subtitle Labels
// and, when open, the two detail Labels — because owner-drawn Panel.Paint did
// not render (the list showed blank). Standard controls paint and capture
// reliably. The list is a FlowLayoutPanel (TopDown, no wrap, vertical
// AutoScroll); the body, list and footer are anchored, and the window clamps its
// height to the screen work area so the footer is never cut off. Screenshots and
// the self-test use DrawToBitmap, so neither depends on the window being fully
// on-screen.
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
$fCheck   = New-Object System.Drawing.Font('Segoe UI', [single](11 * $f), [System.Drawing.FontStyle]::Bold)
$fIcon    = New-Object System.Drawing.Font('Segoe UI', [single](9 * $f), [System.Drawing.FontStyle]::Italic)
$aTL = [System.Windows.Forms.AnchorStyles]::Top -bor [System.Windows.Forms.AnchorStyles]::Left
$aTLR = $aTL -bor [System.Windows.Forms.AnchorStyles]::Right
$aTR = [System.Windows.Forms.AnchorStyles]::Top -bor [System.Windows.Forms.AnchorStyles]::Right
$aBR = [System.Windows.Forms.AnchorStyles]::Bottom -bor [System.Windows.Forms.AnchorStyles]::Right
$aBL = [System.Windows.Forms.AnchorStyles]::Bottom -bor [System.Windows.Forms.AnchorStyles]::Left
$aFill = $aTLR -bor [System.Windows.Forms.AnchorStyles]::Bottom

function Rounded-Region($w, $h, $r) {
  $d = $r * 2
  if ($d -gt $w) { $d = $w }
  if ($d -gt $h) { $d = $h }
  $p = New-Object System.Drawing.Drawing2D.GraphicsPath
  $p.AddArc(0, 0, $d, $d, 180, 90)
  $p.AddArc($w - $d, 0, $d, $d, 270, 90)
  $p.AddArc($w - $d, $h - $d, $d, $d, 0, 90)
  $p.AddArc(0, $h - $d, $d, $d, 90, 90)
  $p.CloseFigure()
  return (New-Object System.Drawing.Region($p))
}

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
$ql.Anchor = $aTL
$form.Controls.Add($ql)
$tl = New-Object System.Windows.Forms.Label
$tl.AutoSize = $true; $tl.MaximumSize = New-Object System.Drawing.Size((Px 584), 0)
$tl.Font = $fBody; $tl.ForeColor = $cMuted; $tl.BackColor = $cWhite; $tl.Text = $cfg.text
$tl.Location = New-Object System.Drawing.Point((Px 28), ($ql.Bottom + (Px 12)))
$tl.Anchor = $aTL
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
$subH = [int][math]::Ceiling($fSub.GetHeight())
$titleTop = Px 12
$titleH = $titleLineH * 2
$subTop = $titleTop + $titleH + (Px 2)
$baseH = [System.Math]::Max((Px 56), $subTop + $subH + (Px 12))
$textX = (Px 16) + (Px 18) + (Px 12) + (Px 76) + (Px 12)
$rightInset = (Px 16) + (Px 20) + (Px 12)

function Update-Count {
  $n = 0
  foreach ($k in $script:checked.Keys) { if ($script:checked[$k]) { $n++ } }
  if ($n -eq 0) { $countl.Text = $cfg.count_zero }
  elseif ($n -eq 1) { $countl.Text = $cfg.count_one }
  else { $countl.Text = ($cfg.count_other -replace '\{n\}', $n) }
}

function Set-Check($state) {
  $on = [bool]$script:checked[$state.card.id]
  if ($on) {
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

function Measure-Wrap($text, $font, $w) {
  return [System.Windows.Forms.TextRenderer]::MeasureText($text, $font, (New-Object System.Drawing.Size($w, 0)), ([System.Windows.Forms.TextFormatFlags]::WordBreak)).Height
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
  $row = New-Object System.Windows.Forms.Panel
  $row.Margin = New-Object System.Windows.Forms.Padding(0)
  $row.Width = $rowW0
  $row.Height = $baseH
  $row.BackColor = $cWhite
  $script:checked[$card.id] = [bool]$card.dismissed

  $bdr = New-Object System.Windows.Forms.Panel
  $bdr.Height = 1; $bdr.Dock = [System.Windows.Forms.DockStyle]::Top; $bdr.BackColor = $cRowTop
  $row.Controls.Add($bdr)

  # Checkbox: a bordered Panel that turns cobalt with a white ✓ when ticked.
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

  # Severity pill (rounded), fixed 76-wide column.
  $pillTxt = $cfg.red; $pillTx = $cRedTx; $pillBg = $cRedBg
  if ($card.sev -eq 'yellow') { $pillTxt = $cfg.yellow; $pillTx = $cYelTx; $pillBg = $cYelBg }
  $pm = [System.Windows.Forms.TextRenderer]::MeasureText($pillTxt, $fPill)
  $ph = $pm.Height + (Px 4)
  $pill = New-Object System.Windows.Forms.Label
  $pill.AutoSize = $false
  $pill.Size = New-Object System.Drawing.Size((Px 76), $ph)
  $pill.Location = New-Object System.Drawing.Point(((Px 16) + (Px 18) + (Px 12)), (Px 12))
  $pill.Anchor = $aTL
  $pill.Font = $fPill; $pill.ForeColor = $pillTx; $pill.BackColor = $pillBg
  $pill.Text = $pillTxt; $pill.TextAlign = [System.Drawing.ContentAlignment]::MiddleCenter
  $pill.Region = Rounded-Region (Px 76) $ph (Px 9)
  $row.Controls.Add($pill)

  # Title, up to two lines with an ellipsis.
  $title = New-Object System.Windows.Forms.Label
  $title.AutoSize = $false
  $title.Location = New-Object System.Drawing.Point($textX, (Px 12))
  $title.Size = New-Object System.Drawing.Size(($rowW0 - $textX - $rightInset), $titleH)
  $title.Anchor = $aTLR
  $title.Font = $fRow; $title.ForeColor = $cInk; $title.BackColor = $cWhite
  $title.AutoEllipsis = $true; $title.Text = $card.title
  $row.Controls.Add($title)

  # Subtitle: New (cobalt, semibold) or «open since …»; then · agent (muted).
  $subStatus = New-Object System.Windows.Forms.Label
  $subStatus.AutoSize = $true; $subStatus.BackColor = $cWhite
  $subStatus.Location = New-Object System.Drawing.Point($textX, $subTop)
  $subStatus.Anchor = $aTL
  if ($card.new) { $subStatus.Font = $fSubB; $subStatus.ForeColor = $cCobalt; $subStatus.Text = $cfg.sub_new }
  else { $subStatus.Font = $fSub; $subStatus.ForeColor = $cSub; $subStatus.Text = $card.since }
  $row.Controls.Add($subStatus)
  $subAgent = $null
  if ($card.agent) {
    $subAgent = New-Object System.Windows.Forms.Label
    $subAgent.AutoSize = $true; $subAgent.BackColor = $cWhite; $subAgent.Font = $fSub; $subAgent.ForeColor = $cSub
    $subAgent.Text = ' · ' + $card.agent
    $subAgent.Anchor = $aTL
    $row.Controls.Add($subAgent)
  }

  # «i» button (circle).
  $ic = New-Object System.Windows.Forms.Label
  $ic.AutoSize = $false; $ic.Size = New-Object System.Drawing.Size((Px 20), (Px 20))
  $ic.Location = New-Object System.Drawing.Point(($rowW0 - (Px 16) - (Px 20)), (Px 12))
  $ic.Anchor = $aTR
  $ic.Font = $fIcon; $ic.Text = 'i'; $ic.TextAlign = [System.Drawing.ContentAlignment]::MiddleCenter
  $ic.ForeColor = $cMuted; $ic.BackColor = $cRowTop
  $ic.Region = Rounded-Region (Px 20) (Px 20) (Px 10)
  $ic.Cursor = [System.Windows.Forms.Cursors]::Hand
  $row.Controls.Add($ic)

  # Detail labels, hidden until the «i» opens.
  $detW = $rowW0 - $textX - $rightInset
  $whatLbl = New-Object System.Windows.Forms.Label
  $whatLbl.AutoSize = $false; $whatLbl.BackColor = $cWhite; $whatLbl.Font = $fDetailB; $whatLbl.ForeColor = $cInk
  $whatLbl.Text = $cfg.what_label; $whatLbl.Anchor = $aTLR; $whatLbl.Visible = $false
  $row.Controls.Add($whatLbl)
  $whatBody = New-Object System.Windows.Forms.Label
  $whatBody.AutoSize = $false; $whatBody.BackColor = $cWhite; $whatBody.Font = $fDetail; $whatBody.ForeColor = $cDetail
  $whatBody.Text = $card.what; $whatBody.Anchor = $aTLR; $whatBody.Visible = $false
  $row.Controls.Add($whatBody)
  $whenLbl = New-Object System.Windows.Forms.Label
  $whenLbl.AutoSize = $false; $whenLbl.BackColor = $cWhite; $whenLbl.Font = $fDetailB; $whenLbl.ForeColor = $cInk
  $whenLbl.Text = $cfg.when_label; $whenLbl.Anchor = $aTLR; $whenLbl.Visible = $false
  $row.Controls.Add($whenLbl)
  $whenBody = New-Object System.Windows.Forms.Label
  $whenBody.AutoSize = $false; $whenBody.BackColor = $cWhite; $whenBody.Font = $fDetail; $whenBody.ForeColor = $cDetail
  $whenBody.Text = $card.when; $whenBody.Anchor = $aTLR; $whenBody.Visible = $false
  $row.Controls.Add($whenBody)

  $state = @{ card = $card; open = $false; row = $row; cb = $cb; tick = $tick; title = $title;
    subStatus = $subStatus; subAgent = $subAgent; icon = $ic;
    whatLbl = $whatLbl; whatBody = $whatBody; whenLbl = $whenLbl; whenBody = $whenBody }
  $row.Tag = $state
  Set-Check $state
  if ($subAgent -ne $null) {
    $subStatus.Add_SizeChanged({ $p = $this.Parent.Tag; if ($p.subAgent -ne $null) { $p.subAgent.Location = New-Object System.Drawing.Point(($this.Left + $this.Width), $this.Top) } })
    $subAgent.Location = New-Object System.Drawing.Point(($subStatus.Left + $subStatus.PreferredWidth), $subTop)
  }

  $toggleCheck = {
    $p = $this.Tag; if ($p -eq $null) { $p = $this.Parent.Tag }
    if ($p -eq $null) { return }
    $script:checked[$p.card.id] = -not $script:checked[$p.card.id]
    Set-Check $p; Update-Count
  }
  $row.Add_Click($toggleCheck)
  $cb.Add_Click($toggleCheck); $tick.Add_Click($toggleCheck)
  $pill.Add_Click($toggleCheck); $title.Add_Click($toggleCheck)
  $subStatus.Add_Click($toggleCheck)
  if ($subAgent -ne $null) { $subAgent.Add_Click($toggleCheck) }
  $ic.Add_Click({ Toggle-Detail $this.Parent })

  $enter = { Set-RowBack $this.Tag $cHover }
  $leave = { Set-RowBack $this.Tag $cWhite }
  $row.Add_MouseEnter($enter); $row.Add_MouseLeave($leave)

  $list.Controls.Add($row)
  $script:rows += $row
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
  $lh = [int][math]::Ceiling($fDetail.GetHeight() * 1.45)
  $y = $subTop + $subH + (Px 8)
  $wh = Measure-Wrap $st.whatBody.Text $fDetail $w
  $nh = Measure-Wrap $st.whenBody.Text $fDetail $w
  $st.whatLbl.SetBounds($x, $y, $w, $lh)
  $st.whatBody.SetBounds($x, ($y + $lh), $w, $wh)
  $y2 = $y + $lh + $wh + $lh
  $st.whenLbl.SetBounds($x, $y2, $w, $lh)
  $st.whenBody.SetBounds($x, ($y2 + $lh), $w, $nh)
  foreach ($c in @($st.whatLbl, $st.whatBody, $st.whenLbl, $st.whenBody)) { $c.Visible = $true }
  $st.icon.BackColor = $cInk; $st.icon.ForeColor = $cWhite
  $row.Height = $y2 + $lh + $nh + (Px 12)
  $st.open = $true
}

function Close-Detail($row) {
  $st = $row.Tag
  foreach ($c in @($st.whatLbl, $st.whatBody, $st.whenLbl, $st.whenBody)) { $c.Visible = $false }
  $st.icon.BackColor = $cRowTop; $st.icon.ForeColor = $cMuted
  $row.Height = $baseH
  $st.open = $false
}

function Fit-Rows {
  $w = $list.ClientSize.Width
  foreach ($c in $list.Controls) { $c.Width = $w }
}

# The cards arrive already ordered (new, then the rest, then dismissed), so we
# walk them once and add a group header whenever the group changes. This avoids
# any Where-Object filtering subtlety and guarantees every card gets a row.
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

# BV-184 point 3: in production clamp the window height to the screen work area
# minus 40 px, so the footer is never cut off; the list (anchored) shrinks and
# scrolls. Screenshots and the self-test keep the natural size (DrawToBitmap
# does not need the window on-screen).
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

function Capture-Form($path) {
  $form.Refresh(); [System.Windows.Forms.Application]::DoEvents()
  $w = $form.ClientSize.Width; $h = $form.ClientSize.Height
  $bmp = New-Object System.Drawing.Bitmap($w, $h)
  $form.DrawToBitmap($bmp, (New-Object System.Drawing.Rectangle(0, 0, $w, $h)))
  $bmp.Save($path, [System.Drawing.Imaging.ImageFormat]::Png)
  $bmp.Dispose()
}

# Pixel self-test: render a row with DrawToBitmap and report whether a target
# colour appears (BV-184 point 2), so a blank list fails the test.
function Near($px, $r, $g, $b, $tol) {
  return (([math]::Abs([int]$px.R - $r) -le $tol) -and ([math]::Abs([int]$px.G - $g) -le $tol) -and ([math]::Abs([int]$px.B - $b) -le $tol))
}
function Row-Has($row, $r, $g, $b, $tol, $xmin) {
  try {
    $w = $row.Width; $h = $row.Height
    if ($w -le 0 -or $h -le 0) { return $false }
    $bmp = New-Object System.Drawing.Bitmap($w, $h)
    $row.DrawToBitmap($bmp, (New-Object System.Drawing.Rectangle(0, 0, $w, $h)))
    $found = $false
    for ($y = 0; $y -lt $h -and -not $found; $y += 3) {
      for ($x = $xmin; $x -lt $w -and -not $found; $x += 3) {
        $p = $bmp.GetPixel($x, $y)
        if ((Near $p $r $g $b $tol)) { $found = $true }
      }
    }
    $bmp.Dispose()
    return $found
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
      # Geometry first, so we always get output even if the pixel scan fails.
      [Console]::Out.WriteLine('SHOWN')
      [Console]::Out.WriteLine('CLIENT=' + $form.ClientSize.Width + ',' + $form.ClientSize.Height)
      [Console]::Out.WriteLine('LISTCLIENT=' + $list.ClientSize.Width + ',' + $list.ClientSize.Height)
      [Console]::Out.WriteLine('CARDS=' + @($cfg.cards).Count)
      [Console]::Out.WriteLine('ROWS=' + $rowsOnly.Count)
      [Console]::Out.WriteLine('ROWMAXRIGHT=' + $maxRight)
      [Console]::Out.WriteLine('HSCROLL=' + $list.HorizontalScroll.Visible)
      [Console]::Out.WriteLine('BORDER=' + $form.FormBorderStyle + ' MAX=' + $form.MaximizeBox + ' MIN=' + $form.MinimizeBox + ' TOP=' + $form.TopMost)
      [Console]::Out.Flush()
      $pillRed = $false; $pillYel = $false; $checkCobalt = $false; $titleDark = $false
      foreach ($r in $rowsOnly) {
        if (-not $pillRed) { $pillRed = Row-Has $r 251 233 229 6 0 }
        if (-not $pillYel) { $pillYel = Row-Has $r 253 245 223 6 0 }
        $rid = $null
        if ($r.Tag -ne $null -and $r.Tag.card -ne $null) { $rid = $r.Tag.card.id }
        if (-not $checkCobalt -and $rid -and $script:checked[$rid]) { $checkCobalt = Row-Has $r 35 70 184 14 0 }
      }
      if ($rowsOnly.Count -gt 0) { $titleDark = Row-Has $rowsOnly[0] 27 26 24 24 $textX }
      [Console]::Out.WriteLine('PILLRED=' + $pillRed)
      [Console]::Out.WriteLine('PILLYEL=' + $pillYel)
      [Console]::Out.WriteLine('TITLEDARK=' + $titleDark)
      [Console]::Out.WriteLine('CHECKCOBALT=' + $checkCobalt)
      $hbefore = 0; $hafter = 0
      if ($rowsOnly.Count -gt 0) { $hbefore = $rowsOnly[0].Height; Open-Detail $rowsOnly[0]; $hafter = $rowsOnly[0].Height; Close-Detail $rowsOnly[0] }
      [Console]::Out.WriteLine('IOPEN=' + $hbefore + ',' + $hafter)
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
