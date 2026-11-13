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
	"sync"
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
	// embedded rosemary icon (BV-178). Empty when it cannot be resolved.
	Icon string `json:"icon"`
	// IconFallback is a .ico used when Icon carries none (a test binary): the
	// repo's assets/windows/rosmarops.ico. Empty in production (the installed
	// exe always carries the icon).
	IconFallback string `json:"iconfallback"`
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
	cmd := exec.Command(ps, windowCmdArgs()...)
	cmd.Env = winpath.ChildEnv(os.Environ())
	stdin, err := cmd.StdinPipe()
	if err != nil {
		recordWindowFailure()
		return connectNotOffered
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		recordWindowFailure()
		return connectNotOffered
	}
	if err := cmd.Start(); err != nil {
		slog.Info("connect window not shown", "err", err)
		recordWindowFailure()
		return connectNotOffered
	}
	if err := writeWindowScript(stdin, connectWindowScript); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		recordWindowFailure()
		return connectNotOffered
	}
	fmt.Fprintln(stdin, connectWindowInput(connectWindowConfigFrom(cat)))
	run := func(onNumber func(int), onOpen func(string, string) error) error {
		ctx, cancel := context.WithTimeout(context.Background(), 11*time.Minute)
		defer cancel()
		return spaceConnect(ctx, paths, onNumber, onOpen)
	}
	// If nobody answers the window within 10 minutes, close it as «Not now»
	// and let the run go on (CTO-15 PR5). BV-227 §3: that limit is only for
	// answering; once «Save to my space» is pressed, the 10 minutes start
	// again for the connection itself. Killing the process closes its stdout,
	// so driveConnectWindow returns.
	done := make(chan connectResult, 1)
	pressed := make(chan struct{})
	var once sync.Once
	go func() {
		done <- driveConnectWindow(stdout, stdin, cat, func() { once.Do(func() { close(pressed) }) }, run)
	}()
	result := awaitConnectWindow(done, pressed, 10*time.Minute, time.After, func() { _ = cmd.Process.Kill() })
	_ = stdin.Close()
	_ = cmd.Wait()
	return result
}
