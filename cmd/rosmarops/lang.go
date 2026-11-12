package main

// The report's language without --lang (decision 2026-10-27, primer uso con
// un solo .exe, U7): the Windows display language when the catalog has a
// translation for it, English otherwise; --lang always wins. Catalan, Galician
// and Basque have no translation of their own and fall back to Spanish
// (BV-221, signed by Albert 12-11 16:38).

// Primary languages of Windows LANGIDs (the low 10 bits) that select Spanish.
const (
	primaryLangSpanish  = 0x0A
	primaryLangCatalan  = 0x03 // ca-ES, ca-ES-valencia
	primaryLangBasque   = 0x2D // eu-ES
	primaryLangGalician = 0x56 // gl-ES
)

// langOf is the catalog language of a Windows LANGID: its primary language
// (the low 10 bits) Spanish, Catalan, Galician or Basque is es; any other, en.
func langOf(id uint16) string {
	switch id & 0x3FF {
	case primaryLangSpanish, primaryLangCatalan, primaryLangGalician, primaryLangBasque:
		return "es"
	}
	return "en"
}

// systemLang is the language without --lang; tests replace it.
var systemLang = func() string { return langOf(uiLanguage()) }
