package main

import "testing"

// U7: the Windows display language's primary language 0x0A is es; any
// other is en.
func TestLangOf(t *testing.T) {
	for id, want := range map[uint16]string{
		0x0C0A: "es", 0x040A: "es", 0x080A: "es", 0x2C0A: "es",
		0x0409: "en", 0x0809: "en", 0x0407: "en", 0x040C: "en", 0x0416: "en", 0: "en",
	} {
		if got := langOf(id); got != want {
			t.Errorf("%#04x: %s, want %s", id, got, want)
		}
	}
}

// U7: without --lang, run follows the system language; --lang wins (the
// flag's value replaces the default).
func TestRunLangDefault(t *testing.T) {
	prev := systemLang
	t.Cleanup(func() { systemLang = prev })
	for _, lang := range []string{"es", "en"} {
		systemLang = func() string { return lang }
		cmd := newRunCmd()
		if cmd.Flags[0].Name != "lang" || cmd.Flags[0].Default != lang {
			t.Errorf("%s: flag %+v", lang, cmd.Flags[0])
		}
	}
}
