package main

// The report's language without --lang (decision 2026-10-27, primer uso con
// un solo .exe, U7): the Windows display language when the catalog has a
// translation for it, English otherwise; --lang always wins.

// primaryLangSpanish is the primary language of every Spanish LANGID.
const primaryLangSpanish = 0x0A

// langOf is the catalog language of a Windows LANGID: its primary language
// (the low 10 bits) 0x0A is es; any other, en.
func langOf(id uint16) string {
	if id&0x3FF == primaryLangSpanish {
		return "es"
	}
	return "en"
}

// systemLang is the language without --lang; tests replace it.
var systemLang = func() string { return langOf(uiLanguage()) }
