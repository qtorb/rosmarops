package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// BV-151: the taskbar OSC 9;4 sequences, one per state.
func TestTaskbarSequences(t *testing.T) {
	var buf bytes.Buffer
	b := &taskbar{w: &buf, enabled: true, lastState: tbNormal}
	step := func(fn func(), want string) {
		t.Helper()
		buf.Reset()
		fn()
		if buf.String() != want {
			t.Errorf("got %q, want %q", buf.String(), want)
		}
	}
	step(b.agents, "\x1b]9;4;3;0\a")
	step(func() { b.reposPct(50) }, "\x1b]9;4;1;50\a")
	step(b.pause, "\x1b]9;4;4;50\a")  // paused at the last value
	step(b.resume, "\x1b]9;4;1;50\a") // back to the state before the pause
	step(b.report, "\x1b]9;4;1;100\a")
	step(b.failed, "\x1b]9;4;2;100\a")
	step(b.clear, "\x1b]9;4;0;0\a")
}

// A disabled taskbar (no Windows Terminal / not active) emits nothing.
func TestTaskbarDisabledSilent(t *testing.T) {
	var buf bytes.Buffer
	b := &taskbar{w: &buf, enabled: false, lastState: tbNormal}
	b.agents()
	b.reposPct(10)
	b.report()
	b.pause()
	b.failed()
	b.clear()
	if buf.Len() != 0 {
		t.Errorf("disabled taskbar emitted %q", buf.String())
	}
	// A nil taskbar is also silent and never panics.
	var nilBar *taskbar
	nilBar.agents()
	nilBar.failed()
}

// newConsoleUI wires title and taskbar by the right conditions, and the title
// transitions carry the signed catalog text.
func TestConsoleUIWiring(t *testing.T) {
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer

	// Active + Windows Terminal: title on, taskbar on.
	u := newConsoleUI(cat, true, true, &buf)
	if u.title == nil || u.bar == nil || !u.bar.enabled {
		t.Fatalf("active+WT: title=%v bar=%v", u.title != nil, u.bar)
	}
	// Active but no Windows Terminal: title on, no taskbar.
	if u := newConsoleUI(cat, true, false, &buf); u.title == nil || u.bar != nil {
		t.Errorf("active, no WT: title=%v bar=%v", u.title != nil, u.bar)
	}
	// Inactive (not a console, or --unattended): nothing — no title, no taskbar.
	if u := newConsoleUI(cat, false, true, &buf); u.title != nil || u.bar != nil {
		t.Errorf("inactive emitted branding: title=%v bar=%v", u.title != nil, u.bar)
	}

	// The transitions set the window title with the catalog text and drive the
	// taskbar. Use a recording title to see both.
	var titles []string
	buf.Reset()
	rec := &consoleUI{cat: cat, title: func(s string) { titles = append(titles, s) }, bar: &taskbar{w: &buf, enabled: true, lastState: tbNormal}}
	rec.bar.reposPct(40)
	buf.Reset()
	rec.question()
	rec.running()
	rec.done()
	rec.failed()
	wantTitles := []string{
		cat.Text("console.title.question", nil),
		cat.Text("console.title.running", nil),
		cat.Text("console.title.done", nil),
		cat.Text("console.title.failed", nil),
	}
	if strings.Join(titles, "|") != strings.Join(wantTitles, "|") {
		t.Errorf("titles %v, want %v", titles, wantTitles)
	}
	if got, want := buf.String(), "\x1b]9;4;4;40\a\x1b]9;4;1;40\a\x1b]9;4;0;0\a\x1b]9;4;2;100\a"; got != want {
		t.Errorf("taskbar during transitions %q, want %q", got, want)
	}
}

// An inactive UI (e.g. --unattended) never sets the title and never emits a
// sequence, however it is driven.
func TestConsoleUIInactiveSilent(t *testing.T) {
	cat, _ := catalog.Load("en")
	var buf bytes.Buffer
	u := newConsoleUI(cat, false, false, &buf)
	u.setTitle("console.title.running")
	u.running()
	u.question()
	u.done()
	u.failed()
	if buf.Len() != 0 {
		t.Errorf("inactive UI emitted %q", buf.String())
	}
	// A nil ui is also safe.
	var nilUI *consoleUI
	nilUI.running()
	nilUI.failed()
	if nilUI.taskbar() != nil {
		t.Error("nil ui has a taskbar")
	}
}

// The brand header: coloured when VT is on, plain text when it is not.
func TestBrandHeader(t *testing.T) {
	var color, plain bytes.Buffer
	writeBrandHeader(&color, "0.7.3", true)
	writeBrandHeader(&plain, "0.7.3", false)
	if !strings.Contains(color.String(), "\x1b[1;38;2;111;143;120mRosmarOps\x1b[0m v0.7.3") {
		t.Errorf("coloured header: %q", color.String())
	}
	if strings.Contains(plain.String(), "\x1b[") {
		t.Errorf("plain header has escapes: %q", plain.String())
	}
	if !strings.Contains(plain.String(), "  RosmarOps v0.7.3") {
		t.Errorf("plain header: %q", plain.String())
	}
}

// The four title keys exist in both catalogs with the signed literal.
func TestConsoleTitleKeys(t *testing.T) {
	want := map[string]map[string]string{
		"en": {
			"console.title.running":  "RosmarOps · checking this computer",
			"console.title.question": "RosmarOps · needs your answer",
			"console.title.done":     "RosmarOps · review finished",
			"console.title.failed":   "RosmarOps · could not finish",
		},
		"es": {
			"console.title.running":  "RosmarOps · revisando este equipo",
			"console.title.question": "RosmarOps · necesita tu respuesta",
			"console.title.done":     "RosmarOps · revisión terminada",
			"console.title.failed":   "RosmarOps · no pudo terminar",
		},
	}
	for lang, keys := range want {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		for k, lit := range keys {
			if got := cat.Text(k, nil); got != lit {
				t.Errorf("%s %s = %q, want %q", lang, k, got, lit)
			}
		}
	}
}
