package main

import (
	"testing"

	"github.com/qtorb/rosmarops/internal/state"
	"github.com/qtorb/rosmarops/internal/usage"
)

// A build that is not a published version (no -X main.version, a journey's
// candidate, a prerelease) never asks about usage statistics and never sends
// them, even with the state recording them on; a published version does.
func TestUsageOnlyPublishedVersionsAskOrSend(t *testing.T) {
	on := &state.Usage{Enabled: true, DecidedAt: "2026-11-18T12:00:00Z", TextVersion: usage.TextVersion, InstallID: "3b2c1d4e-5f60-4a7b-8c9d-0e1f2a3b4c5d"}
	for _, tc := range []struct {
		version string
		sends   bool
	}{
		{"0.1.0-dev", false},
		{"0.19.1-journeys.42", false},
		{"0.5.0-alpha.1", false},
		{"", false},
		{"v0.19.0", false},
		{"0.19.0", true},
		{"1.10.200", true},
	} {
		// On: sends only from a published version.
		stPath, sends := usageTestHome(t, on, "")
		version = tc.version
		afterReportUsage(t.TempDir(), "en", false)
		if got := *sends == 1; got != tc.sends {
			t.Errorf("%q, usage on: sent %d times, want sends=%v", tc.version, *sends, tc.sends)
		}
		if !loadUsage(t, stPath).Enabled {
			t.Errorf("%q: the person's answer changed", tc.version)
		}
		// No answer, someone at a console answering «yes»: asked (and so
		// recorded and sent) only from a published version.
		stPath, sends = usageTestHome(t, nil, "y\n")
		version = tc.version
		afterReportUsage(t.TempDir(), "en", true)
		if asked := loadUsage(t, stPath) != nil; asked != tc.sends || (*sends == 1) != tc.sends {
			t.Errorf("%q, no answer: asked %v, sent %d times, want %v", tc.version, asked, *sends, tc.sends)
		}
		// The line that says whether they are on.
		want := "usage.state.on"
		if !tc.sends {
			want = "usage.state.dev_build"
		}
		if got := usageStateKey(on); got != want {
			t.Errorf("%q: state line %s, want %s", tc.version, got, want)
		}
	}
}
