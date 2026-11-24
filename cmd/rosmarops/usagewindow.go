package main

import (
	"bufio"
	"io"
	"log/slog"
	"strings"
	"time"
)

// The usage statistics window's limits (BV-286). No window may hold the run
// without one: a window nobody answers within usageAnswerLimit closes as if
// the X had been pressed (nothing recorded); after «Yes, help» the window
// shows its thank-you, which closes by itself after 60 seconds, and
// usageThanksLimit is that plus a margin before RosmarOps ends it.
const (
	usageAnswerLimit = 10 * time.Minute
	usageThanksLimit = 70 * time.Second
)

// usageSelfTestEnv is the usage window's self-test hook: with it set, the
// window reports its layout and closes itself as «later» (the journeys on
// Windows). A person never sets it.
const usageSelfTestEnv = "ROSMAROPS_USAGE_WINDOW_SELFTEST"

// readUsageWindow reads the window's lines and delivers its answer: SHOWN once
// it has painted (logged under the self-test hook), then "yes", "no" or
// "later". Other lines (the self-test's layout) are ignored. A stream that
// ends before SHOWN is usageWindowNotShown; one that ends after SHOWN without
// an answer is usageWindowLater.
func readUsageWindow(out io.Reader) <-chan usageWindowAnswer {
	done := make(chan usageWindowAnswer, 1)
	go func() {
		r := bufio.NewReader(out)
		shown := false
		for {
			line, err := r.ReadString('\n')
			switch t := strings.TrimRight(line, "\r\n"); {
			case t == "SHOWN":
				if !shown {
					noteSelfTestShown(t, "usage", usageSelfTestEnv)
				}
				shown = true
			case shown && t == "yes":
				done <- usageWindowYes
				return
			case shown && t == "no":
				done <- usageWindowNo
				return
			case shown && t == "later":
				done <- usageWindowLater
				return
			}
			if err != nil {
				if shown {
					done <- usageWindowLater
				} else {
					done <- usageWindowNotShown
				}
				return
			}
		}
	}()
	return done
}

// awaitUsageWindow waits for the window's answer; with none within limit it
// ends the window (kill) and counts it as «later», as the X. after is
// time.After in production.
func awaitUsageWindow(done <-chan usageWindowAnswer, limit time.Duration, after func(time.Duration) <-chan time.Time, kill func()) usageWindowAnswer {
	select {
	case a := <-done:
		return a
	case <-after(limit):
		slog.Info("usage window: no answer in 10 minutes; closing it and asking again next time")
		kill()
		<-done
		return usageWindowLater
	}
}
