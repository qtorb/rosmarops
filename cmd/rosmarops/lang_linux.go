//go:build linux

package main

import (
	"os"
	"strings"
)

// uiLanguage reads the POSIX locale from LC_ALL, LC_MESSAGES and LANG, in that
// order (BV-165). It returns a Windows-style primary LANGID so langOf maps it
// like the Windows path: Spanish (es, es_ES, es_419, es.UTF-8…), Catalan (ca),
// Galician (gl) and Basque (eu) by their primary language, so langOf selects
// Spanish for them (BV-221); anything else (including C/POSIX and an unset
// environment) is 0, English.
func uiLanguage() uint16 {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v, ok := os.LookupEnv(name); ok && v != "" {
			switch localeLanguage(v) {
			case "es":
				return primaryLangSpanish
			case "ca":
				return primaryLangCatalan
			case "gl":
				return primaryLangGalician
			case "eu":
				return primaryLangBasque
			}
			return 0
		}
	}
	return 0
}

// localeLanguage is the language part of a POSIX locale, lowercased: the text
// before the first '_', '.' or '@' ("es_ES.UTF-8" → "es").
func localeLanguage(locale string) string {
	locale = strings.ToLower(locale)
	if i := strings.IndexAny(locale, "_.@"); i >= 0 {
		return locale[:i]
	}
	return locale
}

// displayLanguageChecked: the system display language is read on Linux (§4).
const displayLanguageChecked = true
