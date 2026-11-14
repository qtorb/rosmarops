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
	"strconv"
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
	cmd := exec.Command(ps, windowCmdArgs()...)
	cmd.Env = winpath.ChildEnv(os.Environ())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		recordWindowFailure()
		return problemAnswer{}, false
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		recordWindowFailure()
		return problemAnswer{}, false
	}
	if err := cmd.Start(); err != nil {
		slog.Info("problem window not shown", "err", err)
		recordWindowFailure()
		return problemAnswer{}, false
	}
	if err := writeWindowScript(stdin, problemWindowScript); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		recordWindowFailure()
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

// problemBoxConfig is a small message box: a body, a primary button and an
// optional secondary one. It closes itself after 60 seconds as the primary
// button (CTO-32 C3), so it never waits without a tope.
type problemBoxConfig struct {
	Body      string `json:"body"`
	Primary   string `json:"primary"`
	Secondary string `json:"secondary"` // empty for a one-button box
	Icon      string `json:"icon"`
}

// runProblemBox shows the box and returns whether the secondary button was
// clicked. A failure, a timeout, or the primary button all return false.
func runProblemBox(cfg problemBoxConfig) bool {
	ps := winpath.PowerShellExe()
	if ps == "" {
		ps = "powershell"
	}
	cmd := exec.Command(ps, windowCmdArgs()...)
	cmd.Env = winpath.ChildEnv(os.Environ())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return false
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false
	}
	if err := cmd.Start(); err != nil {
		slog.Info("problem box not shown", "err", err)
		return false
	}
	if err := writeWindowScript(stdin, problemBoxScript); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return false
	}
	b, _ := json.Marshal(cfg)
	if _, err := stdin.Write([]byte(base64.StdEncoding.EncodeToString(b) + "\n")); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return false
	}
	done := make(chan string, 1)
	go func() {
		r := bufio.NewReader(stdout)
		line, _ := r.ReadString('\n')
		done <- strings.TrimRight(line, "\r\n")
	}()
	var choice string
	select {
	case choice = <-done:
	case <-time.After(2 * time.Minute):
		_ = cmd.Process.Kill()
		<-done
	}
	_ = stdin.Close()
	_ = cmd.Wait()
	return choice == "secondary"
}

func problemBoxIcon() string {
	icon, _ := os.Executable()
	return icon
}

// showProblemOutcome shows the «Sent»/«couldn't be sent» message after the send;
// the box closes itself after 60 seconds (CTO-32 C3).
func showProblemOutcome(cat catalog.Catalog, delivered bool, email string) {
	msg := cat.Text("problems.fail", nil)
	if delivered {
		msg = cat.Text("problems.sent", nil)
		if email != "" {
			msg += " " + cat.Text("problems.sent.email", map[string]string{"email": email})
		}
	}
	runProblemBox(problemBoxConfig{Body: msg, Primary: cat.Text("problems.ok", nil), Icon: problemBoxIcon()})
}

// showProblemAutoSent is the box after an automatic send with someone present
// (CTO-32 C4.1): «We sent {n}…» with «OK» (primary) and «Ask me before sending»
// (secondary). It returns true when the person chose to be asked again; it
// closes itself after 60 seconds as «OK» (false).
func showProblemAutoSent(cat catalog.Catalog, n int) bool {
	return runProblemBox(problemBoxConfig{
		Body:      cat.Text(cat.One("problems.autosent.body", n), map[string]string{"n": strconv.Itoa(n)}),
		Primary:   cat.Text("problems.ok", nil),
		Secondary: cat.Text("problems.ask_again", nil),
		Icon:      problemBoxIcon(),
	})
}
