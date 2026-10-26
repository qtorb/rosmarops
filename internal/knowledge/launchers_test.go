package knowledge

import (
	"testing"
)

func launchers(t *testing.T) []CodeLauncher {
	t.Helper()
	b, err := ClaudeCode()
	if err != nil {
		t.Fatal(err)
	}
	return b.CodeLaunchers
}

// The advisor's reading of the real graph (21450b6), §4: the allow rules of
// execute_shell with a wildcard, as the graph stores their specifier. The
// rule texts are reconstructed from the shapes the reading names; the real
// count comes from the run log ("report: launcher rules ...").
func TestLauncherOf_RealShapes(t *testing.T) {
	ls := launchers(t)
	counts := []struct{ spec, form string }{
		// second shell
		{`powershell -Command *`, "powershell -Command"},
		{`powershell -NoProfile -Command *`, "powershell -Command"},
		{`powershell -NoProfile -Command "Get-ChildItem -Recurse *`, "powershell -Command"},
		// inline code: the "*" inside the code argument not yet closed
		{`node -e 'const fs=require\(\"fs\"\); *`, "node -e / --eval"},
		{`node -e 'console.log\(JSON.parse\(require\(\"fs\"\).readFileSync\(*`, "node -e / --eval"},
		{`node -e "require('child_process').execSync(*`, "node -e / --eval"},
		{`node --input-type=module -e ' *`, "node -e / --eval"},
		{`python -c ' *`, "python -c"},
		{`python -c "import json; *`, "python -c"},
		{`node -e *`, "node -e / --eval"},
		{`node --eval=console.log*`, "node -e / --eval"},
		{`node -p *`, "node -p / --print"},
		// npx executors and env launchers
		{`npx tsx *`, "npx tsx *"},
		{`npx -y tsx@4 *`, "npx tsx *"},
		{`npx *`, "npx *"},
		{`docker exec *`, "docker exec"},
		{`docker exec -it web *`, "docker exec"},
		{`cmd /c *`, "cmd /c"},
		{`git -c *`, "git * (options before the subcommand)"},
		{`git * main`, "git * (options before the subcommand)"},
		{`pwsh -c *`, "pwsh -Command / -c"},
	}
	for _, c := range counts {
		l, ok := LauncherOf(c.spec, ls)
		if !ok || l.Form != c.form {
			t.Errorf("%q: got %q %v, want %q", c.spec, l.Form, ok, c.form)
		}
	}
	none := []string{
		`npx eslint *`, `npx next *`, `npx playwright *`, `npx supabase *`, `npx tsc *`,
		`git -c user.name=t -c user.email=t@t commit -m 'fix: *`,
		`git -c core.autocrlf=false commit -m "wip *`,
		`git log *`,
		`node -e 'console.log(1)' *`, // closed code argument: program arguments
		`node --version *`,
		`node scripts/build.js *`,
		`python -c 'print(1)' *`,
		`npm run *`,
		`npx tsx*`,             // the package is not fixed
		`powershell -c *`,      // Windows PowerShell 5.1: -c not in the vendor doc
		`bash -c *`, `sh -c *`, // no vendor source reachable (BV-37 report)
		`powershell Get-ChildItem *`,
		`* --version`,
		`npm test`,
	}
	for _, spec := range none {
		if l, ok := LauncherOf(spec, ls); ok {
			t.Errorf("%q counted as %q", spec, l.Form)
		}
	}
}

func TestTokenize(t *testing.T) {
	cases := []struct {
		in   string
		want []token
	}{
		{`a b `, []token{{"a", true}, {"b", true}}},
		{`a 'b c`, []token{{"a", true}, {"b c", false}}},
		{`a 'b c' `, []token{{"a", true}, {"b c", true}}},
		{`a "x\" y`, []token{{"a", true}, {`x" y`, false}}},
		{`a b\ c`, []token{{"a", true}, {"b c", false}}},
		{`a 'x\(1\)' `, []token{{"a", true}, {`x\(1\)`, true}}},
	}
	for _, c := range cases {
		got := tokenize(c.in)
		if len(got) != len(c.want) {
			t.Errorf("%q: got %v, want %v", c.in, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%q: got %v, want %v", c.in, got, c.want)
			}
		}
	}
}

// Every code launcher of the knowledge base names its source.
func TestCodeLaunchersHaveSources(t *testing.T) {
	for _, l := range launchers(t) {
		if l.SourceURL == "" || l.Verified == "" {
			t.Errorf("%s: no source", l.Form)
		}
	}
}
