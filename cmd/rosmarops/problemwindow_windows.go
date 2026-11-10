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
	"github.com/qtorb/rosmarops/internal/winpath"
)

// problemWindowConfig is everything the «send a problem?» window draws, all text
// from the catalog, passed as base64 JSON on the first stdin line (BV-210 §3).
// It carries only what the person reads and the exact preview of what would be
// sent — never a path, name or key.
type problemWindowConfig struct {
	Title    string `json:"title"`
	Text     string `json:"text"`
	Send     string `json:"send"`
	NotNow   string `json:"not_now"`
	See      string `json:"see"`
	Preview  string `json:"preview"`
	Fold     string `json:"fold"`
	CommentQ string `json:"comment_q"`
	EmailQ   string `json:"email_q"`
	Remember string `json:"remember"`
	Icon     string `json:"icon"`
	EncProbe string `json:"enc_probe"`
}

const problemEncProbe = "·áñ"

func problemWindowConfigFrom(a problemAsk) problemWindowConfig {
	icon, _ := os.Executable()
	titleKey, textKey := "problems.a.title", "problems.a.text"
	if a.windowB {
		titleKey, textKey = "problems.b.title", "problems.b.text"
	}
	return problemWindowConfig{
		Title:    a.cat.Text(titleKey, nil),
		Text:     a.cat.Text(textKey, map[string]string{"summary": a.summary}),
		Send:     a.cat.Text("problems.send", nil),
		NotNow:   a.cat.Text("problems.not_now", nil),
		See:      a.cat.Text("problems.see", nil),
		Preview:  a.preview,
		Fold:     a.cat.Text("problems.comment.fold", nil),
		CommentQ: a.cat.Text("problems.comment.q", nil),
		EmailQ:   a.cat.Text("problems.email.q", nil),
		Remember: a.cat.Text("problems.remember", nil),
		Icon:     icon,
		EncProbe: problemEncProbe,
	}
}

func problemWindowInput(c problemWindowConfig) string {
	b, _ := json.Marshal(c)
	return base64.StdEncoding.EncodeToString(b)
}

func problemWindowArgs() []string {
	return []string{"-NoProfile", "-NonInteractive", "-STA", "-Command", problemWindowScript}
}

// driveProblemWindow reads the window's answer (BV-210 §3): four lines —
// "send" or "cancel"; remember "1"/"0"; base64 comment; base64 email. A closed
// stream or an unexpected first line is «Not now».
func driveProblemWindow(out io.Reader) (problemAnswer, bool) {
	r := bufio.NewReader(out)
	first, _ := r.ReadString('\n')
	decision := strings.TrimRight(first, "\r\n")
	if decision != "send" && decision != "cancel" {
		return problemAnswer{}, false
	}
	rem, _ := r.ReadString('\n')
	cB, _ := r.ReadString('\n')
	eB, _ := r.ReadString('\n')
	ans := problemAnswer{
		send:     decision == "send",
		remember: strings.TrimRight(rem, "\r\n") == "1",
		comment:  b64Decode(strings.TrimRight(cB, "\r\n")),
		email:    b64Decode(strings.TrimRight(eB, "\r\n")),
	}
	return ans, true
}

func b64Decode(s string) string {
	if s == "" {
		return ""
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return ""
	}
	return string(b)
}

// askProblem shows the «send a problem?» window on Windows and returns the
// answer and whether the person was asked. Any failure to show it is logged and
// treated as «Not now» (not asked).
func askProblem(a problemAsk) (problemAnswer, bool) {
	ps := winpath.PowerShellExe()
	if ps == "" {
		ps = "powershell"
	}
	cmd := exec.Command(ps, problemWindowArgs()...)
	cmd.Env = winpath.ChildEnv(os.Environ())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return problemAnswer{}, false
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return problemAnswer{}, false
	}
	if err := cmd.Start(); err != nil {
		slog.Info("problem window not shown", "err", err)
		return problemAnswer{}, false
	}
	if _, err := stdin.Write([]byte(problemWindowInput(problemWindowConfigFrom(a)) + "\n")); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return problemAnswer{}, false
	}
	type res struct {
		ans   problemAnswer
		asked bool
	}
	done := make(chan res, 1)
	go func() {
		ans, asked := driveProblemWindow(stdout)
		done <- res{ans, asked}
	}()
	var out res
	select {
	case out = <-done:
	case <-time.After(15 * time.Minute):
		slog.Info("problem window: no answer in 15 minutes; closing as «Not now»")
		_ = cmd.Process.Kill()
		<-done
	}
	_ = stdin.Close()
	_ = cmd.Wait()
	return out.ans, out.asked
}

// showProblemOutcome shows the «Sent»/«couldn't be sent» message after the send.
func showProblemOutcome(cat catalog.Catalog, delivered bool, email string) {
	msg := cat.Text("problems.fail", nil)
	if delivered {
		msg = cat.Text("problems.sent", nil)
		if email != "" {
			msg += " " + cat.Text("problems.sent.email", map[string]string{"email": email})
		}
	}
	ps := winpath.PowerShellExe()
	if ps == "" {
		ps = "powershell"
	}
	cmd := exec.Command(ps, "-NoProfile", "-NonInteractive", "-STA", "-Command", problemOutcomeScript)
	cmd.Env = winpath.ChildEnv(os.Environ())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return
	}
	if err := cmd.Start(); err != nil {
		return
	}
	_, _ = stdin.Write([]byte(base64.StdEncoding.EncodeToString([]byte(msg)) + "\n"))
	_ = stdin.Close()
	_ = cmd.Wait()
}

// problemOutcomeScript shows a plain message box with the base64 text on stdin.
const problemOutcomeScript = `
$ErrorActionPreference = 'Stop'
Add-Type -AssemblyName System.Windows.Forms
$b64 = [Console]::In.ReadLine()
$msg = [System.Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($b64))
[void][System.Windows.Forms.MessageBox]::Show($msg, 'RosmarOps')
`

// problemWindowScript is the WinForms window. It reads the base64 JSON config
// on the first stdin line, draws the question with a primary «Send» button, a
// «Not now» button, a «See what is sent» toggle that reveals the exact JSON, an
// «Add a comment» toggle that reveals a comment box and an email box, and the
// «send the next ones without asking» checkbox. On Send/Not now/close it prints
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
$form.ClientSize = New-Object System.Drawing.Size(460,300)
$form.Font = $fBody
try { if ($cfg.icon -and (Test-Path $cfg.icon)) { $form.Icon = [System.Drawing.Icon]::ExtractAssociatedIcon($cfg.icon) } } catch {}

$title = New-Object System.Windows.Forms.Label
$title.Text = $cfg.title
$title.Font = $fTitle
$title.ForeColor = $cInk
$title.AutoSize = $false
$title.SetBounds(24,20,412,28)
$form.Controls.Add($title)

$text = New-Object System.Windows.Forms.Label
$text.Text = $cfg.text
$text.Font = $fBody
$text.ForeColor = $cInk
$text.AutoSize = $false
$text.SetBounds(24,52,412,70)
$form.Controls.Add($text)

$see = New-Object System.Windows.Forms.LinkLabel
$see.Text = $cfg.see
$see.SetBounds(24,124,412,20)
$form.Controls.Add($see)

$preview = New-Object System.Windows.Forms.TextBox
$preview.Multiline = $true
$preview.ReadOnly = $true
$preview.ScrollBars = 'Vertical'
$preview.Font = $fMono
$preview.BackColor = $cPaper
$preview.Text = $cfg.preview
$preview.SetBounds(24,148,412,96)
$preview.Visible = $false
$form.Controls.Add($preview)
$see.Add_LinkClicked({ $preview.Visible = -not $preview.Visible })

$fold = New-Object System.Windows.Forms.LinkLabel
$fold.Text = $cfg.fold
$fold.SetBounds(24,148,412,20)
$form.Controls.Add($fold)

$commentLbl = New-Object System.Windows.Forms.Label
$commentLbl.Text = $cfg.comment_q
$commentLbl.AutoSize = $false
$commentLbl.SetBounds(24,172,412,18)
$commentLbl.Visible = $false
$form.Controls.Add($commentLbl)

$comment = New-Object System.Windows.Forms.TextBox
$comment.Multiline = $true
$comment.ScrollBars = 'Vertical'
$comment.SetBounds(24,192,412,44)
$comment.Visible = $false
$form.Controls.Add($comment)

$emailLbl = New-Object System.Windows.Forms.Label
$emailLbl.Text = $cfg.email_q
$emailLbl.AutoSize = $false
$emailLbl.SetBounds(24,240,412,18)
$emailLbl.Visible = $false
$form.Controls.Add($emailLbl)

$email = New-Object System.Windows.Forms.TextBox
$email.SetBounds(24,260,412,24)
$email.Visible = $false
$form.Controls.Add($email)

$remember = New-Object System.Windows.Forms.CheckBox
$remember.Text = $cfg.remember
$remember.AutoSize = $true
$remember.SetBounds(24,296,412,22)
$form.Controls.Add($remember)

$send = New-Object System.Windows.Forms.Button
$send.Text = $cfg.send
$send.BackColor = $cCobalt
$send.ForeColor = $cWhite
$send.FlatStyle = 'Flat'
$send.FlatAppearance.BorderSize = 0
$send.SetBounds(236,330,100,32)
$form.Controls.Add($send)

$no = New-Object System.Windows.Forms.Button
$no.Text = $cfg.not_now
$no.BackColor = $cWhite
$no.ForeColor = $cInk
$no.FlatStyle = 'Flat'
$no.FlatAppearance.BorderColor = $cBorder
$no.SetBounds(120,330,110,32)
$form.Controls.Add($no)

$foldState = $false
$fold.Add_LinkClicked({
  $script:foldState = -not $script:foldState
  $commentLbl.Visible = $script:foldState
  $comment.Visible = $script:foldState
  $emailLbl.Visible = $script:foldState
  $email.Visible = $script:foldState
})

$form.ClientSize = New-Object System.Drawing.Size(460,374)
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
