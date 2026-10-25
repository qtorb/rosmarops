package report_test

import (
	"strings"
	"testing"
	"time"

	"github.com/qtorb/rosmarops/internal/catalog"
	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/knowledge"
	"github.com/qtorb/rosmarops/internal/memory"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/report"
	"github.com/qtorb/rosmarops/internal/scan/secrets"
)

// memoryRun builds a report from g over the memory prev, as the run command
// does; version replaces the knowledge version when not empty.
func memoryRun(t *testing.T, g *graph.Graph, prev memory.File, at time.Time, version string) report.V2 {
	t.Helper()
	base, err := knowledge.ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	if version != "" {
		base.Version = version
	}
	v, _ := report.BuildV2(report.V2Input{
		Graph: g, GraphSHA256: strings.Repeat("ab", 32), Base: base, RepoRoots: []string{"/src/a", "/src/b"},
		Families: [][]string{{"/src/a"}, {"/src/b"}}, V1: report.Build("t", nil, nil, 0, 0, 0, 0, nil),
		GeneratedAt: at, Salt: []byte("memory-salt"), Memory: prev,
	})
	return v
}

// memorySection is the Memory section of md, empty when it is not there.
func memorySection(t *testing.T, md string, cat catalog.Catalog) string {
	t.Helper()
	head := "## " + cat.Text("section.memory", nil) + "\n\n"
	i := strings.Index(md, head)
	if i < 0 {
		return ""
	}
	j := strings.Index(md, "## "+cat.Text("section.coverage", nil)+"\n")
	if j < i {
		t.Fatalf("memory section not before coverage")
	}
	return md[i+len(head) : j]
}

// BV-43: the Memory section of the technical Markdown, in en and es: counts,
// one line per moved pair, one per finding no longer observed, just before
// Coverage; not there without an earlier memory; the reason when the runs
// are not comparable.
func TestRenderV2Markdown_Memory(t *testing.T) {
	sbSecret := "sb_secret_" + strings.Repeat("S", 22) + "_" + strings.Repeat("T", 8)
	first := v2GraphWith(t, []platform.CredentialEntry{
		{Path: "/src/a/dashboard/.env.local", Line: 4, Key: "SUPABASE_SERVICE_ROLE_KEY", Value: secrets.NewSecretValue(serviceRoleJWT)},
		{Path: "/src/b/.env", Line: 1, Key: "SUPABASE_SECRET_KEY", Value: secrets.NewSecretValue(sbSecret)},
	}, nil)
	// The service_role key moved to another file; the secret key is gone.
	second := v2GraphWith(t, []platform.CredentialEntry{
		{Path: "/src/a/web/.env.local", Line: 4, Key: "SUPABASE_SERVICE_ROLE_KEY", Value: secrets.NewSecretValue(serviceRoleJWT)},
	}, nil)
	run1 := memoryRun(t, &first, memory.File{}, v2Now, "")
	run2 := memoryRun(t, &second, run1.NextMemory(), v2Now.Add(24*time.Hour), "")
	if len(run2.Memory.Moved) != 1 || len(run2.Memory.NoLongerObserved) == 0 {
		t.Fatalf("memory %+v", run2.Memory)
	}
	gone := run2.Memory.NoLongerObserved[0]

	for _, c := range []struct {
		lang         string
		counts, line string
		moved        string
	}{
		{
			"en", "Since the last run — New: ",
			"A red finding (" + gone.Rule + "), open since " + gone.OpenSince + ", is no longer observed. Details: " + gone.LastReport + ".md. No longer observed does not mean fixed.",
			"A " + run2.Memory.Moved[0].Rule + " finding looks moved or merged: it disappeared from one place and appeared in another with the same credential.",
		},
		{
			"es", "Desde la ejecución anterior — Nuevos: ",
			"Un aviso urgente (" + gone.Rule + "), abierto desde " + gone.OpenSince + ", ya no se observa. Detalle: " + gone.LastReport + ".md. Que ya no se observe no quiere decir que esté resuelto.",
			"Un aviso (" + run2.Memory.Moved[0].Rule + ") parece movido o unido: desapareció de un sitio y apareció en otro con la misma clave.",
		},
	} {
		cat, err := catalog.Load(c.lang)
		if err != nil {
			t.Fatal(err)
		}
		if gone.Severity != "red" {
			t.Fatalf("severity %s", gone.Severity)
		}
		if s := memorySection(t, report.RenderV2Markdown(run1, cat, time.UTC), cat); s != "" {
			t.Errorf("%s: memory section without an earlier memory: %q", c.lang, s)
		}
		s := memorySection(t, report.RenderV2Markdown(run2, cat, time.UTC), cat)
		lines := strings.Split(strings.TrimSpace(s), "\n\n")
		if len(lines) != 1+len(run2.Memory.Moved)+len(run2.Memory.NoLongerObserved) {
			t.Fatalf("%s: memory section %q", c.lang, s)
		}
		if !strings.HasPrefix(lines[0], c.counts) || lines[1] != c.moved || lines[2] != c.line {
			t.Errorf("%s: memory section %q", c.lang, s)
		}
		if strings.Contains(s, "[") || strings.Contains(s, "{") {
			t.Errorf("%s: unresolved text in %q", c.lang, s)
		}

		// Other detection rules: the reason after the counts.
		run3 := memoryRun(t, &second, run2.NextMemory(), v2Now.Add(48*time.Hour), "other")
		s = memorySection(t, report.RenderV2Markdown(run3, cat, time.UTC), cat)
		want := map[string]string{
			"en": "Not comparable with the last run: the detection rules changed.",
			"es": "No comparable con la ejecución anterior: han cambiado las reglas de detección.",
		}[c.lang]
		if lines := strings.Split(strings.TrimSpace(s), "\n\n"); len(lines) < 2 || lines[1] != want {
			t.Errorf("%s: not comparable %q", c.lang, s)
		}
	}
}
