package trust_test

// Trust test (informe HTML v1, §9): the visual report is self-contained:
// no URL, no external resource, no network or storage API in its script.

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/index"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
)

func TestNoExternalInHTML(t *testing.T) {
	day := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	home, repo := "/home/alice", "/home/alice/projects/app"
	v := scenarioV2(t, []byte("0123456789abcdef0123456789abcdef"), home, repo,
		[]platform.CredentialEntry{credEntry(repo+"/.env", 1, "GITHUB_TOKEN", fakeGitHubPAT)}, day)
	for _, lang := range []string{"en", "es"} {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		page := report.RenderHTML(v, cat, home, time.UTC)
		if !strings.Contains(page, `id="card-button"`) || len(v.Findings) == 0 {
			t.Fatalf("%s: no card or no finding — test would be vacuous", lang)
		}
		// The one exception (BV-41): the card's namespace attribute, at most
		// once, on the root <svg> of the card only; removed before the
		// search.
		root := `<template id="card-svg"><svg ` + index.SVGNamespaceAttr + " "
		if n := strings.Count(page, index.SVGNamespaceAttr); n > 1 || n == 1 && !strings.Contains(page, root) {
			t.Errorf("%s: the namespace attribute is not only on the card's root <svg> (%d)", lang, n)
		}
		page = strings.Replace(page, root, "", 1)
		for _, banned := range []string{"http://", "https://", "src=", "<link", "fetch(", "XMLHttpRequest", "import(", "localStorage"} {
			if strings.Contains(page, banned) {
				t.Errorf("%s: the HTML contains %q", lang, banned)
			}
		}
	}
}
