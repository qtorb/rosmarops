package main

// BV-211b (CTO-34): the four PowerShell window scripts live here, not in the
// _windows.go files, so the launcher tests can check them on every system. They
// run through windowBootstrap over standard input (consolewin.go); only the
// exec calls that use them are Windows-only.

// dismissWindowScript shows the "No me afecta" window (BV-184 S4) with
// System.Windows.Forms. Rows use ordinary controls: a bordered-Panel checkbox,
// an owner-drawn pill and "i" (rounded/elliptical fills drawn in Paint), the
// title and subtitle Labels, and a read-only borderless RichTextBox for the
// open detail (left-aligned text with the two Semibold labels inline). Capture
// (screenshots and the pixel self-test) is a real screen grab (CopyFromScreen),
// the same method the connect window uses, so it renders the RichTextBox and the
// owner-drawn shapes exactly as painted; the window is TopMost and clamped to the
// working area so it is never occluded or off-screen. Non-ASCII text arrives as
// base64 UTF-8 JSON; the only separator literal is built from a char code.
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
$shot = $env:ROSMAROPS_FINDINGS_WINDOW_SHOT
$shotScale = $env:ROSMAROPS_FINDINGS_WINDOW_SHOT_SCALE
$shotOpenI = $env:ROSMAROPS_FINDINGS_WINDOW_SHOT_OPEN_I
$shotScroll = $env:ROSMAROPS_FINDINGS_WINDOW_SHOT_SCROLL     # "bottom" shows "the rest" and "dismissed"
$shotTickNew = $env:ROSMAROPS_FINDINGS_WINDOW_SHOT_TICK_NEW  # tick the first "new" row so "1 ticked" shows on screen
$shotHover = $env:ROSMAROPS_FINDINGS_WINDOW_SHOT_HOVER       # paint one row in the hover colour
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
# Calibrate the "new" subtitle's left inset once (BV-184 point 3): the pixel gap
# between where a default-rendered "New" (as a Label draws it, lined up with the
# title) and a NoPadding "New" put their first ink. The subtitle draws its two
# pieces NoPadding (so the "." gaps stay equal) shifted by this offset, so "New"
# still lines up with the title at any scale.
function Sub-FirstInk($bmp, $h) {
  for ($x = 0; $x -lt $bmp.Width; $x++) {
    for ($y = 0; $y -lt $h; $y++) {
      $p = $bmp.GetPixel($x, $y)
      if (($p.R + $p.G + $p.B) -lt 720) { return $x }
    }
  }
  return 0
}
$subOff = 0
try {
  $cw = (Px 120); $ch = $subH + (Px 6)
  $cb = New-Object System.Drawing.Bitmap($cw, $ch)
  $cg = [System.Drawing.Graphics]::FromImage($cb)
  $cg.Clear([System.Drawing.Color]::White)
  [System.Windows.Forms.TextRenderer]::DrawText($cg, $cfg.sub_new, $fSubB, (New-Object System.Drawing.Point(0, 0)), [System.Drawing.Color]::Black)
  $dL = Sub-FirstInk $cb $ch
  $cg.Clear([System.Drawing.Color]::White)
  [System.Windows.Forms.TextRenderer]::DrawText($cg, $cfg.sub_new, $fSubB, (New-Object System.Drawing.Point(0, 0)), [System.Drawing.Color]::Black, $noPad)
  $nL = Sub-FirstInk $cb $ch
  $cg.Dispose(); $cb.Dispose()
  $subOff = $dL - $nL
  if ($subOff -lt 0) { $subOff = 0 }
} catch { $subOff = 0 }

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
  # The row and everything that must share its background change together: the
  # title and subtitle labels, the pill (its corners outside the rounded fill
  # show this colour) and the open detail box (BV-184 polish, points 1-2).
  $state.row.BackColor = $col
  foreach ($c in @($state.title, $state.subStatus, $state.subAgent, $state.pill, $state.rtb)) { if ($c -ne $null) { $c.BackColor = $col } }
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
  # " . ". For a new finding "New" is cobalt-bold and the agent muted - drawn
  # owner-draw in one Paint, "New" then " . <agent>", each measured NoPadding so
  # the "." sits between two equal spaces and the gaps match (BV-184 point 3).
  # A Label's trailing padding made the left gap larger, so no Label is used.
  $subAgent = $null
  if ($card.new) {
    $subStatus = New-Object System.Windows.Forms.Panel
    $subStatus.Width = $colW; $subStatus.Height = $subH
    $subStatus.Location = New-Object System.Drawing.Point($textX, $rowSubTop); $subStatus.Anchor = $aTL
    $subStatus.BackColor = $cWhite
    $subStatus.Tag = @{ lead = $cfg.sub_new; agent = $card.agent }
    $subStatus.Add_Paint({
      param($s, $e)
      $t = $s.Tag; $g = $e.Graphics
      # Both pieces are drawn NoPadding and flush: "New" at the left inset $off,
      # then " . <agent>" at "New"'s ink-right ($off + its NoPadding width). The
      # "." thus sits between two equal spaces (gap before = gap after). $off is
      # the GlyphOverhangPadding a Label would add, so "New"'s first ink lines up
      # with the title's (BV-184 point 3).
      [System.Windows.Forms.TextRenderer]::DrawText($g, $t.lead, $fSubB, (New-Object System.Drawing.Point($subOff, 0)), $cCobalt, $noPad)
      if ($t.agent) {
        $ink = [System.Windows.Forms.TextRenderer]::MeasureText($g, $t.lead, $fSubB, [System.Drawing.Size]::Empty, $noPad).Width
        [System.Windows.Forms.TextRenderer]::DrawText($g, ($mid + $t.agent), $fSub, (New-Object System.Drawing.Point(($subOff + $ink), 0)), $cSub, $noPad)
      }
    })
  } else {
    $subStatus = New-Object System.Windows.Forms.Label
    $subStatus.AutoSize = $true; $subStatus.BackColor = $cWhite
    $subStatus.Location = New-Object System.Drawing.Point($textX, $rowSubTop); $subStatus.Anchor = $aTL
    $parts = @()
    if ($card.since) { $parts += $card.since }
    if ($card.agent) { $parts += $card.agent }
    $subStatus.Font = $fSub; $subStatus.ForeColor = $cSub; $subStatus.Text = [string]::Join($mid, $parts)
  }
  $row.Controls.Add($subStatus)

  # "i": owner-drawn circle. Closed: white fill, 1 px #8A867E border, muted "i".
  # Open: #1B1A18 fill, white "i". An elliptical Region clips it to a circle.
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
  $st.rtb.BackColor = $row.BackColor # match the row, hovered or not (point 1)
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

# Keep the window within the working area. For a real run this stops a tall
# window overflowing the screen; for a screenshot it keeps the whole window
# on-screen so CopyFromScreen captures all of it.
try {
  $wa = [System.Windows.Forms.Screen]::PrimaryScreen.WorkingArea.Height
  if ($form.Height -gt ($wa - 40)) { $form.Height = $wa - 40 }
} catch { }

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

# Capture with CopyFromScreen (a real screen grab), the same method the connect
# window uses: it is true WYSIWYG, so it renders the RichTextBox, the owner-drawn
# shapes and everything else exactly as painted. The window is TopMost and shown
# modally, and for shots it is clamped to fit the working area, so it is never
# occluded or off-screen.
function Grab-Rect($x, $y, $w, $h) {
  $bmp = New-Object System.Drawing.Bitmap($w, $h)
  $g = [System.Drawing.Graphics]::FromImage($bmp)
  $g.CopyFromScreen((New-Object System.Drawing.Point($x, $y)), [System.Drawing.Point]::Empty, (New-Object System.Drawing.Size($w, $h)))
  $g.Dispose()
  return $bmp
}
function Grab-Control($ctrl) {
  # Bring the control into the list viewport first: a screen grab can only read
  # pixels that are actually on-screen, so a row scrolled out of view must be
  # scrolled in before it is captured.
  try { $list.ScrollControlIntoView($ctrl); $form.Refresh(); [System.Windows.Forms.Application]::DoEvents(); Start-Sleep -Milliseconds 100 } catch { }
  $pt = $ctrl.PointToScreen([System.Drawing.Point]::Empty)
  return Grab-Rect $pt.X $pt.Y $ctrl.Width $ctrl.Height
}
function Capture-Form($path) {
  $form.Refresh(); [System.Windows.Forms.Application]::DoEvents()
  Start-Sleep -Milliseconds 400
  $pt = $form.PointToScreen([System.Drawing.Point]::Empty)
  $bmp = Grab-Rect $pt.X $pt.Y $form.ClientSize.Width $form.ClientSize.Height
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
    $bmp = Grab-Control $row
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
    $bmp = Grab-Control $row
    $ok = Near $bmp.GetPixel($x, $y) $r $g $b $tol
    $bmp.Dispose()
    return $ok
  } catch {
    return $false
  }
}
# Measure-SubGaps returns the two empty-column runs around the "." in a "new"
# row's subtitle, read from the ink on screen (BV-184 point 3): the gap between
# the last pixel of "New" and the first of ".", and between "." and the agent.
# They must be equal. It measures tint, not control boxes.
function Measure-SubGaps($row, $bg) {
  $st = $row.Tag
  $y0 = $st.subtop; $y1 = $y0 + $subH
  $xEnd = [math]::Min($row.Width, $textX + (Px 260))
  $bmp = Grab-Control $row
  $ink = New-Object 'bool[]' ($xEnd - $textX)
  for ($x = $textX; $x -lt $xEnd; $x++) {
    $f = $false
    for ($y = $y0; $y -lt $y1 -and -not $f; $y++) {
      $p = $bmp.GetPixel($x, $y)
      if ((([math]::Abs([int]$p.R - [int]$bg.R)) + ([math]::Abs([int]$p.G - [int]$bg.G)) + ([math]::Abs([int]$p.B - [int]$bg.B))) -gt 60) { $f = $true }
    }
    $ink[$x - $textX] = $f
  }
  $bmp.Dispose()
  $n = $ink.Length
  $s1 = -1; for ($k = 0; $k -lt $n; $k++) { if ($ink[$k]) { $s1 = $k; break } }
  $e1 = -1; if ($s1 -ge 0) { for ($k = $s1; $k -lt $n; $k++) { if (-not $ink[$k]) { $e1 = $k; break } } }
  $s2 = -1; if ($e1 -ge 0) { for ($k = $e1; $k -lt $n; $k++) { if ($ink[$k]) { $s2 = $k; break } } }
  $e2 = -1; if ($s2 -ge 0) { for ($k = $s2; $k -lt $n; $k++) { if (-not $ink[$k]) { $e2 = $k; break } } }
  $s3 = -1; if ($e2 -ge 0) { for ($k = $e2; $k -lt $n; $k++) { if ($ink[$k]) { $s3 = $k; break } } }
  $gap1 = -1; $gap2 = -1
  if ($s2 -ge 0 -and $e1 -ge 0) { $gap1 = $s2 - $e1 }
  if ($s3 -ge 0 -and $e2 -ge 0) { $gap2 = $s3 - $e2 }
  return @($gap1, $gap2)
}
# First-InkCol is the first column (row-relative x) that has tint in the band
# y0..y1, scanning from just left of the text column (BV-184 point 3 follow-up:
# the subtitle must start in the same ink column as the title).
function First-InkCol($row, $bg, $y0, $y1) {
  $bmp = Grab-Control $row
  $xStart = [math]::Max(0, $textX - (Px 8))
  $xEnd = [math]::Min($row.Width, $textX + (Px 220))
  $col = -1
  for ($x = $xStart; $x -lt $xEnd -and $col -lt 0; $x++) {
    for ($y = $y0; $y -lt $y1; $y++) {
      $p = $bmp.GetPixel($x, $y)
      if ((([math]::Abs([int]$p.R - [int]$bg.R)) + ([math]::Abs([int]$p.G - [int]$bg.G)) + ([math]::Abs([int]$p.B - [int]$bg.B))) -gt 60) { $col = $x; break }
    }
  }
  $bmp.Dispose()
  return $col
}

if ($shot) {
  $form.Add_Shown({
    try {
      Fit-Rows
      $target = $null
      if ($script:rows.Count -gt 0) { $target = $script:rows[0] } # first row = first "new"
      # Tick exactly the first "new" row so a ticked box and "1 ticked" show
      # on screen (BV-184 point 4b): clear the rest first.
      if ($shotTickNew -and $target -ne $null) {
        foreach ($k in @($script:checked.Keys)) { $script:checked[$k] = $false }
        $script:checked[$target.Tag.card.id] = $true
        foreach ($r in $script:rows) { Set-Check $r.Tag }
        Update-Count
      }
      # Scroll to the bottom so "the rest" (long date) and "dismissed" show
      # (point 4a).
      if ($shotScroll -eq 'bottom' -and $script:rows.Count -gt 0) {
        $list.ScrollControlIntoView($script:rows[$script:rows.Count - 1])
      }
      # Hover: paint the target row in the hover colour before opening its
      # detail, so the detail and pill take the hover background (points 1-2, 4c).
      if ($shotHover -and $target -ne $null) { Set-RowBack $target.Tag $cHover }
      if ($shotOpenI -and $target -ne $null) { Open-Detail $target }
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
      # Let the window settle on screen before grabbing pixels from it.
      Start-Sleep -Milliseconds 400
      $form.Refresh(); [System.Windows.Forms.Application]::DoEvents()
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
      $gapFlush = $false; $gap1 = -1; $gap2 = -1; $detailBgRest = $false; $detailBgHover = $false; $pillCornerHover = $false
      $alignSub = $false; $alignNewT = -1; $alignNewS = -1; $alignRestT = -1; $alignRestS = -1
      if ($rowsOnly.Count -gt 0) {
        $r0 = $rowsOnly[0]
        # Point 3 (pixel): the empty-column run before the "." equals the one
        # after it (1 px margin), read from the ink on the row at rest.
        $gaps = Measure-SubGaps $r0 $r0.BackColor
        $gap1 = $gaps[0]; $gap2 = $gaps[1]
        $gapFlush = ($gap1 -gt 0 -and $gap2 -gt 0 -and ([math]::Abs($gap1 - $gap2) -le 1))
        # ALIGNSUB: the subtitle's first ink column equals the title's, for a
        # "new" row (r0) and a dated "rest" row (rowsOnly[2] = f_3).
        $tY0 = (Px 12); $tY1 = (Px 12) + $titleLineH
        $alignNewT = First-InkCol $r0 $r0.BackColor $tY0 $tY1
        $alignNewS = First-InkCol $r0 $r0.BackColor $r0.Tag.subtop ($r0.Tag.subtop + $subH)
        $alignNew = ($alignNewT -ge 0 -and $alignNewS -ge 0 -and ([math]::Abs($alignNewT - $alignNewS) -le 1))
        $alignRest = $true
        if ($rowsOnly.Count -ge 3) {
          $rD = $rowsOnly[2]
          $alignRestT = First-InkCol $rD $rD.BackColor $tY0 $tY1
          $alignRestS = First-InkCol $rD $rD.BackColor $rD.Tag.subtop ($rD.Tag.subtop + $subH)
          $alignRest = ($alignRestT -ge 0 -and $alignRestS -ge 0 -and ([math]::Abs($alignRestT - $alignRestS) -le 1))
        }
        $alignSub = ($alignNew -and $alignRest)
        $hbefore = $r0.Height
        Open-Detail $r0
        $form.Refresh(); [System.Windows.Forms.Application]::DoEvents()
        Start-Sleep -Milliseconds 300
        $hafter = $r0.Height
        $detailText = Row-Has $r0 27 26 24 30 $textX ($r0.Tag.subtop + $subH + (Px 4)) 0
        $ic = $r0.Tag.icon
        $iconFill = Row-Has $r0 27 26 24 16 $ic.Left (Px 12) ((Px 12) + (Px 20))
        # Point 1 at rest: the detail box background equals its row's.
        $detailBgRest = ($r0.Tag.rtb.BackColor.ToArgb() -eq $r0.BackColor.ToArgb())
        # Points 1 & 2 under hover: paint the row in the hover colour; the detail
        # box follows it and the pill corner shows the hover colour, not white.
        Set-RowBack $r0.Tag $cHover
        $form.Refresh(); [System.Windows.Forms.Application]::DoEvents(); Start-Sleep -Milliseconds 200
        $detailBgHover = ($r0.Tag.rtb.BackColor.ToArgb() -eq $r0.BackColor.ToArgb())
        $pl = $r0.Tag.pill
        $pillCornerHover = Pixel-Near $r0 ($pl.Left + 1) ($pl.Top + 1) 246 245 242 8
        Set-RowBack $r0.Tag $cWhite
        Close-Detail $r0
      }
      [Console]::Out.WriteLine('IOPEN=' + $hbefore + ',' + $hafter)
      [Console]::Out.WriteLine('DETAILTEXT=' + $detailText)
      [Console]::Out.WriteLine('ICONFILL=' + $iconFill)
      [Console]::Out.WriteLine('GAPFLUSH=' + $gapFlush + ' GAP1=' + $gap1 + ' GAP2=' + $gap2)
      [Console]::Out.WriteLine('ALIGNSUB=' + $alignSub + ' ANEW=' + $alignNewT + ',' + $alignNewS + ' AREST=' + $alignRestT + ',' + $alignRestS)
      [Console]::Out.WriteLine('DETAILBGREST=' + $detailBgRest)
      [Console]::Out.WriteLine('DETAILBGHOVER=' + $detailBgHover)
      [Console]::Out.WriteLine('PILLCORNERHOVER=' + $pillCornerHover)
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

// connectWindowScript shows the window with System.Windows.Forms and talks to
// the program over the standard streams. The first stdin line is the base64 JSON
// config, read with [Console]::In.ReadLine() before the window shows (as v0.7.0
// did). The window shows the question; on "Connect" it writes "connect", hides
// the buttons and starts a dedicated runspace that reads the program's state
// lines ("number <N>", then "done", "expired" or "failed") from [Console]::In on
// its own thread into a concurrent queue, which a UI Timer drains - so the
// interface thread is never blocked on a read (BV-145, BV-146, BV-147). The final
// state shows an OK button that, clicked, writes "ok" and closes; closing stops
// the reader runspace. "Not now" or closing before connecting writes "no" and
// ends. Under Constrained Language Mode Add-Type fails and PowerShell exits, which
// the caller treats as "Not now" (fail closed). With
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
# BV-178: the RosmarOps look - a white dialog, dark ink, a cobalt primary button
# and a flat outlined secondary, no grey chrome.
#
# One scale mechanism. In production $f is 1 and AutoScaleMode Dpi lets Windows
# scale the 96-DPI design to the real monitor (fonts, in points, scale with it):
# this is the single production mechanism. For the CI screenshots a 96-DPI runner
# cannot drive a real 150 % monitor, so shot mode scales everything by $f
# manually (fonts AND geometry together) with AutoScaleMode None - a true
# 100 %/150 % window, not a stretched bitmap.
$shot = $env:ROSMAROPS_CONNECT_WINDOW_SHOT
$shotScale = $env:ROSMAROPS_CONNECT_WINDOW_SHOT_SCALE
$f = 1.0
if ($shot -and $shotScale) { $f = [double]$shotScale }
function Px($n) { return [int][math]::Round([double]$n * $f) }
$cWhite  = [System.Drawing.ColorTranslator]::FromHtml('#FFFFFF')
$cInk    = [System.Drawing.ColorTranslator]::FromHtml('#1B1A18')
$cMuted  = [System.Drawing.ColorTranslator]::FromHtml('#4F4C46')
$cBorder = [System.Drawing.ColorTranslator]::FromHtml('#C9C5BC')
$cCobalt = [System.Drawing.ColorTranslator]::FromHtml('#2346B8')
$fTitle  = New-Object System.Drawing.Font('Segoe UI Semibold', [single](15 * $f))
$fBody   = New-Object System.Drawing.Font('Segoe UI', [single](10.5 * $f))
$fBtnP   = New-Object System.Drawing.Font('Segoe UI Semibold', [single](10.5 * $f))
$fBtnS   = New-Object System.Drawing.Font('Segoe UI', [single](10.5 * $f))
$form = New-Object System.Windows.Forms.Form
$form.Text = $cfg.title
if ($shot) { $form.AutoScaleMode = [System.Windows.Forms.AutoScaleMode]::None }
else { $form.AutoScaleMode = [System.Windows.Forms.AutoScaleMode]::Dpi; $form.AutoScaleDimensions = New-Object System.Drawing.SizeF(96, 96) }
$form.TopMost = $true
$form.FormBorderStyle = 'FixedSingle'
$form.StartPosition = 'CenterScreen'
$form.BackColor = $cWhite
$form.ClientSize = New-Object System.Drawing.Size((Px 480), (Px 248))
$form.MaximizeBox = $false
$form.MinimizeBox = $false
# Title-bar icon: the rosemary of rosmarops.exe (os.Executable, BV-149's embedded
# icon), never PowerShell's own process icon. If that path carries no icon (a
# test binary), fall back to assets/windows/rosmarops.ico.
$iconSet = $false
if ($cfg.icon -and (Test-Path -LiteralPath $cfg.icon)) {
  try { $ic = [System.Drawing.Icon]::ExtractAssociatedIcon($cfg.icon); if ($ic -ne $null) { $form.Icon = $ic; $iconSet = $true } } catch { }
}
if (-not $iconSet -and $cfg.iconfallback -and (Test-Path -LiteralPath $cfg.iconfallback)) {
  try { $form.Icon = New-Object System.Drawing.Icon($cfg.iconfallback); $iconSet = $true } catch { }
}
$script:answered = $false
# Title (connect.q): Segoe UI Semibold 15, ink, wraps within 424 px.
$ql = New-Object System.Windows.Forms.Label
$ql.AutoSize = $true
$ql.MaximumSize = New-Object System.Drawing.Size((Px 424), 0)
$ql.Font = $fTitle; $ql.ForeColor = $cInk; $ql.BackColor = $cWhite
$ql.Text = $cfg.q
$ql.Location = New-Object System.Drawing.Point((Px 28), (Px 28))
$form.Controls.Add($ql)
# Body (connect.text): Segoe UI 10.5, muted, 12 px under the title, 424 wide.
$tl = New-Object System.Windows.Forms.Label
$tl.AutoSize = $true
$tl.MaximumSize = New-Object System.Drawing.Size((Px 424), 0)
$tl.Font = $fBody; $tl.ForeColor = $cMuted; $tl.BackColor = $cWhite
$tl.Text = $cfg.text
$tl.Location = New-Object System.Drawing.Point((Px 28), ($ql.Bottom + (Px 12)))
$form.Controls.Add($tl)
# The result text replaces the title/body once connecting starts; same muted
# body style, at the top-left content origin.
$resultl = New-Object System.Windows.Forms.Label
$resultl.AutoSize = $true
$resultl.MaximumSize = New-Object System.Drawing.Size((Px 424), 0)
$resultl.Font = $fBody; $resultl.ForeColor = $cMuted; $resultl.BackColor = $cWhite
$resultl.Location = New-Object System.Drawing.Point((Px 28), (Px 28))
$resultl.Visible = $false
$form.Controls.Add($resultl)
# BV-164: the connect offer no longer shows a number. $numValue stays an
# invisible label, set only so the Windows self-test can prove the reader thread
# is alive (it echoes NUMLABEL); the person never sees it.
$numValue = New-Object System.Windows.Forms.Label
$numValue.AutoSize = $true; $numValue.Visible = $false
$form.Controls.Add($numValue)
# The window grows in height if the text needs it; the buttons never wrap.
$clientH = Px 248
$minH = $tl.Bottom + (Px 16) + (Px 40) + (Px 28)
if ($minH -gt $clientH) { $clientH = $minH }
$form.ClientSize = New-Object System.Drawing.Size((Px 480), $clientH)
$btnY = $clientH - (Px 28) - (Px 40)
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
  $b.Height = Px 40
  $b.Width = $m.Width + (Px 36)
  if ($primary) { $b.Cursor = [System.Windows.Forms.Cursors]::Hand }
  $form.Controls.Add($b)
  return $b
}
# Primary "Save to my space" (connect.yes), the web's cobalt (--brand #2346B8,
# ink on hover/press), on the right; secondary "Not now" (connect.no) 12 px to
# its left.
$yes = New-FlatButton $cfg.yes $fBtnP $cCobalt $cWhite 0 $true
$yes.FlatAppearance.MouseOverBackColor = $cInk
$yes.FlatAppearance.MouseDownBackColor = $cInk
$no  = New-FlatButton $cfg.no  $fBtnS $cWhite $cInk 1 $false
$ok  = New-FlatButton $cfg.ok  $fBtnP $cCobalt $cWhite 0 $true
$ok.FlatAppearance.MouseOverBackColor = $cInk
$ok.FlatAppearance.MouseDownBackColor = $cInk
$yes.Location = New-Object System.Drawing.Point(((Px 480) - (Px 28) - $yes.Width), $btnY)
$no.Location  = New-Object System.Drawing.Point(($yes.Left - (Px 12) - $no.Width), $btnY)
$ok.Location  = New-Object System.Drawing.Point(((Px 480) - (Px 28) - $ok.Width), $btnY)
$ok.Visible = $false
$form.AcceptButton = $yes
$form.CancelButton = $no
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
    [Console]::Out.WriteLine('CLIENTRECT=' + $form.ClientRectangle.Width + ',' + $form.ClientRectangle.Height)
    [Console]::Out.WriteLine('VIS=' + $yes.Visible + ',' + $no.Visible)
    [Console]::Out.WriteLine('PT=' + $ql.Font.SizeInPoints + ',' + $tl.Font.SizeInPoints + ',' + $yes.Font.SizeInPoints + ',' + $no.Font.SizeInPoints)
    [Console]::Out.WriteLine('TEXTW=' + ([System.Windows.Forms.TextRenderer]::MeasureText($cfg.yes, $fBtnP).Width) + ',' + ([System.Windows.Forms.TextRenderer]::MeasureText($cfg.no, $fBtnS).Width) + ',' + (Px 36))
    [Console]::Out.WriteLine('BORDER=' + $form.FormBorderStyle + ' MAX=' + $form.MaximizeBox + ' MIN=' + $form.MinimizeBox + ' TOP=' + $form.TopMost)
    [Console]::Out.WriteLine('FORMBACK=' + $form.BackColor.ToArgb())
    [Console]::Out.WriteLine('YESBACK=' + $yes.BackColor.ToArgb() + ' YESFORE=' + $yes.ForeColor.ToArgb() + ' YESBORDER=' + $yes.FlatAppearance.BorderSize + ' YESFLAT=' + $yes.FlatStyle + ' YESMOVER=' + $yes.FlatAppearance.MouseOverBackColor.ToArgb() + ' YESMDOWN=' + $yes.FlatAppearance.MouseDownBackColor.ToArgb())
    [Console]::Out.WriteLine('NOBACK=' + $no.BackColor.ToArgb() + ' NOFORE=' + $no.ForeColor.ToArgb() + ' NOBORDER=' + $no.FlatAppearance.BorderSize + ' NOBORDERCOLOR=' + $no.FlatAppearance.BorderColor.ToArgb() + ' NOFLAT=' + $no.FlatStyle)
    # Icon: scan the title-bar icon for a rosemary-green (#6F8F78 +/-10) pixel, as
    # BV-149 does for the exe icon, so the self-test can prove it is not the
    # generic WinForms/PowerShell icon.
    $iconRosemary = $false
    try {
      $ib = $form.Icon.ToBitmap()
      for ($yy = 0; $yy -lt $ib.Height -and -not $iconRosemary; $yy++) {
        for ($xx = 0; $xx -lt $ib.Width -and -not $iconRosemary; $xx++) {
          $p = $ib.GetPixel($xx, $yy)
          if (([math]::Abs($p.R - 111) -le 10) -and ([math]::Abs($p.G - 143) -le 10) -and ([math]::Abs($p.B - 120) -le 10)) { $iconRosemary = $true }
        }
      }
      $ib.Dispose()
    } catch { }
    [Console]::Out.WriteLine('ICONROSEMARY=' + $iconRosemary)
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

// problemWindowScript is the WinForms window. It reads the base64 JSON config
// on the first stdin line, draws the question with a primary "Send" button, a
// "Not now" button, a "See what is sent" toggle that reveals the exact JSON, an
// "Add a comment" toggle that reveals a comment box and an email box, and the
// "send the next ones without asking" checkbox. On Send/Not now/close it prints
// four lines (decision, remember, base64 comment, base64 email) and exits.
const problemWindowScript = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[System.Windows.Forms.Application]::EnableVisualStyles()

$b64 = [Console]::In.ReadLine()
$json = [System.Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($b64))
$cfg = $json | ConvertFrom-Json

$cInk    = [System.Drawing.Color]::FromArgb(17,24,39)
$cCobalt = [System.Drawing.Color]::FromArgb(37,99,235)
$cWhite  = [System.Drawing.Color]::White
$cPaper  = [System.Drawing.Color]::FromArgb(249,250,251)
$cBorder = [System.Drawing.Color]::FromArgb(209,213,219)

$fTitle = New-Object System.Drawing.Font('Segoe UI',13,[System.Drawing.FontStyle]::Bold)
$fBody  = New-Object System.Drawing.Font('Segoe UI',10)
$fMono  = New-Object System.Drawing.Font('Consolas',9)

$form = New-Object System.Windows.Forms.Form
$form.Text = 'RosmarOps'
$form.StartPosition = 'CenterScreen'
$form.FormBorderStyle = 'FixedDialog'
$form.MaximizeBox = $false
$form.MinimizeBox = $false
$form.BackColor = $cWhite
$form.Font = $fBody
try { if ($cfg.icon -and (Test-Path $cfg.icon)) { $form.Icon = [System.Drawing.Icon]::ExtractAssociatedIcon($cfg.icon) } } catch {}

$title = New-Object System.Windows.Forms.Label
$title.Text = $cfg.title
$title.Font = $fTitle
$title.ForeColor = $cInk
$title.AutoSize = $false
$title.SetBounds(24,0,412,28)
$form.Controls.Add($title)

$text = New-Object System.Windows.Forms.Label
$text.Text = $cfg.text
$text.Font = $fBody
$text.ForeColor = $cInk
$text.AutoSize = $false
$text.SetBounds(24,0,412,70)
$form.Controls.Add($text)

$see = New-Object System.Windows.Forms.LinkLabel
$see.Text = $cfg.see
$see.SetBounds(24,0,412,20)
$form.Controls.Add($see)

# BV-211 S4: one line per field, monospaced, vertical scroll. Go's indented
# JSON uses LF; a WinForms TextBox needs its Lines set so each field shows on
# its own line instead of a single strip.
$preview = New-Object System.Windows.Forms.TextBox
$preview.Multiline = $true
$preview.ReadOnly = $true
$preview.WordWrap = $false
$preview.ScrollBars = 'Vertical'
$preview.Font = $fMono
$preview.BackColor = $cPaper
$preview.Lines = [regex]::Split($cfg.preview, "\r?\n")
$preview.SetBounds(24,0,412,120)
$preview.Visible = $false
$form.Controls.Add($preview)

$fold = New-Object System.Windows.Forms.LinkLabel
$fold.Text = $cfg.fold
$fold.SetBounds(24,0,412,20)
$form.Controls.Add($fold)

$commentLbl = New-Object System.Windows.Forms.Label
$commentLbl.Text = $cfg.comment_q
$commentLbl.AutoSize = $false
$commentLbl.SetBounds(24,0,412,18)
$commentLbl.Visible = $false
$form.Controls.Add($commentLbl)

$comment = New-Object System.Windows.Forms.TextBox
$comment.Multiline = $true
$comment.ScrollBars = 'Vertical'
$comment.SetBounds(24,0,412,44)
$comment.Visible = $false
$form.Controls.Add($comment)

$emailLbl = New-Object System.Windows.Forms.Label
$emailLbl.Text = $cfg.email_q
$emailLbl.AutoSize = $false
$emailLbl.SetBounds(24,0,412,18)
$emailLbl.Visible = $false
$form.Controls.Add($emailLbl)

$email = New-Object System.Windows.Forms.TextBox
$email.SetBounds(24,0,412,24)
$email.Visible = $false
$form.Controls.Add($email)

$remember = New-Object System.Windows.Forms.CheckBox
$remember.Text = $cfg.remember
$remember.AutoSize = $true
$remember.SetBounds(24,0,412,22)
$form.Controls.Add($remember)

$send = New-Object System.Windows.Forms.Button
$send.Text = $cfg.send
$send.BackColor = $cCobalt
$send.ForeColor = $cWhite
$send.FlatStyle = 'Flat'
$send.FlatAppearance.BorderSize = 0
$send.SetBounds(236,0,100,32)
$form.Controls.Add($send)

$no = New-Object System.Windows.Forms.Button
$no.Text = $cfg.not_now
$no.BackColor = $cWhite
$no.ForeColor = $cInk
$no.FlatStyle = 'Flat'
$no.FlatAppearance.BorderColor = $cBorder
$no.SetBounds(120,0,110,32)
$form.Controls.Add($no)

# BV-211 S4: lay the window out to fit what is visible, with no white gap when
# the folds are closed, and grow it when one opens.
function Relayout {
  $y = 20
  $title.Top = $y; $y += 36
  $text.Top = $y; $y += 74
  $see.Top = $y; $y += 24
  if ($preview.Visible) { $preview.Top = $y; $y += $preview.Height + 10 }
  $fold.Top = $y; $y += 26
  if ($comment.Visible) {
    $commentLbl.Top = $y; $y += 20
    $comment.Top = $y; $y += $comment.Height + 8
    $emailLbl.Top = $y; $y += 20
    $email.Top = $y; $y += $email.Height + 10
  }
  $remember.Top = $y; $y += 32
  $send.Top = $y; $no.Top = $y; $y += 32 + 20
  $form.ClientSize = New-Object System.Drawing.Size(460, $y)
}

$see.Add_LinkClicked({ $preview.Visible = -not $preview.Visible; Relayout })

$foldState = $false
$fold.Add_LinkClicked({
  $script:foldState = -not $script:foldState
  $commentLbl.Visible = $script:foldState
  $comment.Visible = $script:foldState
  $emailLbl.Visible = $script:foldState
  $email.Visible = $script:foldState
  Relayout
})

Relayout
$script:decision = 'cancel'
$send.Add_Click({ $script:decision = 'send'; $form.Close() })
$no.Add_Click({ $script:decision = 'cancel'; $form.Close() })
$form.AcceptButton = $send

[void]$form.ShowDialog()

function Enc([string]$s) { if ([string]::IsNullOrEmpty($s)) { return '' } else { return [Convert]::ToBase64String([System.Text.Encoding]::UTF8.GetBytes($s)) } }
$rememberOut = '0'; if ($remember.Checked) { $rememberOut = '1' }
[Console]::Out.WriteLine($script:decision)
[Console]::Out.WriteLine($rememberOut)
[Console]::Out.WriteLine((Enc $comment.Text))
[Console]::Out.WriteLine((Enc $email.Text))
`

// problemBoxScript draws the box from the base64 JSON config on stdin, prints
// "primary" or "secondary" on the click, and auto-closes after 60 s as primary.
const problemBoxScript = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
Add-Type -AssemblyName System.Drawing
[System.Windows.Forms.Application]::EnableVisualStyles()
$b64 = [Console]::In.ReadLine()
$cfg = [System.Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($b64)) | ConvertFrom-Json

$cInk    = [System.Drawing.Color]::FromArgb(17,24,39)
$cCobalt = [System.Drawing.Color]::FromArgb(37,99,235)
$cWhite  = [System.Drawing.Color]::White
$cBorder = [System.Drawing.Color]::FromArgb(209,213,219)

$form = New-Object System.Windows.Forms.Form
$form.Text = 'RosmarOps'
$form.StartPosition = 'CenterScreen'
$form.FormBorderStyle = 'FixedDialog'
$form.MaximizeBox = $false
$form.MinimizeBox = $false
$form.BackColor = $cWhite
$form.ClientSize = New-Object System.Drawing.Size(420,130)
$form.Font = New-Object System.Drawing.Font('Segoe UI',10)
try { if ($cfg.icon -and (Test-Path $cfg.icon)) { $form.Icon = [System.Drawing.Icon]::ExtractAssociatedIcon($cfg.icon) } } catch {}

$label = New-Object System.Windows.Forms.Label
$label.Text = $cfg.body
$label.ForeColor = $cInk
$label.AutoSize = $false
$label.SetBounds(24,20,372,56)
$form.Controls.Add($label)

$script:choice = 'primary'

$primary = New-Object System.Windows.Forms.Button
$primary.Text = $cfg.primary
$primary.BackColor = $cCobalt
$primary.ForeColor = $cWhite
$primary.FlatStyle = 'Flat'
$primary.FlatAppearance.BorderSize = 0
$primary.SetBounds(320,88,76,30)
$primary.Add_Click({ $script:choice = 'primary'; $form.Close() })
$form.Controls.Add($primary)
$form.AcceptButton = $primary

if ($cfg.secondary) {
  $secondary = New-Object System.Windows.Forms.Button
  $secondary.Text = $cfg.secondary
  $secondary.BackColor = $cWhite
  $secondary.ForeColor = $cInk
  $secondary.FlatStyle = 'Flat'
  $secondary.FlatAppearance.BorderColor = $cBorder
  $secondary.SetBounds(96,88,216,30)
  $secondary.Add_Click({ $script:choice = 'secondary'; $form.Close() })
  $form.Controls.Add($secondary)
}

$timer = New-Object System.Windows.Forms.Timer
$timer.Interval = 60000
$timer.Add_Tick({ $script:choice = 'primary'; $timer.Stop(); $form.Close() })
$timer.Start()

[void]$form.ShowDialog()
[Console]::Out.WriteLine($script:choice)
`
