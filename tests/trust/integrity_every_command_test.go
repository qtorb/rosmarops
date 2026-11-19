package trust_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/catalog"
)

// unfilled is a {name} placeholder left in what the program printed.
var unfilled = regexp.MustCompile(`\{[a-z_]+\}`)

// withLocale is the sandbox with its system language set to lang: Before has
// no --lang, so a command's integrity texts follow the system's language.
func withLocale(s *sandbox, lang string) *sandbox {
	locale := map[string]string{"en": "C", "es": "es_ES.UTF-8"}[lang]
	c := *s
	c.env = nil
	for _, kv := range s.env {
		if name, _, _ := strings.Cut(kv, "="); name != "LC_ALL" && name != "LC_MESSAGES" && name != "LANG" {
			c.env = append(c.env, kv)
		}
	}
	c.env = append(c.env, "LC_ALL="+locale, "LANG="+locale)
	return &c
}

// changeBinaryReference makes the state's binary reference another binary's,
// as after downloading a new version.
func changeBinaryReference(t *testing.T, s *sandbox) string {
	t.Helper()
	p := filepath.Join(s.home, ".rosmarops", "state.json")
	var st map[string]any
	raw, err := os.ReadFile(p)
	if err == nil {
		err = json.Unmarshal(raw, &st)
	}
	if err != nil {
		t.Fatal(err)
	}
	st["integrity"].(map[string]any)["vigil_binary_sha256"] = strings.Repeat("a", 64)
	out, _ := json.MarshalIndent(st, "", "  ")
	if err := os.WriteFile(p, out, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// A command other than run (usage), after the binary changed: without a terminal it
// stops in red (exit 2) with the catalog's texts in the system's language;
// with one it asks like run and, confirmed, records the new reference and
// goes on. Nothing it prints mentions baseline or leaves a placeholder.
func TestIntegrityEveryCommand(t *testing.T) {
	s := newSandbox(t)
	s.runAll(t, nil)
	exe, err := os.ReadFile(s.binary)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(exe)
	self := hex.EncodeToString(sum[:])
	for _, lang := range []string{"en", "es"} {
		t.Run(lang, func(t *testing.T) {
			ls := withLocale(s, lang)
			cat, err := catalog.Load(lang)
			if err != nil {
				t.Fatal(err)
			}
			stPath := changeBinaryReference(t, ls)

			cmd := exec.Command(ls.binary, "usage")
			cmd.Dir, cmd.Env = ls.work, ls.env
			out, err := cmd.CombinedOutput()
			var exitErr *exec.ExitError
			if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
				t.Fatalf("usage without a terminal: %v, want exit 2\n%s", err, out)
			}
			for _, k := range []string{"integrity.abort.binary.why", "integrity.abort.binary.action"} {
				if !strings.Contains(string(out), cat.Text(k, nil)) {
					t.Errorf("no %s in:\n%s", k, out)
				}
			}
			checkPrinted(t, "without a terminal", string(out))

			yes := map[string]string{"en": "y\n", "es": "s\n"}[lang]
			got := runInPTY(t, ls, []string{ls.binary, "usage"}, yes)
			if strings.Contains(got, cat.Text("integrity.abort.binary.why", nil)) {
				t.Errorf("stopped although confirmed:\n%s", got)
			}
			if !strings.Contains(got, strings.TrimSpace(cat.Text("console.binary_confirm", nil))) {
				t.Errorf("not asked:\n%s", got)
			}
			checkPrinted(t, "with a terminal", got)
			var st struct {
				Integrity struct {
					Binary string `json:"vigil_binary_sha256"`
				} `json:"integrity"`
			}
			raw, _ := os.ReadFile(stPath)
			if err := json.Unmarshal(raw, &st); err != nil || st.Integrity.Binary != self {
				t.Errorf("reference not recorded: %v %q", err, st.Integrity.Binary)
			}
		})
	}
}

func checkPrinted(t *testing.T, name, out string) {
	t.Helper()
	if strings.Contains(strings.ToLower(out), "baseline") {
		t.Errorf("%s: baseline in the output:\n%s", name, out)
	}
	if m := unfilled.FindString(out); m != "" {
		t.Errorf("%s: placeholder %s left:\n%s", name, m, out)
	}
}
