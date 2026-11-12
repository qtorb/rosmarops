package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/cli"
)

// TestBV221LangOf: with no --lang, a Windows display language of Catalan,
// Galician or Basque (no translation of their own) selects Spanish, not
// English; Spanish stays Spanish and any other language without a
// translation is English.
func TestBV221LangOf(t *testing.T) {
	for _, c := range []struct {
		name string
		id   uint16
		want string
	}{
		{"ca-ES", 0x0403, "es"},
		{"ca-ES-valencia", 0x0803, "es"},
		{"gl-ES", 0x0456, "es"},
		{"eu-ES", 0x042D, "es"},
		{"es-ES", 0x0C0A, "es"},
		{"fr-FR", 0x040C, "en"},
		{"en-US", 0x0409, "en"},
	} {
		if got := langOf(c.id); got != c.want {
			t.Errorf("%s (%#04x): %s, want %s", c.name, c.id, got, c.want)
		}
	}
}

// TestBV221LangFlagWins: --lang always wins over the system language, also
// when the system language is Catalan (which would otherwise give Spanish).
func TestBV221LangFlagWins(t *testing.T) {
	prev := systemLang
	t.Cleanup(func() { systemLang = prev })
	systemLang = func() string { return langOf(0x0403) } // ca-ES
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"run", "--lang", "en"}, "en"},
		{[]string{"run"}, "es"},
	} {
		cmd := newRunCmd()
		var got string
		cmd.Before = nil
		cmd.Run = func(inv cli.Invocation) error { got = inv.Flags["lang"]; return nil }
		p := cli.Program{Name: "rosmarops", Lang: systemLang(), Root: &cli.Command{Name: "rosmarops", Subs: []*cli.Command{cmd}}, Stdout: io.Discard, Stderr: io.Discard}
		if err := p.Execute(c.args); err != nil {
			t.Fatalf("%v: %v", c.args, err)
		}
		if got != c.want {
			t.Errorf("%v with a Catalan system: lang %q, want %q", c.args, got, c.want)
		}
	}
}

// TestBV221SlowNoteOnce: three repositories with a long history in one review
// — each stalls the count for more than 15 s — give a single «long history»
// line, at the first one.
func TestBV221SlowNoteOnce(t *testing.T) {
	cat := enCat(t)
	var buf bytes.Buffer
	base := time.Unix(1_000_000, 0)
	clk := base
	p := &progress{out: &buf, terminal: true, cat: cat, now: func() time.Time { return clk }}
	slow := cat.Text("console.progress.repos_slow", nil)

	p.repos(0, 3)
	for i := 0; i < 3; i++ {
		start := clk
		for s := 1; s <= 20; s++ { // a 20 s stall, ticking once a second
			clk = start.Add(time.Duration(s) * time.Second)
			p.tick()
		}
		if i == 0 && strings.Count(buf.String(), slow) != 1 {
			t.Fatalf("the first long-history repository should show the note once:\n%q", buf.String())
		}
		p.repos(i+1, 3)
	}
	if n := strings.Count(buf.String(), slow); n != 1 {
		t.Fatalf("three long-history repositories: %d notes, want 1:\n%q", n, buf.String())
	}
}
