package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/qtorb/rosmarops/internal/notify"
)

// A scheduled review's notice (decision 2026-10-26, integridad y revisión
// semanal, J7, J9, J10): Windows's notification when it allows one;
// otherwise, or when showing it fails, the report opened in the browser.
// The log always says which.

// notifier runs the notification script (notify.Run) and opens the report
// (notify.OpenReport); tests replace both.
type notifier struct {
	run  func(ctx context.Context, toastXML string) (setting string, err error)
	open func(path string) error
}

var defaultNotifier = notifier{run: notify.Run, open: notify.OpenReport}

// notice is what the probe found: whether Windows will show the
// notification, and the report's code when it will not for a known reason.
type notice struct {
	enabled bool
	code    string // notification_unavailable; empty for an unknown setting
}

// probe reads ToastNotifier.Setting before the report is written, so that
// the report says why the notification will not show. An unknown setting
// is unavailable too, but only the log names it (it is not in the schema).
func (n notifier) probe(ctx context.Context) notice {
	setting, err := n.run(ctx, "")
	if err != nil {
		slog.Warn(fmt.Sprintf("notify: failed err=%v", err))
		return notice{}
	}
	if setting == notify.Enabled {
		return notice{enabled: true}
	}
	slog.Warn("notify: unavailable setting=" + setting)
	code, _ := notify.Unavailable(setting)
	return notice{code: code}
}

// errNoText: a finished review without a scored agent has no notification
// text.
var errNoText = errors.New("no notification text")

// finish shows text (ok: there is one), opening reportPath on click; when
// the probe found it unavailable, when there is no text, or when showing it
// fails or Windows no longer allows it, it opens the report instead.
func (n notifier) finish(ctx context.Context, nt notice, text string, ok bool, reportPath string) {
	if nt.enabled {
		if !ok {
			slog.Warn(fmt.Sprintf("notify: failed err=%v", errNoText))
		} else {
			setting, err := n.run(ctx, notify.ToastXML(text, notify.FileURI(reportPath)))
			switch {
			case err != nil:
				slog.Warn(fmt.Sprintf("notify: failed err=%v", err))
			case setting != notify.Enabled:
				slog.Warn("notify: unavailable setting=" + setting)
			default:
				slog.Info("notify: shown")
				return
			}
		}
	}
	if err := n.open(reportPath); err != nil {
		slog.Warn(fmt.Sprintf("notify: report not opened err=%v", err))
		return
	}
	slog.Info("notify: report opened")
}

// announce is probe and finish at once, for an abort's report.
func (n notifier) announce(ctx context.Context, text, reportPath string) {
	n.finish(ctx, n.probe(ctx), text, true, reportPath)
}
