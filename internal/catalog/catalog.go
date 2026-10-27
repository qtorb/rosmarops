// Package catalog is the message catalog (decision 2026-10-23, idioma): every
// text shown to people comes from here by a stable code. One embedded flat
// JSON file per language, messages/<lang>.json, code → text; English is the
// source and the fallback for any missing or empty key. Texts are copy: the
// advisor writes them and the owner signs them; code never does.
//
// Placeholders are {name}, replaced from the params of the text; plurals are
// separate keys (.one, .other).
package catalog

import (
	"embed"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// DefaultLang is the source language and the fallback.
const DefaultLang = "en"

//go:embed messages/*.json
var files embed.FS

// Catalog resolves codes to texts for one language.
type Catalog struct {
	lang string
	msgs map[string]string
	en   map[string]string
}

func load(lang string) (map[string]string, error) {
	raw, err := files.ReadFile("messages/" + lang + ".json")
	if err != nil {
		return nil, fmt.Errorf("catalog %q: %w", lang, err)
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("catalog %q: %w", lang, err)
	}
	return m, nil
}

// Load returns the catalog of lang ("" is English). A language with no file
// is an error: the language is chosen explicitly, never guessed.
func Load(lang string) (Catalog, error) {
	if lang == "" {
		lang = DefaultLang
	}
	en, err := load(DefaultLang)
	if err != nil {
		return Catalog{}, err
	}
	msgs := en
	if lang != DefaultLang {
		if msgs, err = load(lang); err != nil {
			return Catalog{}, err
		}
	}
	return Catalog{lang: lang, msgs: msgs, en: en}, nil
}

// Langs returns the languages with a catalog file, sorted: the report is
// written in each of them (decision 2026-10-27, primer uso con un solo
// .exe, U7).
func Langs() []string {
	entries, _ := files.ReadDir("messages")
	langs := make([]string, 0, len(entries))
	for _, e := range entries {
		if name := e.Name(); strings.HasSuffix(name, ".json") {
			langs = append(langs, strings.TrimSuffix(name, ".json"))
		}
	}
	sort.Strings(langs)
	return langs
}

// Keys returns the codes of the English catalog, sorted.
func Keys() ([]string, error) {
	en, err := load(DefaultLang)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(en))
	for k := range en {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

// Text returns the text of key in the catalog's language, falling back to
// English, with {name} placeholders replaced from params. A key with no text
// (empty, or missing everywhere) renders as "[key]".
func (c Catalog) Text(key string, params map[string]string) string {
	t := c.msgs[key]
	if t == "" {
		t = c.en[key]
	}
	if t == "" {
		return "[" + key + "]"
	}
	for name, v := range params {
		t = strings.ReplaceAll(t, "{"+name+"}", v)
	}
	return t
}

// Plural returns key.one for n == 1 and key.other otherwise.
func (c Catalog) Plural(key string, n int, params map[string]string) string {
	if n == 1 {
		return c.Text(key+".one", params)
	}
	return c.Text(key+".other", params)
}

// Lang is the catalog's language.
func (c Catalog) Lang() string { return c.lang }

// Has reports whether key has a text in the catalog's language or in
// English.
func (c Catalog) Has(key string) bool { return c.msgs[key] != "" || c.en[key] != "" }

// HasOwn reports whether key has a text in the catalog's own language (no
// fallback to English).
func (c Catalog) HasOwn(key string) bool { return c.msgs[key] != "" }

// months are the abbreviated month names of the day label (informe HTML v1,
// §3.1): uppercase, in English and in Spanish.
var months = map[string][12]string{
	"en": {"JAN", "FEB", "MAR", "APR", "MAY", "JUN", "JUL", "AUG", "SEP", "OCT", "NOV", "DEC"},
	"es": {"ENE", "FEB", "MAR", "ABR", "MAY", "JUN", "JUL", "AGO", "SEP", "OCT", "NOV", "DIC"},
}

// DayMonthLabel is the day of t as "24 OCT", without the year (informe HTML
// v2, §5.3), with the month as in DayLabel.
func (c Catalog) DayMonthLabel(t time.Time) string {
	m, ok := months[c.lang]
	if !ok {
		m = months[DefaultLang]
	}
	return fmt.Sprintf("%d %s", t.Day(), m[t.Month()-1])
}

// DayLabel is the day of t as "24 OCT 2026", with the month in the
// catalog's language (English for a language with no month names).
func (c Catalog) DayLabel(t time.Time) string {
	m, ok := months[c.lang]
	if !ok {
		m = months[DefaultLang]
	}
	return fmt.Sprintf("%d %s %d", t.Day(), m[t.Month()-1], t.Year())
}
