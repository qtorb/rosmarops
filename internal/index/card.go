package index

import (
	"fmt"
	"html"
	"strconv"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// Card colours (informe HTML v1, §7): the dark tokens, the band in its dark
// colour.
var cardBandColour = map[string]string{
	BandExposed:   "#FF6A4D",
	BandOpenGaps:  "#F0B429",
	BandContained: "#4CC38A",
}

// SVGNamespace is the namespace of the card's root element (BV-41).
const SVGNamespace = "http://www.w3.org/2000/svg"

// SVGNamespaceAttr is the exact attribute the card's root carries, the only
// "http" the card and the report may hold (BV-41).
const SVGNamespaceAttr = `xmlns="` + SVGNamespace + `"`

// Card is the share card of an agent's score (I10 with adjustment 1, I11):
// an inert SVG and one line of text, from the closed list only: the index
// name, the agent's product name, the score and band, the red and yellow
// counts, the silenced count (memoria, M11; only when there are any), the
// day and the method version. Nothing of the machine: two machines with the
// same score, band, counts, silenced count, day and version give the same
// bytes. ok is false when the agent has no score.
//
// The SVG has no script, link, URL, comment or metadata. Its root has the
// SVG namespace, xmlns="http://www.w3.org/2000/svg", the one exception
// signed in BV-41: without it the saved file does not render.
func Card(a AgentIndex, silenced int, day time.Time, cat catalog.Catalog) (svg, text string, ok bool) {
	if a.Value == nil {
		return "", "", false
	}
	esc := html.EscapeString
	value := strconv.Itoa(*a.Value)
	colour := cardBandColour[a.Band]
	name := cat.Text("html.index.name", nil)
	of := cat.Text("html.index.of", nil)
	band := cat.Text("band."+a.Band, nil)
	counts := cat.Text("html.card.counts", map[string]string{"red": strconv.Itoa(a.Red), "yellow": strconv.Itoa(a.Yellow)})
	// The silenced count follows the counts (informe HTML v3 §9.3): 14px,
	// #A3A098.
	sil, silText := "", ""
	if silenced > 0 {
		t := cat.Text("html.card.silenced", map[string]string{"count": strconv.Itoa(silenced)})
		sil = `<tspan font-size="14" fill="#A3A098"> · ` + esc(t) + `</tspan>`
		silText = " · " + t
	}
	date := cat.DayLabel(day)
	method := cat.Text("html.card.method", map[string]string{"version": strconv.Itoa(Version)})
	// "/100" follows the score, 16 apart, at a mean digit width of 0.6 × 220.
	ofX := 72 + int(float64(len(value))*0.6*220) + 16
	font := `font-family="system-ui, sans-serif"`
	svg = fmt.Sprintf(`<svg xmlns="`+SVGNamespace+`" width="1200" height="630" viewBox="0 0 1200 630">`+
		`<rect width="1200" height="630" fill="#131311"/>`+
		`<rect width="1200" height="12" fill="%[1]s"/>`+
		`<text x="72" y="120" %[2]s font-size="32" font-weight="700" fill="#A3A098">%[3]s</text>`+
		`<text x="72" y="180" %[2]s font-size="40" font-weight="600" fill="#EDEBE6">%[4]s</text>`+
		`<text x="72" y="400" %[2]s font-size="220" font-weight="800" fill="%[1]s">%[5]s</text>`+
		`<text x="%[6]d" y="400" %[2]s font-size="64" font-weight="600" fill="#A3A098">%[7]s</text>`+
		`<text x="72" y="480" %[2]s font-size="44" font-weight="700" fill="%[1]s">%[8]s</text>`+
		`<text x="72" y="540" %[2]s font-size="30" font-weight="400" fill="#EDEBE6">%[9]s%[12]s</text>`+
		`<text x="72" y="590" %[2]s font-size="26" fill="#A3A098">%[10]s · %[11]s</text>`+
		`</svg>`,
		colour, font, esc(name), esc(a.Agent), value, ofX, esc(of), esc(band), esc(counts), esc(date), esc(method), sil)
	text = fmt.Sprintf("%s · %s · %s%s (%s) · %s%s · %s · %s", name, a.Agent, value, of, band, counts, silText, date, method)
	return svg, text, true
}
