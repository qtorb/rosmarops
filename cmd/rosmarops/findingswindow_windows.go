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
	Since string `json:"since"` // the localized «open since» date; empty when New
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
// the catalog. The per-card «open since» subtitle is already localized in the
// card; the window only joins it with findings.sub.since at draw time, so the
// window gets the resolved sentence per card.
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
		sub := c.Since
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
// A scrollable panel holds the three groups (New, The rest, Dismissed by you);
// each row draws its own checkbox, severity pill and «i», and folds its detail.
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
$cListBd  = [System.Drawing.ColorTranslator]::FromHtml('#E3E0D8')
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

# The list panel, between the body and the buttons, with a 1px border and an
# inner scrollable panel so the window never grows.
$listTop = $tl.Bottom + (Px 20)
$listBottom = $btnY - (Px 20)
$listHost = New-Object System.Windows.Forms.Panel
$listHost.Location = New-Object System.Drawing.Point((Px 28), $listTop)
$listHost.Size = New-Object System.Drawing.Size((Px 584), ($listBottom - $listTop))
$listHost.BackColor = $cWhite
$listHost.BorderStyle = [System.Windows.Forms.BorderStyle]::FixedSingle
$form.Controls.Add($listHost)
$list = New-Object System.Windows.Forms.Panel
$list.Dock = [System.Windows.Forms.DockStyle]::Fill
$list.AutoScroll = $true
$list.BackColor = $cWhite
$listHost.Controls.Add($list)

$script:checked = @{}
$script:openRow = $null
$innerW = (Px 584) - (Px 2)

function Update-Count {
  $n = 0
  foreach ($k in $script:checked.Keys) { if ($script:checked[$k]) { $n++ } }
  if ($n -eq 0) { $countl.Text = $cfg.count_zero }
  elseif ($n -eq 1) { $countl.Text = $cfg.count_one }
  else { $countl.Text = ($cfg.count_other -replace '\{n\}', $n) }
}

# Build the rows, grouped. $y tracks the stacking position inside $list.
$script:y = 0
function Add-Header($text) {
  $h = New-Object System.Windows.Forms.Label
  $h.AutoSize = $false
  $h.Font = $fHdr; $h.ForeColor = $cMuted; $h.BackColor = $cWhite
  $h.Text = $text.ToUpper()
  $h.TextAlign = [System.Drawing.ContentAlignment]::BottomLeft
  $h.Location = New-Object System.Drawing.Point((Px 16), $script:y)
  $h.Size = New-Object System.Drawing.Size(($innerW - (Px 16)), ((Px 12) + (Px 14) + (Px 6)))
  $h.Padding = New-Object System.Windows.Forms.Padding(0, (Px 12), 0, (Px 6))
  $list.Controls.Add($h)
  $script:y += $h.Height
}

function Add-Row($card) {
  $row = New-Object System.Windows.Forms.Panel
  $row.Width = $innerW
  $row.BackColor = $cWhite
  $row.Location = New-Object System.Drawing.Point(0, $script:y)
  $row.Tag = $card
  $script:checked[$card.id] = [bool]$card.dismissed
  $box = New-Object System.Drawing.Rectangle((Px 16), (Px 12), (Px 18), (Px 18))
  $pillX = (Px 16) + (Px 18) + (Px 12)
  $pillW = Px 76
  $textX = $pillX + $pillW + (Px 12)
  $iconD = Px 20
  $iconX = $innerW - (Px 16) - $iconD
  $textW = $iconX - (Px 12) - $textX
  # Title, up to two lines then an ellipsis.
  $titleL = New-Object System.Windows.Forms.Label
  $titleL.AutoSize = $false
  $titleL.Font = $fRow; $titleL.ForeColor = $cInk; $titleL.BackColor = [System.Drawing.Color]::Transparent
  $titleL.Text = $card.title
  $titleL.AutoEllipsis = $true
  $titleL.Location = New-Object System.Drawing.Point($textX, (Px 12))
  $titleL.Size = New-Object System.Drawing.Size($textW, ([int]([System.Math]::Ceiling(2 * $fRow.GetHeight()))))
  $row.Controls.Add($titleL)
  # Subtitle: «New» (cobalt, semibold) or «Open since …», then · agent.
  $subL = New-Object System.Windows.Forms.Label
  $subL.AutoSize = $false
  $subL.Font = $fSub; $subL.BackColor = [System.Drawing.Color]::Transparent
  $subText = $card.since
  if ($card.new) { $subL.Font = $fSubB; $subL.ForeColor = $cCobalt; $subText = $cfg.sub_new } else { $subL.ForeColor = $cSub }
  if ($card.agent) { $subText = $subText + ' · ' + $card.agent }
  $subL.Text = $subText
  $subL.Location = New-Object System.Drawing.Point($textX, ($titleL.Bottom + (Px 2)))
  $subL.Size = New-Object System.Drawing.Size($textW, ([int]([System.Math]::Ceiling($fSub.GetHeight()))))
  $row.Controls.Add($subL)
  # The folded detail, hidden by default.
  $detail = New-Object System.Windows.Forms.Label
  $detail.AutoSize = $false
  $detail.Font = $fDetail; $detail.ForeColor = $cDetail; $detail.BackColor = [System.Drawing.Color]::Transparent
  $detail.Text = ($cfg.what_label + ' ' + $card.what + [Environment]::NewLine + [Environment]::NewLine + $cfg.when_label + ' ' + $card.when)
  $detail.Visible = $false
  $detail.Location = New-Object System.Drawing.Point($textX, ($subL.Bottom + (Px 8)))
  $detail.Size = New-Object System.Drawing.Size($textW, (Px 120))
  $row.Controls.Add($detail)
  # Row height: at least 56, or enough for the (optional) detail.
  $baseH = [System.Math]::Max((Px 56), ($subL.Bottom + (Px 12)))
  $row.Tag = @{ card = $card; box = $box; pillX = $pillX; pillW = $pillW; iconX = $iconX; iconD = $iconD; detail = $detail; baseH = $baseH; open = $false }
  $row.Height = $baseH
  $row.Add_Paint({
    param($s, $e)
    $g = $e.Graphics
    $g.SmoothingMode = [System.Drawing.Drawing2D.SmoothingMode]::AntiAlias
    $t = $s.Tag
    $g.DrawLine((New-Object System.Drawing.Pen($cRowTop, 1)), 0, 0, $s.Width, 0)
    # Checkbox.
    $b = $t.box
    if ($script:checked[$t.card.id]) {
      $g.FillRectangle((New-Object System.Drawing.SolidBrush($cCobalt)), $b)
      $g.DrawRectangle((New-Object System.Drawing.Pen($cCobalt, 1)), $b)
      $pen = New-Object System.Drawing.Pen($cWhite, 2)
      $g.DrawLines($pen, @(
        (New-Object System.Drawing.Point(($b.X + [int]($b.Width*0.22)), ($b.Y + [int]($b.Height*0.52)))),
        (New-Object System.Drawing.Point(($b.X + [int]($b.Width*0.42)), ($b.Y + [int]($b.Height*0.72)))),
        (New-Object System.Drawing.Point(($b.X + [int]($b.Width*0.78)), ($b.Y + [int]($b.Height*0.30))))))
    } else {
      $g.FillRectangle((New-Object System.Drawing.SolidBrush($cWhite)), $b)
      $g.DrawRectangle((New-Object System.Drawing.Pen($cBox, 1)), $b)
    }
    # Severity pill.
    $txt = $cfg.red; $tx = $cRedTx; $bg = $cRedBg
    if ($t.card.sev -eq 'yellow') { $txt = $cfg.yellow; $tx = $cYelTx; $bg = $cYelBg }
    $pm = [System.Windows.Forms.TextRenderer]::MeasureText($txt, $fPill)
    $ph = $pm.Height + (Px 4)
    $pr = New-Object System.Drawing.Rectangle($t.pillX, (Px 12), $t.pillW, $ph)
    $path = New-Object System.Drawing.Drawing2D.GraphicsPath
    $rad = Px 9
    $path.AddArc($pr.X, $pr.Y, $rad*2, $rad*2, 180, 90)
    $path.AddArc($pr.Right - $rad*2, $pr.Y, $rad*2, $rad*2, 270, 90)
    $path.AddArc($pr.Right - $rad*2, $pr.Bottom - $rad*2, $rad*2, $rad*2, 0, 90)
    $path.AddArc($pr.X, $pr.Bottom - $rad*2, $rad*2, $rad*2, 90, 90)
    $path.CloseFigure()
    $g.FillPath((New-Object System.Drawing.SolidBrush($bg)), $path)
    $g.DrawString($txt, $fPill, (New-Object System.Drawing.SolidBrush($tx)), (New-Object System.Drawing.PointF(($t.pillX + (Px 8)), (Px 12 + (($ph - $pm.Height)/2)))))
    # «i» circle.
    $ir = New-Object System.Drawing.Rectangle($t.iconX, (Px 12), $t.iconD, $t.iconD)
    if ($t.open) {
      $g.FillEllipse((New-Object System.Drawing.SolidBrush($cInk)), $ir)
      $g.DrawString('i', $fIcon, (New-Object System.Drawing.SolidBrush($cWhite)), (New-Object System.Drawing.PointF(($t.iconX + (Px 7)), (Px 13))))
    } else {
      $g.DrawEllipse((New-Object System.Drawing.Pen($cBox, 1)), $ir)
      $g.DrawString('i', $fIcon, (New-Object System.Drawing.SolidBrush($cMuted)), (New-Object System.Drawing.PointF(($t.iconX + (Px 7)), (Px 13))))
    }
  })
  $row.Add_MouseEnter({ $this.BackColor = $cHover; foreach ($c in $this.Controls) { $c.BackColor = $cHover }; $this.Invalidate() })
  $row.Add_MouseLeave({ $this.BackColor = $cWhite; foreach ($c in $this.Controls) { $c.BackColor = [System.Drawing.Color]::Transparent }; $this.Invalidate() })
  $toggleDetail = {
    param($r)
    $t = $r.Tag
    if ($script:openRow -ne $null -and $script:openRow -ne $r) {
      $ot = $script:openRow.Tag; $ot.open = $false; $ot.detail.Visible = $false
      $script:openRow.Height = $ot.baseH; $script:openRow.Invalidate()
      Relayout
    }
    $t.open = -not $t.open
    $t.detail.Visible = $t.open
    if ($t.open) { $r.Height = $t.detail.Bottom + (Px 12); $script:openRow = $r } else { $r.Height = $t.baseH; $script:openRow = $null }
    $r.Invalidate(); Relayout
  }
  $click = {
    param($s, $e)
    $r = $s; if ($r -isnot [System.Windows.Forms.Panel]) { $r = $s.Parent }
    $t = $r.Tag
    # Inside the «i» circle: toggle the detail, never the checkbox.
    if ($e.X -ge $t.iconX -and $e.X -le ($t.iconX + $t.iconD) -and $e.Y -ge (Px 12) -and $e.Y -le ((Px 12) + $t.iconD)) {
      & $toggleDetail $r; return
    }
    $script:checked[$t.card.id] = -not $script:checked[$t.card.id]
    Update-Count; $r.Invalidate()
  }
  $row.Add_MouseUp($click)
  $titleL.Add_MouseUp($click); $subL.Add_MouseUp($click)
  $list.Controls.Add($row)
  $script:y += $row.Height
}

function Relayout {
  $yy = 0
  foreach ($ctl in $list.Controls) {
    $ctl.Location = New-Object System.Drawing.Point($ctl.Left, $yy)
    $yy += $ctl.Height
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
# Centre the count label vertically with the buttons.
$countl.Location = New-Object System.Drawing.Point((Px 28), ($btnY + [int]((((Px 40)) - $countl.Height)/2)))

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
    if ($shotOpenI -and $list.Controls.Count -gt 0) {
      foreach ($ctl in $list.Controls) { if ($ctl -is [System.Windows.Forms.Panel]) { & $toggleDetail $ctl; break } }
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
    [Console]::Out.WriteLine('SHOWN')
    [Console]::Out.WriteLine('CLIENT=' + $form.ClientSize.Width + ',' + $form.ClientSize.Height)
    [Console]::Out.WriteLine('TITLE=' + $ql.Left + ',' + $ql.Top)
    [Console]::Out.WriteLine('LIST=' + $listHost.Left + ',' + $listHost.Top + ',' + $listHost.Width + ',' + $listHost.Height)
    [Console]::Out.WriteLine('SAVE=' + $save.Left + ',' + $save.Top + ',' + $save.Width + ',' + $save.Height)
    [Console]::Out.WriteLine('NO=' + $no.Left + ',' + $no.Top + ',' + $no.Width + ',' + $no.Height)
    [Console]::Out.WriteLine('ROWS=' + @($list.Controls | Where-Object { $_ -is [System.Windows.Forms.Panel] }).Count)
    [Console]::Out.WriteLine('FORMBACK=' + $form.BackColor.ToArgb())
    [Console]::Out.WriteLine('SAVEBACK=' + $save.BackColor.ToArgb())
    [Console]::Out.WriteLine('BORDER=' + $form.FormBorderStyle + ' MAX=' + $form.MaximizeBox + ' MIN=' + $form.MinimizeBox + ' TOP=' + $form.TopMost)
    [Console]::Out.WriteLine('PT=' + $ql.Font.SizeInPoints + ',' + $tl.Font.SizeInPoints)
    [Console]::Out.Flush()
    $form.Close()
  })
}
[void]$form.ShowDialog()
if ($script:result -ne 'save' -and -not $shot -and -not $selftest) { [Console]::Out.WriteLine('cancel'); [Console]::Out.Flush() }`
