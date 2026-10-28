package catalog

import "testing"

func TestLoadEnglish(t *testing.T) {
	c, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	keys, err := Keys()
	if err != nil || len(keys) == 0 {
		t.Fatalf("keys %d, %v", len(keys), err)
	}
	if c.lang != DefaultLang {
		t.Errorf("lang %q", c.lang)
	}
}

// No language is guessed: one with no file is an error.
func TestLoadUnknownLanguage(t *testing.T) {
	if _, err := Load("xx"); err == nil {
		t.Error("unknown language loaded")
	}
}

// An empty or missing text renders as the key in brackets; a language falls
// back to English per key; placeholders take params.
func TestText(t *testing.T) {
	c := Catalog{
		lang: "es",
		msgs: map[string]string{"a": "", "b": "B {n}"},
		en:   map[string]string{"a": "A {n} {n}", "b": "en b", "c": ""},
	}
	p := map[string]string{"n": "3"}
	for key, want := range map[string]string{"a": "A 3 3", "b": "B 3", "c": "[c]", "d": "[d]"} {
		if got := c.Text(key, p); got != want {
			t.Errorf("%s: %q, want %q", key, got, want)
		}
	}
	c.en["x.one"], c.en["x.other"] = "one", "{n} many"
	if c.Plural("x", 1, p) != "one" || c.Plural("x", 0, p) != "3 many" {
		t.Errorf("plural: %q %q", c.Plural("x", 1, p), c.Plural("x", 0, p))
	}
	empty := Catalog{msgs: map[string]string{}, en: map[string]string{"finding.cred_read_and_send.title": ""}}
	if got := empty.Text("finding.cred_read_and_send.title", nil); got != "[finding.cred_read_and_send.title]" {
		t.Errorf("empty: %q", got)
	}
}

// B7 (decision 2026-10-27, blindaje): placeholders are filled in one pass,
// so a value read from disk that contains a placeholder is never expanded,
// whatever the order of params; an unknown placeholder stays; a lone brace
// stays.
func TestTextNeverExpandsAValue(t *testing.T) {
	c := Catalog{lang: "en", msgs: map[string]string{
		"k": "{name} ({agent}) in {repos} {missing} {",
	}}
	c.en = c.msgs
	p := map[string]string{"name": "evil{agent}{repos}", "agent": "Claude Code", "repos": "3"}
	want := "evil{agent}{repos} (Claude Code) in 3 {missing} {"
	for i := 0; i < 50; i++ { // map order varies between iterations
		if got := c.Text("k", p); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}
