package credfiles_test

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/qtorb/rosmarops/internal/graph"
	"github.com/qtorb/rosmarops/internal/platform"
	"github.com/qtorb/rosmarops/internal/scan/credfiles"
)

type platformEntry = platform.CredentialEntry

// BV-88 (decision 2026-10-27, agentes nuevos, N1). Codex's auth.json keys
// come from the vendor's source: https://github.com/openai/codex, commit
// b89952d582beaae3af3ddd7b679286cbb094f8b9, codex-rs/login/src/auth/storage.rs
// (AuthDotJson) and codex-rs/login/src/token_data.rs (TokenData).
func TestCollect_CodexAuth(t *testing.T) {
	home := t.TempDir()
	auth := filepath.Join(home, ".codex", "auth.json")
	write(t, auth, `{"auth_mode": "chatgpt", "OPENAI_API_KEY": "sk-key",
	  "tokens": {"id_token": "eyJ.id.sig", "access_token": "eyJ.acc.sig", "refresh_token": "rt-1", "account_id": "acct-1"},
	  "last_refresh": "2026-10-01T00:00:00Z", "personal_access_token": "pat-1",
	  "bedrock_api_key": {"api_key": "not read"}}`)
	scan := credfiles.Collect(home, nil, noEnv)
	got := map[string]string{}
	for _, e := range scan.Entries {
		if e.Path == auth {
			got[e.Pointer] = e.Kind + "=" + e.Value.Reveal()
		}
	}
	want := map[string]string{
		"/OPENAI_API_KEY":        "codex_openai_api_key=sk-key",
		"/tokens/id_token":       "codex_id_token=eyJ.id.sig",
		"/tokens/access_token":   "codex_access_token=eyJ.acc.sig",
		"/tokens/refresh_token":  "codex_refresh_token=rt-1",
		"/personal_access_token": "codex_personal_access_token=pat-1",
	}
	if len(got) != len(want) {
		t.Fatalf("entries %v, want %v", got, want)
	}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("%s: %q, want %q", p, got[p], w)
		}
		kind := strings.SplitN(w, "=", 2)[0]
		if typ, service, ok := graph.CredentialKind(kind); !ok || service != "openai" || typ == "" {
			t.Errorf("%s: kind %q not mapped (%q %q %v)", p, kind, typ, service, ok)
		}
	}
}

// A null or empty value, or no tokens, is no entry; a value that is not a
// string fails the file, without its content in the reason.
func TestCollect_CodexAuthShapes(t *testing.T) {
	for name, tc := range map[string]struct {
		content string
		entries int
		failed  bool
	}{
		"api key only":       {`{"OPENAI_API_KEY": "sk-key", "tokens": null}`, 1, false},
		"null and empty":     {`{"OPENAI_API_KEY": null, "tokens": {"access_token": "", "refresh_token": " "}}`, 0, false},
		"empty object":       {`{}`, 0, false},
		"not an object":      {`["sk-secret-in-array"]`, 0, true},
		"tokens not object":  {`{"tokens": "sk-secret-text"}`, 0, true},
		"key not a string":   {`{"OPENAI_API_KEY": 42}`, 0, true},
		"token not a string": {`{"tokens": {"access_token": {"v": "sk-secret-nested"}}}`, 0, true},
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			auth := filepath.Join(home, ".codex", "auth.json")
			write(t, auth, tc.content)
			scan := credfiles.Collect(home, nil, noEnv)
			n := 0
			for _, e := range scan.Entries {
				if e.Path == auth {
					n++
				}
			}
			var failed []string
			for _, f := range scan.Failed {
				if f.Path == auth {
					failed = append(failed, f.Reason)
				}
			}
			if n != tc.entries || (len(failed) > 0) != tc.failed {
				t.Errorf("entries %d, failed %v", n, failed)
			}
			for _, r := range failed {
				if strings.Contains(r, "sk-secret") {
					t.Errorf("reason holds content: %s", r)
				}
			}
		})
	}
}

// CODEX_HOME moves auth.json; one that is not a folder is ignored.
func TestCollect_CodexHome(t *testing.T) {
	home := t.TempDir()
	alt := filepath.Join(home, "alt")
	write(t, filepath.Join(alt, "auth.json"), `{"OPENAI_API_KEY": "sk-alt"}`)
	write(t, filepath.Join(home, ".codex", "auth.json"), `{"OPENAI_API_KEY": "sk-default"}`)
	env := func(k string) (string, bool) {
		if k == "CODEX_HOME" {
			return alt, true
		}
		return "", false
	}
	if got := codexKeys(credfiles.Collect(home, nil, env).Entries); strings.Join(got, ",") != "sk-alt" {
		t.Errorf("CODEX_HOME: %v", got)
	}
	file := filepath.Join(home, "file")
	write(t, file, "")
	env = func(k string) (string, bool) {
		if k == "CODEX_HOME" {
			return file, true
		}
		return "", false
	}
	if got := codexKeys(credfiles.Collect(home, nil, env).Entries); strings.Join(got, ",") != "sk-default" {
		t.Errorf("CODEX_HOME not a folder: %v", got)
	}
}

func codexKeys(entries []platformEntry) []string {
	var out []string
	for _, e := range entries {
		if e.Kind == "codex_openai_api_key" {
			out = append(out, e.Value.Reveal())
		}
	}
	sort.Strings(out)
	return out
}

// Hermes Agent: <H>\.env and every profile's .env, read as .env files;
// HERMES_HOME moves <H>.
func TestCollect_HermesEnv(t *testing.T) {
	home := t.TempDir()
	h := filepath.Join(home, "AppData", "Local", "hermes")
	write(t, filepath.Join(h, ".env"), "OPENROUTER_API_KEY=a\n# comment\nTELEGRAM_BOT_TOKEN=b\n")
	write(t, filepath.Join(h, "profiles", "work", ".env"), "ANTHROPIC_API_KEY=c\n")
	write(t, filepath.Join(h, "profiles", "home", ".env"), "ANTHROPIC_API_KEY=d\n")
	write(t, filepath.Join(h, "profiles", "notes.txt"), "not a profile\n")
	scan := credfiles.Collect(home, nil, noEnv)
	got := map[string][]string{}
	for _, e := range scan.Entries {
		if strings.HasPrefix(e.Path, h) {
			rel, _ := filepath.Rel(h, e.Path)
			got[filepath.ToSlash(rel)] = append(got[filepath.ToSlash(rel)], e.Key+"="+e.Value.Reveal())
		}
	}
	want := map[string]string{
		".env":               "OPENROUTER_API_KEY=a,TELEGRAM_BOT_TOKEN=b",
		"profiles/work/.env": "ANTHROPIC_API_KEY=c",
		"profiles/home/.env": "ANTHROPIC_API_KEY=d",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for p, w := range want {
		if strings.Join(got[p], ",") != w {
			t.Errorf("%s: %v, want %s", p, got[p], w)
		}
	}

	hh := filepath.Join(home, "hh")
	write(t, filepath.Join(hh, ".env"), "K=moved\n")
	env := func(k string) (string, bool) {
		if k == "HERMES_HOME" {
			return hh, true
		}
		return "", false
	}
	var keys []string
	for _, e := range credfiles.Collect(home, nil, env).Entries {
		if strings.HasPrefix(e.Path, hh) || strings.HasPrefix(e.Path, h) {
			keys = append(keys, e.Value.Reveal())
		}
	}
	if strings.Join(keys, ",") != "moved" {
		t.Errorf("HERMES_HOME: %v", keys)
	}
}
