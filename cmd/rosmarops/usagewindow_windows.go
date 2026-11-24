//go:build windows

package main

import (
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

// usageWindowConfig is the usage statistics window's text, all from the
// catalog, passed as base64 JSON on the first stdin line so no code page
// touches it (as the connect window's).
type usageWindowConfig struct {
	Title        string `json:"title"`
	TitleText    string `json:"titletext"`
	Why          string `json:"why"`
	Body         string `json:"body"`
	Never        string `json:"never"`
	Check        string `json:"check"`
	Yes          string `json:"yes"`
	No           string `json:"no"`
	Thanks       string `json:"thanks"`
	OK           string `json:"ok"`
	Icon         string `json:"icon"`
	IconFallback string `json:"iconfallback"`
}

func usageWindowConfigFrom(cat catalog.Catalog) usageWindowConfig {
	icon, _ := os.Executable()
	return usageWindowConfig{
		Title:     "RosmarOps",
		TitleText: cat.Text("usage.ask.title", nil),
		Why:       cat.Text("usage.ask.why", nil),
		Body:      cat.Text("usage.ask.body", nil),
		Never:     cat.Text("usage.ask.never", nil),
		Check:     cat.Text("usage.ask.check", nil),
		Yes:       cat.Text("usage.window.yes", nil),
		No:        cat.Text("usage.window.no", nil),
		Thanks:    cat.Text("usage.window.thanks", nil),
		OK:        cat.Text("usage.window.ok", nil),
		Icon:      icon,
	}
}

func usageWindowInput(c usageWindowConfig) string {
	b, _ := json.Marshal(c)
	return base64.StdEncoding.EncodeToString(b)
}

// askUsageWindow shows the usage statistics window (BV-286) and returns the
// person's answer, and a function the caller runs once it has recorded the
// answer and sent this review's summary: after «Yes, help» the window stays
// on its thank-you (OK, the X, or 60 seconds), and finish waits for it to
// close. A window that could not be shown, or that ended before it painted,
// is usageWindowNotShown: the question is then asked in the console.
func askUsageWindow(cat catalog.Catalog) (usageWindowAnswer, func()) {
	// PowerShell by absolute path, with PSModulePath narrowed to the system
	// modules, as for every RosmarOps window.
	ps := winpath.PowerShellExe()
	if ps == "" {
		ps = "powershell"
	}
	cmd := exec.Command(ps, windowCmdArgs()...)
	cmd.Env = winpath.ChildEnv(os.Environ())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		recordWindowFailure()
		return usageWindowNotShown, nil
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		recordWindowFailure()
		return usageWindowNotShown, nil
	}
	if err := cmd.Start(); err != nil {
		slog.Info("usage window not shown", "err", err)
		recordWindowFailure()
		return usageWindowNotShown, nil
	}
	if err := writeWindowScript(stdin, usageWindowScript); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		recordWindowFailure()
		return usageWindowNotShown, nil
	}
	fmt.Fprintln(stdin, usageWindowInput(usageWindowConfigFrom(cat)))
	ans := awaitUsageWindow(readUsageWindow(stdout), usageAnswerLimit, time.After, func() { _ = cmd.Process.Kill() })
	if ans == usageWindowNotShown {
		slog.Info("usage window ended before it showed; asking in the console")
		recordWindowFailure()
	}
	limit := windowExitLimit
	if ans == usageWindowYes {
		limit = usageThanksLimit
	}
	return ans, func() {
		_ = stdin.Close()
		waitWindowExit(cmd, "usage", limit)
	}
}
