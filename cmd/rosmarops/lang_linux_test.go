//go:build linux

package main

import "testing"

func TestUILanguageLinux(t *testing.T) {
	cases := map[string]string{
		"es_ES.UTF-8":    "es",
		"es":             "es",
		"es_419":         "es",
		"ca_ES.UTF-8":    "es", // BV-221: Catalan, Galician and Basque → Spanish
		"ca_ES@valencia": "es",
		"gl_ES.UTF-8":    "es",
		"eu_ES.UTF-8":    "es",
		"fr_FR.UTF-8":    "en",
		"en_US.UTF-8":    "en",
		"C":              "en",
		"POSIX":          "en",
		"":               "en",
	}
	for _, v := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		t.Setenv(v, "")
	}
	for val, want := range cases {
		t.Setenv("LC_ALL", val)
		if got := langOf(uiLanguage()); got != want {
			t.Errorf("LC_ALL=%q -> %q, want %q", val, got, want)
		}
	}
	// Order: LC_ALL wins over LC_MESSAGES over LANG.
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "es_ES.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")
	if got := langOf(uiLanguage()); got != "es" {
		t.Errorf("LC_MESSAGES should win: got %q", got)
	}
	if !displayLanguageChecked {
		t.Error("displayLanguageChecked should be true on linux")
	}
}
