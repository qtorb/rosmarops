package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/agenthooks"
	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/cli"
)

// TestSessionCheckLatestResult (BV-254, CTO-68 §6 A): with a last review,
// session-check gives Claude statusline's line in
// hookSpecificOutput.additionalContext, on the first opening (with a message)
// and on a later one with nothing to tell (no systemMessage); without a last
// review it adds nothing. The output is one JSON object of the fields Claude
// Code reads for SessionStart.
func TestSessionCheckLatestResult(t *testing.T) {
	type hookOut struct {
		SystemMessage      *string `json:"systemMessage"`
		HookSpecificOutput *struct {
			HookEventName     string `json:"hookEventName"`
			AdditionalContext string `json:"additionalContext"`
		} `json:"hookSpecificOutput"`
	}
	decode := func(t *testing.T, raw string) hookOut {
		t.Helper()
		var o hookOut
		dec := json.NewDecoder(strings.NewReader(raw))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&o); err != nil {
			t.Fatalf("not one JSON object of known fields: %q (%v)", raw, err)
		}
		if strings.Count(strings.TrimSpace(raw), "\n") != 0 {
			t.Fatalf("more than one line: %q", raw)
		}
		return o
	}
	en, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	checked := time.Now().Add(-time.Hour)

	t.Run("with a last review", func(t *testing.T) {
		home, proj, out := bv226Home(t, selfRef(t))
		data := filepath.Join(home, ".rosmarops", "reports", "data")
		if err := agenthooks.WriteStatusline(data, checked, map[string]string{"en": agenthooks.StatuslineText(en, 1, 2, 64)}); err != nil {
			t.Fatal(err)
		}
		want := latestLine(data, en)
		if !strings.HasPrefix(want, "RosmarOps · 1 red · 2 yellow · 64/100") {
			t.Fatalf("statusline line %q", want)
		}
		for i, wantMsg := range []bool{true, false} { // first opening, then nothing new
			out.Reset()
			sessionInput = strings.NewReader(hookInput(proj))
			if err := runSessionCheck(cli.Invocation{}); err != nil {
				t.Fatal(err)
			}
			o := decode(t, out.String())
			if (o.SystemMessage != nil) != wantMsg {
				t.Errorf("run %d: systemMessage %v, want present=%v", i, o.SystemMessage, wantMsg)
			}
			if o.HookSpecificOutput == nil || o.HookSpecificOutput.HookEventName != "SessionStart" ||
				o.HookSpecificOutput.AdditionalContext != agenthooks.LatestResultPrefix+want {
				t.Errorf("run %d: hookSpecificOutput %+v, want %q", i, o.HookSpecificOutput, agenthooks.LatestResultPrefix+want)
			}
		}
	})

	t.Run("without a last review", func(t *testing.T) {
		_, proj, out := bv226Home(t, selfRef(t))
		sessionInput = strings.NewReader(hookInput(proj))
		if err := runSessionCheck(cli.Invocation{}); err != nil {
			t.Fatal(err)
		}
		if o := decode(t, out.String()); o.HookSpecificOutput != nil || o.SystemMessage == nil {
			t.Errorf("output %q: want the message only", out.String())
		}
		out.Reset()
		sessionInput = strings.NewReader(hookInput(proj))
		_ = runSessionCheck(cli.Invocation{})
		if out.Len() != 0 {
			t.Errorf("nothing to tell and no last review, said %q", out.String())
		}
	})
}
