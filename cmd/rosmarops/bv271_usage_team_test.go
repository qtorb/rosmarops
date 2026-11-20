package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/cli"
	"github.com/qtorb/rosmarops/internal/schemas"
	"github.com/qtorb/rosmarops/internal/schemaval"
	"github.com/qtorb/rosmarops/internal/spacenet"
	"github.com/qtorb/rosmarops/internal/state"
	"github.com/qtorb/rosmarops/internal/usage"
)

// runUsageOut runs `rosmarops usage <args>` and returns what it printed.
func runUsageOut(t *testing.T, lang string, args ...string) (string, error) {
	t.Helper()
	r, w, _ := os.Pipe()
	prev := os.Stdout
	os.Stdout = w
	err := runUsage(cli.Invocation{Flags: map[string]string{"lang": lang}, Args: args})
	w.Close()
	os.Stdout = prev
	b, _ := io.ReadAll(r)
	return strings.TrimSpace(string(b)), err
}

func loadState(t *testing.T, stPath string) state.State {
	t.Helper()
	st, err := state.LoadState(stPath)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

// `usage team on|off` records the mark, prints its signed line, and never
// touches the person's answer about usage statistics.
func TestUsageTeamCommand(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		stPath, sends := usageTestHome(t, nil, "")
		cat, _ := catalog.Load(lang)
		got, err := runUsageOut(t, lang, "team", "on")
		if err != nil || got != cat.Text("console.usage_team_on", nil) {
			t.Fatalf("%s team on: %q %v", lang, got, err)
		}
		st := loadState(t, stPath)
		if !st.UsageTeam || st.Usage != nil {
			t.Errorf("%s team on: team %v, usage %+v: the answer must stay unanswered", lang, st.UsageTeam, st.Usage)
		}
		got, err = runUsageOut(t, lang, "team", "off")
		if err != nil || got != cat.Text("console.usage_team_off", nil) {
			t.Fatalf("%s team off: %q %v", lang, got, err)
		}
		if st := loadState(t, stPath); st.UsageTeam || st.Usage != nil {
			t.Errorf("%s team off: team %v, usage %+v", lang, st.UsageTeam, st.Usage)
		}
		if *sends != 0 {
			t.Errorf("%s: the command sent %d packages", lang, *sends)
		}
	}
	// The usage answer never changes the mark, and the mark never the answer.
	stPath, _ := usageTestHome(t, nil, "")
	for _, args := range [][]string{{"team", "on"}, {"on"}, {"off"}} {
		if _, err := runUsageOut(t, "en", args...); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if st := loadState(t, stPath); !st.UsageTeam || st.Usage == nil || st.Usage.Enabled {
		t.Errorf("after team on, on, off: team %v, usage %+v", st.UsageTeam, st.Usage)
	}
	for _, args := range [][]string{{"team"}, {"team", "yes"}, {"team", "on", "now"}} {
		if _, err := runUsageOut(t, "en", args...); err == nil {
			t.Errorf("%v: no error", args)
		}
	}
}

// A team installation sends with the channel "internal" and nothing else
// changes; an installation not marked sends its real channel. With usage
// statistics off or unanswered, a team installation sends nothing.
func TestUsageTeamChannel(t *testing.T) {
	sv, err := schemaval.Compile(schemas.UsageSchema)
	if err != nil {
		t.Fatal(err)
	}
	send := func(team bool, u *state.Usage) (bodies []map[string]any) {
		t.Helper()
		stPath, _ := usageTestHome(t, u, "")
		st := loadState(t, stPath)
		st.UsageTeam = team
		if err := state.SaveState(stPath, st); err != nil {
			t.Fatal(err)
		}
		sendUsageFn = func(_ context.Context, _ spacenet.UsageConsent, body []byte) (int, error) {
			if res, err := sv.Validate(body); err != nil || !res.Valid() {
				t.Errorf("the package does not validate against usage.v1: %s %v %v", body, err, res.Errors())
			}
			var m map[string]any
			if err := json.Unmarshal(body, &m); err != nil {
				t.Fatal(err)
			}
			bodies = append(bodies, m)
			return 204, nil
		}
		afterReportUsage(t.TempDir(), "en", false)
		return bodies
	}
	on := &state.Usage{Enabled: true, DecidedAt: "2026-11-18T12:00:00Z", TextVersion: usage.TextVersion, InstallID: "3b2c1d4e-5f60-4a7b-8c9d-0e1f2a3b4c5d"}
	team := send(true, on)
	real := send(false, on)
	if len(team) != 1 || len(real) != 1 {
		t.Fatalf("sends: team %d, real %d, want 1 each", len(team), len(real))
	}
	if team[0]["channel"] != usage.ChannelInternal || real[0]["channel"] != usage.ChannelOther {
		t.Errorf("channel: team %v, real %v", team[0]["channel"], real[0]["channel"])
	}
	delete(team[0], "channel")
	delete(real[0], "channel")
	a, _ := json.Marshal(team[0])
	b, _ := json.Marshal(real[0])
	if string(a) != string(b) {
		t.Errorf("a team package differs in more than its channel:\nteam %s\nreal %s", a, b)
	}
	off := &state.Usage{Enabled: false, DecidedAt: "2026-11-18T12:00:00Z", TextVersion: usage.TextVersion, InstallID: on.InstallID}
	if n := len(send(true, off)); n != 0 {
		t.Errorf("team with usage statistics off: %d sends", n)
	}
	if n := len(send(true, nil)); n != 0 {
		t.Errorf("team with no answer: %d sends", n)
	}
}

// The general help does not mention the team mark.
func TestUsageTeamNotInHelp(t *testing.T) {
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		if h := cat.Text("usage.help", nil); strings.Contains(h, "team") || strings.Contains(h, "equipo") {
			t.Errorf("%s: usage.help mentions the team: %q", lang, h)
		}
	}
}
