package report_test

import (
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/report"
)

// BV-153: the «Found in» line on the card's first line. It shows the first
// location, then «and N more»; nothing without a location; once per merged
// row with the combined, deduplicated list; a deceptive path is shown as
// ⟨U+XXXX⟩ (untrusted.PlainText).

const whereOpen = `<p class="where">`

// onlyWhere returns the single «Found in» line's inner HTML, failing if there
// is not exactly one on the page.
func onlyWhere(t *testing.T, page string) string {
	t.Helper()
	if n := strings.Count(page, whereOpen); n != 1 {
		t.Fatalf("%d «Found in» lines, want 1", n)
	}
	_, rest, _ := strings.Cut(page, whereOpen)
	inner, _, _ := strings.Cut(rest, `</p>`)
	return inner
}

// clearLocations blanks every finding's locations; craft sets the ones we test.
func clearLocations(v report.V2) report.V2 {
	v = cloneV2(v)
	for i := range v.Findings {
		v.Findings[i].Evidence.Locations = nil
	}
	return v
}

func TestWhereLine_Card(t *testing.T) {
	g := v2Graph(t)
	base := buildV2(t, &g, report.Build("t", nil, nil, 0, 0, 0, 0, nil))

	// Pick a finding that is not today's (today's card is not a step-3 row).
	var target string
	for _, f := range base.Findings {
		if f.ID != base.Today.FindingID {
			target = f.ID
			break
		}
	}
	if target == "" {
		t.Fatal("no non-today finding to craft")
	}
	set := func(locs []string) report.V2 {
		v := clearLocations(base)
		for i := range v.Findings {
			if v.Findings[i].ID == target {
				v.Findings[i].Evidence.Locations = locs
			}
		}
		return v
	}

	t.Run("first location and «and N more»", func(t *testing.T) {
		v := set([]string{"/src/a/.env.local", "/src/b/.env", "/src/c/.env"})
		for _, lang := range []string{"en", "es"} {
			cat, _ := catalog.Load(lang)
			inner := onlyWhere(t, renderHTML(t, lang, v))
			wantLabel := htmlEscape(cat.Text("html.where", nil))
			wantMore := htmlEscape(cat.Text("html.tech.more", map[string]string{"count": "2"}))
			if !strings.HasPrefix(inner, wantLabel+` <code>/src/a/.env.local</code>`) {
				t.Errorf("%s: line %q does not start with label + first location", lang, inner)
			}
			if !strings.Contains(inner, wantMore) {
				t.Errorf("%s: line %q lacks %q", lang, inner, wantMore)
			}
		}
	})

	t.Run("single location, no «more»", func(t *testing.T) {
		v := set([]string{"/src/a/only.env"})
		cat, _ := catalog.Load("en")
		inner := onlyWhere(t, renderHTML(t, "en", v))
		if inner != htmlEscape(cat.Text("html.where", nil))+` <code>/src/a/only.env</code>` {
			t.Errorf("single-location line is %q", inner)
		}
		if strings.Contains(inner, htmlEscape(cat.Text("html.tech.more", map[string]string{"count": "0"}))) {
			t.Errorf("single-location line shows «and N more»: %q", inner)
		}
	})

	t.Run("no line without locations", func(t *testing.T) {
		v := clearLocations(base)
		for _, lang := range []string{"en", "es"} {
			if n := strings.Count(renderHTML(t, lang, v), whereOpen); n != 0 {
				t.Errorf("%s: %d «Found in» lines with no locations, want 0", lang, n)
			}
		}
	})

	t.Run("deceptive path shown as ⟨U+XXXX⟩", func(t *testing.T) {
		v := set([]string{"/src/a/\u202eevil.env"})
		page := renderHTML(t, "en", v)
		inner := onlyWhere(t, page)
		if !strings.Contains(inner, `<code>/src/a/⟨U+202E⟩evil.env</code>`) {
			t.Errorf("deceptive path not neutralized: %q", inner)
		}
		if strings.Contains(page, "\u202e") {
			t.Error("the raw U+202E character is in the page")
		}
	})
}

// A merged row shows one «Found in» line built from the members' combined,
// deduplicated locations (BV-153 point 2).
func TestWhereLine_MergedRow(t *testing.T) {
	v := clearLocations(noiseReport(t))
	// The service_role credential is one merged row across two agents.
	n := 0
	for i := range v.Findings {
		if v.Findings[i].Params.Scope == "service_role" {
			// Overlapping lists with a shared duplicate to prove dedup.
			if n == 0 {
				v.Findings[i].Evidence.Locations = []string{"/dup/.env:1", "/aaa/.env:2"}
			} else {
				v.Findings[i].Evidence.Locations = []string{"/dup/.env:1", "/zzz/.env:3"}
			}
			n++
		}
	}
	if n != 2 {
		t.Fatalf("expected 2 service_role members, got %d", n)
	}
	for _, lang := range []string{"en", "es"} {
		cat, _ := catalog.Load(lang)
		page := renderHTML(t, lang, v)
		inner := onlyWhere(t, page) // exactly one line for the whole page
		// Combined uniqueSorted = /aaa/.env:2, /dup/.env:1, /zzz/.env:3.
		if !strings.HasPrefix(inner, htmlEscape(cat.Text("html.where", nil))+` <code>/aaa/.env:2</code>`) {
			t.Errorf("%s: merged line %q does not start with the first combined location", lang, inner)
		}
		if !strings.Contains(inner, htmlEscape(cat.Text("html.tech.more", map[string]string{"count": "2"}))) {
			t.Errorf("%s: merged line %q lacks «and 2 more» (dedup of the shared path)", lang, inner)
		}
		if strings.Count(inner, "/dup/.env:1") != 0 && strings.Count(inner, "<code>") != 1 {
			t.Errorf("%s: merged line is not a single deduplicated code span: %q", lang, inner)
		}
	}
}

// The html.where key exists in both catalogs with the signed literal.
func TestWhereLine_Keys(t *testing.T) {
	want := map[string]string{"en": "Found in:", "es": "Encontrado en:"}
	for lang, lit := range want {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		if got := cat.Text("html.where", nil); got != lit {
			t.Errorf("%s html.where = %q, want %q", lang, got, lit)
		}
	}
}
