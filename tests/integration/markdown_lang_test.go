package integration_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// BV-45: the technical Markdown is always English. A run with --lang en and
// two with --lang es (the last has an earlier memory): the Markdown carries
// no text of the es catalog and its memory section is titled "Memory".
func TestMarkdownAlwaysEnglish(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("black-box run is implemented for Unix-like hosts")
	}
	bin := filepath.Join(t.TempDir(), "rosmarops")
	build := exec.Command("go", "build", "-o", bin, "./cmd/rosmarops")
	build.Dir = filepath.Join("..", "..")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	root := t.TempDir()
	home := filepath.Join(root, "home")
	env := []string{"PATH=" + os.Getenv("PATH")}
	for name, dir := range map[string]string{
		"HOME": home, "USERPROFILE": home, "APPDATA": filepath.Join(root, "appdata"), "LOCALAPPDATA": filepath.Join(root, "localappdata"),
		"XDG_CONFIG_HOME": filepath.Join(root, "xdg"), "XDG_CACHE_HOME": filepath.Join(root, "xdgc"), "TMPDIR": filepath.Join(root, "tmp"),
	} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		env = append(env, name+"="+dir)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"mcpServers":{"demo":{"command":"node","args":["server.js"]}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Claude Code, and a project in the user's folder with two keys: the
	// export then has findings, actions and paths under the home.
	repo := filepath.Join(home, "src", "app")
	for path, content := range map[string]string{
		filepath.Join(home, ".claude", "settings.json"): `{"permissions":{"allow":["Bash(npm test)"]}}`,
		filepath.Join(repo, ".env.local"):               "SUPABASE_SECRET_KEY=sb_secret_" + strings.Repeat("S", 22) + "_" + strings.Repeat("T", 8) + "\n",
		filepath.Join(repo, ".env"):                     "OPENAI_API_KEY=sk-proj-" + strings.Repeat("O", 48) + "\n",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	consoles := map[string]string{} // --lang: the run's standard output
	// init records the configuration (BV-68, J1): the repo_roots edited
	// after it are accepted with baseline, as a person would confirm them.
	for _, argv := range [][]string{{"init"}, {"baseline"}, {"run", "--lang", "en"}, {"run", "--lang", "es"}, {"run", "--lang", "es"}} {
		cmd := exec.Command(bin, argv...)
		cmd.Dir, cmd.Env = root, env
		out, err := cmd.Output()
		var exitErr *exec.ExitError
		if err != nil && !(errors.As(err, &exitErr) && argv[0] == "run") {
			t.Fatalf("%v: %v\n%s", argv, err, out)
		}
		if argv[0] == "run" {
			consoles[argv[2]] = string(out)
		}
		if argv[0] == "init" {
			cfgPath := filepath.Join(home, ".rosmarops", "config.json")
			cfg := map[string]any{}
			b, err := os.ReadFile(cfgPath)
			if err == nil {
				err = json.Unmarshal(b, &cfg)
			}
			if err != nil {
				t.Fatal(err)
			}
			cfg["repo_roots"] = []string{repo}
			b, _ = json.Marshal(cfg)
			if err := os.WriteFile(cfgPath, b, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}

	all, _ := filepath.Glob(filepath.Join(home, ".rosmarops", "reports", "*.md"))
	var mds, exports []string // the technical Markdown; the exports for your AI
	for _, p := range all {
		if strings.HasSuffix(p, ".ai.md") {
			exports = append(exports, p)
		} else {
			mds = append(mds, p)
		}
	}
	if len(mds) == 0 || len(exports) == 0 {
		t.Fatalf("no Markdown report or export: %v", all)
	}
	messages := func(lang string) map[string]string {
		b, err := os.ReadFile(filepath.Join("..", "..", "internal", "catalog", "messages", lang+".json"))
		if err != nil {
			t.Fatal(err)
		}
		m := map[string]string{}
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	en, es := messages("en"), messages("es")

	// BV-46: the console's summary line starts with the status text and a
	// space, and has no ".:", in en and es.
	for lang, out := range consoles {
		cat := map[string]map[string]string{"en": en, "es": es}[lang]
		found := false
		for _, line := range strings.Split(out, "\n") {
			for _, status := range []string{"green", "yellow", "red"} {
				if text := cat["report.status."+status]; strings.HasPrefix(line, text) {
					found = true
					if !strings.HasPrefix(line, text+" ") || strings.Contains(line, ".:") {
						t.Errorf("%s: console line %q", lang, line)
					}
				}
			}
		}
		if !found {
			t.Errorf("%s: no summary line in %q", lang, out)
		}
	}
	memory := false
	for _, p := range mds {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		md := string(b)
		memory = memory || strings.Contains(md, "\n## Memory\n")
		for k, text := range es {
			// The fixed part of each Spanish text: what precedes its first
			// parameter, when it is not the English text too.
			fixed, _, _ := strings.Cut(text, "{")
			if fixed = strings.TrimSpace(fixed); len(fixed) < 4 || fixed == strings.TrimSpace(strings.SplitN(en[k], "{", 2)[0]) {
				continue
			}
			if strings.Contains(md, fixed) {
				t.Errorf("%s: text of es key %s: %q", filepath.Base(p), k, fixed)
			}
		}
	}
	// informe HTML v2 §6: the export of the last run (--lang es) uses only
	// html.* texts: no English text of finding.*, action.*, unknown.*,
	// today.*, nor the code's English labels.
	sort.Slice(exports, func(i, j int) bool {
		a, _ := os.Stat(exports[i])
		b, _ := os.Stat(exports[j])
		return a.ModTime().Before(b.ModTime())
	})
	raw, err := os.ReadFile(exports[len(exports)-1])
	if err != nil {
		t.Fatal(err)
	}
	export := string(raw)
	if !strings.Contains(export, es["ai.instruction"]) || !strings.Contains(export, "## "+es["ai.findings"]) {
		t.Errorf("export not in Spanish, or without findings: %q", export)
	}
	if strings.Contains(export, home) || strings.Contains(export, strings.TrimPrefix(home, "/")) {
		t.Errorf("export names the user's folder: %q", export)
	}
	for k, text := range en {
		if !strings.HasPrefix(k, "finding.") && !strings.HasPrefix(k, "action.") && !strings.HasPrefix(k, "unknown.") && !strings.HasPrefix(k, "today.") {
			continue
		}
		for _, part := range strings.FieldsFunc(text, func(r rune) bool { return r == '{' || r == '}' }) {
			if part = strings.TrimSpace(part); len(part) >= 12 && strings.Contains(export, part) {
				t.Errorf("export carries English text of %s: %q", k, part)
			}
		}
	}
	for _, label := range []string{"the service it belongs to", "database credential"} {
		if strings.Contains(export, label) {
			t.Errorf("export carries the code label %q", label)
		}
	}
	if !memory {
		t.Error(`no Markdown with a section "## Memory"`)
	}
}
