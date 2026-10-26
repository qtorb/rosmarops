package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// fakeNotifier records the script's runs (the probe with no XML, a show
// with one) and the report's openings.
type fakeNotifier struct {
	probeSetting, showSetting string
	probeErr, showErr         error
	openErr                   error
	shows, opens              []string
}

func (f *fakeNotifier) notifier() notifier {
	return notifier{
		run: func(_ context.Context, toastXML string) (string, error) {
			if toastXML == "" {
				return f.probeSetting, f.probeErr
			}
			f.shows = append(f.shows, toastXML)
			return f.showSetting, f.showErr
		},
		open: func(path string) error {
			f.opens = append(f.opens, path)
			return f.openErr
		},
	}
}

// captureLog sends slog to a buffer for the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

const reportPath = `C:\Users\ana\.rosmarops\reports\report.html`

// J9 and J10: Windows's notification when Setting is Enabled and showing
// works; otherwise the report opened with the opener and never Show; the
// log says which, every time.
func TestNotice(t *testing.T) {
	boom := errors.New("boom")
	for _, c := range []struct {
		name       string
		f          fakeNotifier
		text       bool
		code       string
		shown      bool
		logs       []string
		openFailed bool
	}{
		{name: "enabled", f: fakeNotifier{probeSetting: "Enabled", showSetting: "Enabled"}, text: true, shown: true, logs: []string{"notify: shown"}},
		{name: "disabled for user", f: fakeNotifier{probeSetting: "DisabledForUser"}, text: true, code: "disabled_for_user", logs: []string{"notify: unavailable setting=DisabledForUser", "notify: report opened"}},
		{name: "group policy", f: fakeNotifier{probeSetting: "DisabledByGroupPolicy"}, text: true, code: "disabled_by_group_policy", logs: []string{"notify: unavailable setting=DisabledByGroupPolicy", "notify: report opened"}},
		{name: "unknown setting", f: fakeNotifier{probeSetting: "SomethingNew"}, text: true, logs: []string{"notify: unavailable setting=SomethingNew", "notify: report opened"}},
		{name: "setting not read", f: fakeNotifier{probeErr: boom}, text: true, logs: []string{"notify: failed err=boom", "notify: report opened"}},
		{name: "show fails", f: fakeNotifier{probeSetting: "Enabled", showErr: boom}, text: true, shown: true, logs: []string{"notify: failed err=boom", "notify: report opened"}},
		{name: "turned off since", f: fakeNotifier{probeSetting: "Enabled", showSetting: "DisabledForUser"}, text: true, shown: true, logs: []string{"notify: unavailable setting=DisabledForUser", "notify: report opened"}},
		{name: "no text", f: fakeNotifier{probeSetting: "Enabled"}, logs: []string{"notify: failed err=no notification text", "notify: report opened"}},
		{name: "open fails", f: fakeNotifier{probeSetting: "DisabledForUser", openErr: boom}, text: true, code: "disabled_for_user", logs: []string{"notify: unavailable setting=DisabledForUser", "notify: report not opened err=boom"}, openFailed: true},
	} {
		log := captureLog(t)
		f := c.f
		n := f.notifier()
		nt := n.probe(context.Background())
		if nt.code != c.code || nt.enabled != (c.f.probeSetting == "Enabled" && c.f.probeErr == nil) {
			t.Errorf("%s: probe %+v", c.name, nt)
		}
		n.finish(context.Background(), nt, "RosmarOps: done", c.text, reportPath)
		if (len(f.shows) == 1) != c.shown || len(f.shows) > 1 {
			t.Errorf("%s: %d shows", c.name, len(f.shows))
		}
		wantOpen := !(c.shown && c.f.showErr == nil && c.f.showSetting == "Enabled")
		if wantOpen != (len(f.opens) == 1) || len(f.opens) > 1 || (wantOpen && f.opens[0] != reportPath) {
			t.Errorf("%s: opens %q, want open %v", c.name, f.opens, wantOpen)
		}
		if c.shown && !strings.Contains(f.shows[0], `launch="file:///C:/Users/ana/.rosmarops/reports/report.html"`) {
			t.Errorf("%s: toast %s", c.name, f.shows[0])
		}
		got := log.String()
		at := 0
		for _, want := range c.logs {
			i := strings.Index(got[at:], want)
			if i < 0 {
				t.Errorf("%s: no %q (in order) in the log:\n%s", c.name, want, got)
				break
			}
			at += i + len(want)
		}
		for _, other := range []string{"notify: shown", "notify: report opened", "notify: report not opened"} {
			listed := false
			for _, want := range c.logs {
				listed = listed || strings.HasPrefix(want, other)
			}
			if !listed && strings.Contains(got, other) {
				t.Errorf("%s: unexpected %q in the log:\n%s", c.name, other, got)
			}
		}
	}
}

// J9: an abort's notice follows the same rule.
func TestNoticeAnnounce(t *testing.T) {
	for _, setting := range []string{"Enabled", "DisabledForUser"} {
		log := captureLog(t)
		f := fakeNotifier{probeSetting: setting, showSetting: setting}
		f.notifier().announce(context.Background(), "RosmarOps: stopped", reportPath)
		enabled := setting == "Enabled"
		if (len(f.shows) == 1) != enabled || (len(f.opens) == 1) == enabled {
			t.Errorf("%s: shows %d, opens %d", setting, len(f.shows), len(f.opens))
		}
		want := "notify: report opened"
		if enabled {
			want = "notify: shown"
		}
		if !strings.Contains(log.String(), want) {
			t.Errorf("%s: no %q in %s", setting, want, log.String())
		}
	}
}
