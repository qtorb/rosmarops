package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"runtime"
	"time"

	"github.com/qtorb/rosmarops/internal/problems"
	"github.com/qtorb/rosmarops/internal/spacenet"
)

// sendProblemsFn is the network seam for the problem report send (BV-210 §4):
// production is spacenet.SendProblems (host-pinned to app.rosmarops.com:443,
// POST /api/v1/problems, no account, credential or cookie); tests point it at a
// local server. It is the only way this package reaches the problems endpoint.
var sendProblemsFn = spacenet.SendProblems

// problemsSendResult is the outcome of trying to send the problem notebook.
type problemsSendResult struct {
	Sent      int  // how many problems were accepted (marked sent) — 0 unless delivered
	Attempted bool // there were unsent problems and a send was tried
	Delivered bool // the endpoint accepted the report (HTTP 204)
}

// osVersionFn returns the OS version string for the report (§11 os_version,
// optional and may be empty). A seam so tests are deterministic and a platform
// can fill it in without changing the caller. The value is only the OS version,
// never anything that identifies the person or the machine.
var osVersionFn = func() string { return "" }

// submitProblems builds a problems.v1 report from the unsent notebook lines (at
// most 20), validates it, and sends it to app.rosmarops.com — but only when the
// caller has the person's permission (the «Send» button or the saved checkbox).
// On HTTP 204 the sent lines are marked sent; otherwise they stay unsent for
// next time. Every attempt is written to the sends log. It never panics and
// never fails the run: a build/validate failure means nothing is sent and a
// "failed" line is recorded. comment and email are optional (nil → null).
func submitProblems(ctx context.Context, dataDir, lang, version string, comment, email *string, now time.Time) problemsSendResult {
	nb := problems.New(dataDir)
	unsent, err := nb.Unsent(problems.MaxPerSend)
	if err != nil {
		slog.Warn("problems send: read notebook", "err", err)
		return problemsSendResult{}
	}
	if len(unsent) == 0 {
		return problemsSendResult{}
	}

	logPath := filepath.Join(dataDir, SendsLogName)
	sys := problems.System{
		Lang:      lang,
		Version:   version,
		OS:        runtime.GOOS,
		OSVersion: osVersionFn(),
		Arch:      runtime.GOARCH,
	}
	body, err := problems.Marshal(problems.BuildReport(sys, unsent, comment, email))
	if err != nil {
		// Did not pass its own schema check: never sent, and recorded (§2, §7).
		slog.Warn("problems send: report did not validate; not sent", "err", err)
		appendSendLog(logPath, now, "rosmarops-problems.v1", 0, sendFailed)
		return problemsSendResult{Attempted: true}
	}
	if len(body) > problems.MaxBodyBytes {
		slog.Warn("problems send: body too large; not sent", "bytes", len(body))
		appendSendLog(logPath, now, "rosmarops-problems.v1", len(body), sendTooLarge)
		return problemsSendResult{Attempted: true}
	}

	status, sendErr := sendProblemsFn(ctx, body)
	result := problemsResultFor(status, sendErr)
	appendSendLog(logPath, now, "rosmarops-problems.v1", len(body), result)
	if result != sendSent {
		slog.Info("problems send: not delivered; kept for next time", "status", status)
		return problemsSendResult{Attempted: true}
	}
	if _, err := nb.MarkSent(unsent); err != nil {
		slog.Warn("problems send: delivered but could not mark sent", "err", err)
	}
	return problemsSendResult{Sent: len(unsent), Attempted: true, Delivered: true}
}

// problemsResultFor maps the endpoint's status (0 on a network error) to a
// sends-log result. 204 is the only success (§11); 400/413 are terminal, 429
// and 5xx and a network error are kept for next time.
func problemsResultFor(status int, err error) string {
	if err != nil {
		return sendDeferred
	}
	switch status {
	case 204:
		return sendSent
	case 400:
		return sendBadRequest
	case 413:
		return sendTooLarge
	case 429:
		return sendDeferred
	default:
		if status >= 500 {
			return sendDeferred
		}
		return sendError
	}
}
