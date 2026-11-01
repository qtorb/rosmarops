package report_test

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/report"
)

// BV-142: the report header (HTML) and the scope line (Markdown) tell the
// truth about this run. They say «sent only your encrypted results» only when
// this review's package (and any pending one) was actually sent; with no
// permission, or a pending or rejected send, they keep the «sent nothing»
// line. The text follows the real send result, never the mere permission.
func TestReportHeaderAndScopeFollowSendResult(t *testing.T) {
	const (
		nothingHeader = "sent nothing off this computer"
		sentHeader    = "sent only your encrypted results, to your space"
		nothingScope  = "sent nothing off this machine"
		sentScope     = "sent only your encrypted results, to your space"
	)
	cases := []struct {
		name      string
		connected bool
		sentDate  string
		rejected  bool
		wantSent  bool // expect the «sent only…» variant
	}{
		{"no permission", false, "", false, false},
		{"sent", true, "2026-10-25", false, true},
		{"pending", true, "", false, false},
		{"rejected", true, "", true, false},
	}

	g := v2Graph(t)
	base := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	cat, err := catalog.Load("en")
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := base
			v.SpaceConnected = tc.connected
			v.SpaceSentDate = tc.sentDate
			v.SpaceRejected = tc.rejected

			page := renderHTML(t, "en", v)
			header := page[strings.Index(page, `<div class="hdr">`):]
			header = header[:strings.Index(header, "</div>")]
			md := report.RenderV2Markdown(v, cat, time.UTC)

			if tc.wantSent {
				if !strings.Contains(header, sentHeader) || strings.Contains(header, nothingHeader) {
					t.Errorf("header: want sent variant, got %q", header)
				}
				if !strings.Contains(md, sentScope) || strings.Contains(md, nothingScope) {
					t.Errorf("scope: want sent variant")
				}
			} else {
				if !strings.Contains(header, nothingHeader) || strings.Contains(header, sentHeader) {
					t.Errorf("header: want «sent nothing» variant, got %q", header)
				}
				if !strings.Contains(md, nothingScope) || strings.Contains(md, sentScope) {
					t.Errorf("scope: want «sent nothing» variant")
				}
			}
		})
	}
}

// BV-142: the Spanish texts are wired too (the sent variant), header and scope.
func TestReportHeaderAndScopeSentSpanish(t *testing.T) {
	g := v2Graph(t)
	v := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))
	v.SpaceConnected = true
	v.SpaceSentDate = "2026-10-25"
	cat, err := catalog.Load("es")
	if err != nil {
		t.Fatal(err)
	}
	const sent = "solo ha enviado tus resultados cifrados, a tu espacio"
	page := renderHTML(t, "es", v)
	if !strings.Contains(page, sent) {
		t.Errorf("es header lacks the sent promise")
	}
	if md := report.RenderV2Markdown(v, cat, time.UTC); !strings.Contains(md, sent) {
		t.Errorf("es scope lacks the sent promise")
	}
}
