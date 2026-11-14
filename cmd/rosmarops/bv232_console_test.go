package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/scan/projects"
)

// BV-232 §6, §7: the console's two combinations of console.projects.found
// with several projects (one folder, several folders), in EN and ES, and the
// form for one of console.projects.not_searched.
func TestConsoleProjectsFoundForms(t *testing.T) {
	home := t.TempDir()
	items := []projects.Item{
		{Path: filepath.Join(home, "a"), Source: projects.SourceHome},
		{Path: filepath.Join(home, "b"), Source: projects.SourceHome},
	}
	want := map[string][2]string{
		"en": {"RosmarOps found 2 projects in 1 folder:", "RosmarOps found 2 projects in 3 folders:"},
		"es": {"RosmarOps ha encontrado 2 proyectos en 1 carpeta:", "RosmarOps ha encontrado 2 proyectos en 3 carpetas:"},
	}
	notSearched := map[string]string{
		"en": "It did not look in 1 place (a hidden, system or network folder, a link…). The report says which.",
		"es": "No ha buscado en 1 sitio (una carpeta oculta, del sistema o de red, un enlace…). El informe dice cuál.",
	}
	for lang, w := range want {
		cat, err := catalog.Load(lang)
		if err != nil {
			t.Fatal(err)
		}
		for i, roots := range [][]string{{home}, {home, home + "2", home + "3"}} {
			res := projects.Result{
				Status: projects.StatusComplete, Roots: roots, Items: items,
				Skipped: []projects.NotSearched{{Reason: projects.ReasonHidden, Count: 1}},
			}
			var buf bytes.Buffer
			printProjects(&buf, cat, res, 80)
			if !strings.Contains(buf.String(), w[i]) {
				t.Errorf("%s, %d folders: no %q in\n%s", lang, len(roots), w[i], buf.String())
			}
			if !strings.Contains(buf.String(), notSearched[lang]) {
				t.Errorf("%s: no %q in\n%s", lang, notSearched[lang], buf.String())
			}
		}
	}
}
