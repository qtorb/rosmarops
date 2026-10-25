package report

import (
	"math"
	"strconv"
	"testing"

	"github.com/qtorb/rosmarops/internal/rules"
)

// luminance is the WCAG 2 relative luminance of a #RRGGBB colour.
func luminance(t *testing.T, hex string) float64 {
	t.Helper()
	if len(hex) != 7 || hex[0] != '#' {
		t.Fatalf("colour %q", hex)
	}
	var c [3]float64
	for i := range c {
		n, err := strconv.ParseUint(hex[1+2*i:3+2*i], 16, 8)
		if err != nil {
			t.Fatalf("colour %q: %v", hex, err)
		}
		v := float64(n) / 255
		if v <= 0.03928 {
			c[i] = v / 12.92
		} else {
			c[i] = math.Pow((v+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
}

// contrast is the WCAG 2 contrast ratio of two colours.
func contrast(t *testing.T, a, b string) float64 {
	la, lb := luminance(t, a), luminance(t, b)
	return (math.Max(la, lb) + 0.05) / (math.Min(la, lb) + 0.05)
}

// informe HTML v2 §2, the contrast test in Go (no axe-core: it would be a
// new dependency): in each mode, every text token of §1 at least 7:1 on
// --card and on --bg, every fill token at least 3:1, and the today tag's
// white text at least 7:1 on its two backgrounds.
func TestHTMLContrast(t *testing.T) {
	tok := map[string]cssToken{}
	for _, c := range htmlTokens {
		tok[c.Name] = c
	}
	for _, mode := range []string{"light", "dark"} {
		pick := func(name string) string {
			c, ok := tok[name]
			if !ok {
				t.Fatalf("no token --%s", name)
			}
			if mode == "dark" {
				return c.Dark
			}
			return c.Light
		}
		for _, bg := range []string{"card", "bg"} {
			for _, fg := range textTokens {
				if r := contrast(t, pick(fg), pick(bg)); r < 7 {
					t.Errorf("%s: text --%s on --%s is %.2f:1, want 7:1", mode, fg, bg, r)
				}
			}
			for _, fg := range fillTokens {
				if r := contrast(t, pick(fg), pick(bg)); r < 3 {
					t.Errorf("%s: fill --%s on --%s is %.2f:1, want 3:1", mode, fg, bg, r)
				}
			}
		}
	}
	for _, sev := range []string{rules.SeverityRed, rules.SeverityYellow} {
		if r := contrast(t, "#FFFFFF", todayPill[sev]); r < 7 {
			t.Errorf("today tag %s: #FFFFFF on %s is %.2f:1, want 7:1", sev, todayPill[sev], r)
		}
	}
}
