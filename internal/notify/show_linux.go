//go:build linux

package notify

import (
	"context"
	"encoding/xml"
	"fmt"
	"os/exec"
	"time"

	"github.com/qtorb/rosmarops/internal/platform"
)

// linuxNotifyTimeout bounds the notify-send call.
const linuxNotifyTimeout = 30 * time.Second

// hasCmd reports whether name is found on the PATH.
func hasCmd(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// Run shows the scheduled review's notification with notify-send when it is
// available (BV-165). On the probe call (empty payload) it reports Enabled if
// notify-send is on the PATH, otherwise platform.ErrNotApplicable (no
// notification, and no code in the report). On the show call it sends the
// toast's text with notify-send; a failure is reported so the caller opens the
// report instead.
func Run(ctx context.Context, toastXML string) (string, error) {
	if !hasCmd("notify-send") {
		return "", fmt.Errorf("notify: %w", platform.ErrNotApplicable)
	}
	if toastXML == "" {
		return Enabled, nil
	}
	ctx, cancel := context.WithTimeout(ctx, linuxNotifyTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, "notify-send", "RosmarOps", toastText(toastXML)).Run(); err != nil {
		return "", fmt.Errorf("notify: notify-send: %w", err)
	}
	return Enabled, nil
}

// toastText is the one line of text inside a ToastXML; «RosmarOps» if it cannot
// be parsed. The same format ToastXML writes, read back for notify-send.
func toastText(x string) string {
	var t struct {
		Text string `xml:"visual>binding>text"`
	}
	if err := xml.Unmarshal([]byte(x), &t); err == nil && t.Text != "" {
		return t.Text
	}
	return "RosmarOps"
}

// OpenReport opens the report in the browser with xdg-open, then wslview (for
// WSL); if neither exists or both fail, platform.ErrNotApplicable so the caller
// falls back to console.report_not_opened (BV-165).
func OpenReport(path string) error {
	for _, name := range []string{"xdg-open", "wslview"} {
		if openLinuxWith(name, path) {
			return nil
		}
	}
	return fmt.Errorf("notify: open report: %w", platform.ErrNotApplicable)
}

// openLinuxWith runs name on path and reports whether it was found and
// succeeded.
func openLinuxWith(name, path string) bool {
	if !hasCmd(name) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), openTimeout)
	defer cancel()
	return exec.CommandContext(ctx, name, path).Run() == nil
}
